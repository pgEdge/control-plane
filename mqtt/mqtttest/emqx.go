package mqtttest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type EMQX struct {
	container testcontainers.Container
	host      string
	port      int
}

func (e *EMQX) Hostname() string {
	return e.host
}

func (e *EMQX) Port() int {
	return e.port
}

func (e *EMQX) URL() string {
	return fmt.Sprintf("tcp://%s:%d", e.host, e.port)
}

func (e *EMQX) Container() testcontainers.Container {
	return e.container
}

func (e *EMQX) Terminate(ctx context.Context) error {
	return e.container.Terminate(ctx)
}

// NewEMQX creates an emqx container for local testing. Call .URL() on the
// returned EMQX instance to get the URL to connect to.
func NewEMQX(ctx context.Context) (*EMQX, error) {
	port, err := nat.NewPort("tcp", "1883")
	if err != nil {
		return nil, err
	}
	req := testcontainers.ContainerRequest{
		Image:        "emqx/emqx:5.3.1",
		Env:          map[string]string{},
		ExposedPorts: []string{port.Port()},
		WaitingFor: wait.ForLog("EMQX 5.3.1 is running now!").
			WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx,
		testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
	if err != nil {
		return nil, err
	}
	mappedPort, err := container.MappedPort(ctx, port)
	if err != nil {
		termErr := container.Terminate(ctx)
		return nil, errors.Join(err, termErr)
	}
	host, err := container.Host(ctx)
	if err != nil {
		termErr := container.Terminate(ctx)
		return nil, errors.Join(err, termErr)
	}
	return &EMQX{container: container, host: host, port: mappedPort.Int()}, nil
}
