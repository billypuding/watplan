package durable

import (
	"context"
	"time"
)

func Retry(ctx context.Context, delays []time.Duration, fn func() error) error {
	err := fn()
	for _, d := range delays {
		if err == nil {
			return nil
		}
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return err
		case <-t.C:
		}
		err = fn()
	}
	return err
}
