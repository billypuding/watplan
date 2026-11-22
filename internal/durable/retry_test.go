package durable

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetry(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}, func() error {
		calls++
		if calls < 3 {
			return errors.New("jeszcze nie")
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("err = %v, prób = %d, chcę nil i 3", err, calls)
	}

	calls = 0
	err = Retry(context.Background(), []time.Duration{time.Millisecond, time.Millisecond}, func() error {
		calls++
		return errors.New("nie działa")
	})
	if err == nil || calls != 3 {
		t.Fatalf("err = %v, prób = %d, chcę błąd i 3 próby", err, calls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls = 0
	if err := Retry(ctx, []time.Duration{time.Hour}, func() error { calls++; return errors.New("x") }); err == nil || calls != 1 {
		t.Fatalf("po anulowaniu: err = %v, prób = %d", err, calls)
	}
}
