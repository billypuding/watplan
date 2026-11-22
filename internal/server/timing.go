package server

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const slowResponse = 500 * time.Millisecond

type timingKey struct{}

type timingPart struct {
	name string
	dur  time.Duration
}

type timing struct {
	start time.Time
	last  time.Time
	parts []timingPart
}

func newTiming() *timing {
	now := time.Now()
	return &timing{start: now, last: now}
}

func timingFrom(ctx context.Context) *timing {
	if t, ok := ctx.Value(timingKey{}).(*timing); ok {
		return t
	}
	return newTiming()
}

func (t *timing) mark(name string) {
	if t == nil {
		return
	}
	now := time.Now()
	t.parts = append(t.parts, timingPart{name: name, dur: now.Sub(t.last)})
	t.last = now
}

func (t *timing) total() time.Duration {
	return time.Since(t.start)
}

func (t *timing) header() string {
	var b strings.Builder
	for _, p := range t.parts {
		fmt.Fprintf(&b, "%s;dur=%.1f, ", p.name, float64(p.dur.Microseconds())/1000)
	}
	fmt.Fprintf(&b, "total;dur=%.1f", float64(t.total().Microseconds())/1000)
	return b.String()
}

func (t *timing) String() string {
	parts := make([]string, 0, len(t.parts))
	for _, p := range t.parts {
		parts = append(parts, fmt.Sprintf("%s=%v", p.name, p.dur.Round(time.Millisecond)))
	}
	return fmt.Sprintf("%v (%s)", t.total().Round(time.Millisecond), strings.Join(parts, " "))
}
