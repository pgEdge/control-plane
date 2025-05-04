package utils

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

var ErrTimedOut = errors.New("operation timed out")

func WithTimeout(ctx context.Context, timeout time.Duration, f func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- f(ctx)
	}()

	select {
	case <-ctx.Done():
		return ErrTimedOut
	case err := <-done:
		return err
	}
}

// Retry retries the given function up to maxAttempts times with an exponential backoff starting at initialDelay.
func Retry(maxAttempts int, initialDelay time.Duration, f func() error) error {
	var err error
	delay := initialDelay
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = f()

		// Return if successful
		if err == nil {
			return nil
		}

		// Otherwise sleep and try again
		time.Sleep(delay)
		delay *= time.Duration(2)
	}
	if err != nil {
		return fmt.Errorf("exhausted retries. final error: %w", err)
	}
	return nil
}

func PointerTo[T any](v T) *T {
	return &v
}

func FromPointer[T comparable](v *T) T {
	var zero T
	if v == nil {
		return zero
	}
	return *v
}

// NillablePointerTo returns a pointer to v if v is not the zero value of its
// type, otherwise it returns nil.
func NillablePointerTo[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

func RandomString(length int) (string, error) {
	randomBytes := make([]byte, length)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(randomBytes), nil
}
