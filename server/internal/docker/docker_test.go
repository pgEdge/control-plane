package docker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeImageInspector is a configurable stand-in for the docker client's
// registry + local image inspection surface, letting checkImageExists be
// unit-tested without a live daemon or registry.
type fakeImageInspector struct {
	distErr  error // returned by DistributionInspect (registry lookup)
	localErr error // returned by ImageInspectWithRaw (local daemon lookup)

	// blockDist makes DistributionInspect block until its context expires and
	// then return that context's error, simulating a hung/unreachable registry.
	blockDist bool

	distCalled  bool
	localCalled bool
	// localCtxErr records the context error observed by ImageInspectWithRaw at
	// call time, so a test can assert the local fallback got a live budget.
	localCtxErr error
}

func (f *fakeImageInspector) DistributionInspect(ctx context.Context, image, encodedRegistryAuth string) (registry.DistributionInspect, error) {
	f.distCalled = true
	if f.blockDist {
		<-ctx.Done()
		return registry.DistributionInspect{}, ctx.Err()
	}
	return registry.DistributionInspect{}, f.distErr
}

func (f *fakeImageInspector) ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error) {
	f.localCalled = true
	f.localCtxErr = ctx.Err()
	return types.ImageInspect{}, nil, f.localErr
}

// Registry lookup succeeds => image is verified, no local fallback needed.
func TestCheckImageExists_RegistryHit(t *testing.T) {
	f := &fakeImageInspector{}
	require.NoError(t, checkImageExists(context.Background(), f, "", "registry.example.com/img:tag", time.Second, time.Second))
	assert.True(t, f.distCalled, "expected registry inspect to run")
	assert.False(t, f.localCalled, "local fallback should not run when the registry lookup succeeds")
}

// Registry lookup fails but the image is present in the local daemon (a
// locally-built or already-pulled image) => verified via the fallback.
func TestCheckImageExists_RegistryMissLocalHit(t *testing.T) {
	f := &fakeImageInspector{distErr: errors.New("no such image in registry")}
	require.NoError(t, checkImageExists(context.Background(), f, "", "localbuild:latest", time.Second, time.Second))
	assert.True(t, f.localCalled, "expected local fallback to run when the registry lookup fails")
}

// Registry lookup fails AND the image is absent locally => error, and it should
// surface the registry error (the primary lookup path).
func TestCheckImageExists_BothMiss(t *testing.T) {
	f := &fakeImageInspector{
		distErr:  errors.New("registry unreachable"),
		localErr: errors.New("no such image"),
	}
	err := checkImageExists(context.Background(), f, "", "ghost:latest", time.Second, time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost:latest")
	assert.Contains(t, err.Error(), "registry unreachable")
}

// A slow/hung registry lookup that consumes its whole budget must NOT starve
// the local-daemon fallback: the local check gets its own independent budget,
// so a locally-built image is still found. Regression guard for the shared-
// context starvation bug.
func TestCheckImageExists_SlowRegistryDoesNotStarveLocal(t *testing.T) {
	f := &fakeImageInspector{blockDist: true}
	err := checkImageExists(context.Background(), f, "", "localbuild:latest",
		20*time.Millisecond, 2*time.Second)
	require.NoError(t, err, "local fallback should still find the image after a slow registry failure")
	assert.True(t, f.localCalled, "expected local fallback to run")
	assert.NoError(t, f.localCtxErr, "local inspect must receive a live (un-expired) context")
}
