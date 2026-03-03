package systemd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/logging"
)

const stopTimeout = 30 * time.Second

var ErrUnitNotFound = errors.New("unit does not exist")

type Client struct {
	logger zerolog.Logger
	conn   *dbus.Conn
}

func NewClient(loggerFactory *logging.Factory) *Client {
	return &Client{
		logger: loggerFactory.Logger("systemd_client"),
	}
}

func (c *Client) Start(ctx context.Context) error {
	c.logger.Debug().Msg("starting systemd client")

	conn, err := dbus.NewWithContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to start dbus connection: %w", err)
	}

	c.conn = conn

	return nil
}

func (c *Client) Reload(ctx context.Context) error {
	c.logger.Debug().Msg("reloading systemd")

	if err := c.conn.ReloadContext(ctx); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	c.logger.Debug().Msg("reloaded systemd")

	return nil
}

func (c *Client) StartUnit(ctx context.Context, name string) error {
	logger := c.logger.With().Str("unit", name).Logger()
	logger.Debug().Msg("starting unit")

	resCh := make(chan string, 1)
	pid, err := c.conn.StartUnitContext(ctx, name, "replace", resCh)
	if err != nil {
		return fmt.Errorf("failed to start unit '%s': %w", name, err)
	}
	res := <-resCh

	c.logger.Debug().
		Str("response", res).
		Int("pid", pid).
		Msg("started unit")

	return nil
}

func (c *Client) StopUnit(ctx context.Context, name string, wait bool) error {
	logger := c.logger.With().Str("unit", name).Logger()
	logger.Debug().Msg("stopping unit")

	resCh := make(chan string, 1)
	pid, err := c.conn.StopUnitContext(ctx, name, "replace", resCh)
	if err != nil {
		return fmt.Errorf("failed to stop unit '%s': %w", name, err)
	}
	res := <-resCh

	c.logger.Debug().
		Str("response", res).
		Int("pid", pid).
		Msg("stopped unit")

	if wait && pid != 0 {
		c.logger.Debug().
			Int("pid", pid).
			Float64("timeout_seconds", stopTimeout.Seconds()).
			Msg("waiting for main process to exit")

		if err := waitForPid(pid, stopTimeout); err != nil {
			return fmt.Errorf("failed to wait for pid %d to exit: %w", pid, err)
		}
	}

	return nil
}

func (c *Client) RestartUnit(ctx context.Context, name string) error {
	logger := c.logger.With().Str("unit", name).Logger()
	logger.Debug().Msg("restarting unit")

	resCh := make(chan string, 1)
	pid, err := c.conn.ReloadOrRestartUnitContext(ctx, name, "replace", resCh)
	if err != nil {
		return fmt.Errorf("failed to restart unit '%s': %w", name, err)
	}
	res := <-resCh

	c.logger.Debug().
		Str("response", res).
		Int("pid", pid).
		Msg("restarted unit")

	return nil
}

func (c *Client) EnableUnit(ctx context.Context, name string) error {
	logger := c.logger.With().Str("unit", name).Logger()
	logger.Debug().Msg("enabling unit")

	_, res, err := c.conn.EnableUnitFilesContext(ctx, []string{name}, false, false)
	if err != nil {
		return fmt.Errorf("failed to enable unit '%s': %w", name, err)
	}

	var change dbus.EnableUnitFileChange
	if len(res) > 0 {
		change = res[0]
	}

	c.logger.Debug().
		Str("change.filename", change.Filename).
		Str("change.destination", change.Destination).
		Str("change.type", change.Type).
		Msg("enabled unit")

	return nil
}

func (c *Client) LinkUnit(ctx context.Context, path string) error {
	logger := c.logger.With().Str("unit", path).Logger()
	logger.Debug().Msg("linking unit")

	res, err := c.conn.LinkUnitFilesContext(ctx, []string{path}, false, false)
	if err != nil {
		return fmt.Errorf("failed to link unit '%s': %w", path, err)
	}

	var change dbus.LinkUnitFileChange
	if len(res) > 0 {
		change = res[0]
	}

	c.logger.Debug().
		Str("change.filename", change.Filename).
		Str("change.destination", change.Destination).
		Str("change.type", change.Type).
		Msg("linked unit")

	return nil
}

func (c *Client) DisableUnit(ctx context.Context, path string) error {
	logger := c.logger.With().Str("unit", path).Logger()
	logger.Debug().Msg("disabling unit")

	res, err := c.conn.DisableUnitFilesContext(ctx, []string{path}, false)
	if err != nil {
		return fmt.Errorf("failed to disable unit '%s': %w", path, err)
	}

	var change dbus.DisableUnitFileChange
	if len(res) > 0 {
		change = res[0]
	}

	c.logger.Debug().
		Str("change.filename", change.Filename).
		Str("change.destination", change.Destination).
		Str("change.type", change.Type).
		Msg("disabled unit")

	return nil
}

func (c *Client) GetUnitFilePath(ctx context.Context, name string) (string, error) {
	logger := c.logger.With().Str("unit", name).Logger()
	logger.Debug().Msg("getting unit file path")

	prop, err := c.conn.GetUnitPropertyContext(ctx, name, "FragmentPath")
	if err != nil {
		return "", fmt.Errorf("failed to get unit property: %w", err)
	}
	path := prop.Value.String()

	logger.Debug().
		Str("path", path).
		Msg("got unit file path")

	return path, nil
}

func (c *Client) UnitExists(ctx context.Context, name string) error {
	logger := c.logger.With().Str("unit", name).Logger()
	logger.Debug().Msg("checking if unit exists")

	resp, err := c.conn.ListUnitsContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to list units: %w", err)
	}

	for _, unit := range resp {
		if unit.Name == name {
			return nil
		}
	}

	return ErrUnitNotFound
}

func (c *Client) RemoveUnitFile(ctx context.Context, name string) error {
	logger := c.logger.With().Str("unit", name).Logger()
	logger.Debug().Msg("removing unit file")

	if err := c.UnitExists(ctx, name); err != nil {
		return err
	}

	path, err := c.GetUnitFilePath(ctx, name)
	if err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("got empty fragment path for unit '%s'", name)
	}

	err = os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove unit file '%s': %w", name, err)
	}

	logger.Debug().
		Str("path", path).
		Msg("removed unit file")

	return c.Reload(ctx)
}

func (c *Client) Shutdown() error {
	c.logger.Debug().Msg("stopping systemd client")

	if c.conn != nil {
		c.conn.Close()
	}

	return nil
}

// waitForPid waits for the given PID to not exist using a method that works
// for non-child processes.
func waitForPid(pid int, timeout time.Duration) error {
	// FindProcess will return
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(timeout)
	for {
		// Signal 0 doesn't kill — just checks if process exists
		err := proc.Signal(syscall.Signal(0))
		if err != nil {
			return nil // process is gone
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for pid %d to exit after %.2f seconds", pid, timeout.Seconds())
		}
		time.Sleep(500 * time.Millisecond)
	}
}
