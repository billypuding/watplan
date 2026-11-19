package durable

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu    sync.Mutex
	puts  map[string][]string
	fails int
}

func (r *recorder) Put(key string, value []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fails > 0 {
		r.fails--
		return errors.New("chwilowy błąd")
	}
	r.puts[key] = append(r.puts[key], string(value))
	return nil
}

func (r *recorder) All(string) (map[string][]byte, error) { return nil, nil }

func TestQueueCoalescesAndRetries(t *testing.T) {
	rec := &recorder{puts: map[string][]string{}, fails: 1}
	q := NewQueue(rec)
	_ = q.Put("plan/A", []byte("1"))
	_ = q.Put("plan/A", []byte("2"))
	_ = q.Put("plan/A", []byte("3"))

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		got := rec.puts["plan/A"]
		rec.mu.Unlock()
		if len(got) > 0 {
			if got[len(got)-1] != "3" || len(got) != 1 {
				t.Fatalf("zapisy %v, chcę tylko ostatni", got)
			}
			if failed, saved := q.Status(); failed != "" || saved != 1 {
				t.Fatalf("status %q %d", failed, saved)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("zapis nie dotarł do bazy mimo ponowienia")
}
