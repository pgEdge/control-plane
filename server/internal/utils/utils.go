package utils

import (
	"context"
	"errors"
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
