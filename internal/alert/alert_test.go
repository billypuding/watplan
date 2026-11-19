package alert

import (
	"sync"
	"testing"
	"time"
)

type inbox struct {
	mu   sync.Mutex
	msgs []string
}

func (b *inbox) post(text string) error {
	b.mu.Lock()
	b.msgs = append(b.msgs, text)
	b.mu.Unlock()
	return nil
}

func (b *inbox) wait(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		got := append([]string(nil), b.msgs...)
		b.mu.Unlock()
		if len(got) >= n {
			time.Sleep(50 * time.Millisecond)
			b.mu.Lock()
			defer b.mu.Unlock()
			return append([]string(nil), b.msgs...)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("nie przyszło %d wiadomości", n)
	return nil
}

func TestAlertsOnlyOnStateChanges(t *testing.T) {
	box := &inbox{}
	n := NewFunc(box.post)
	n.Resolved("proxy", "działa")
	n.Problem("proxy", "402")
	n.Problem("proxy", "402")
	n.Problem("proxy", "402")
	n.Resolved("proxy", "znowu działa")
	n.Resolved("proxy", "znowu działa")
	got := box.wait(t, 2)
	if len(got) != 2 || got[0] != "⚠️ 402" || got[1] != "✅ znowu działa" {
		t.Fatalf("wiadomości %q", got)
	}
}

func TestAlertRepeatsChangedProblem(t *testing.T) {
	box := &inbox{}
	n := NewFunc(box.post)
	n.Problem("faculty:WCY", "blokada")
	n.Problem("faculty:WCY", "404")
	if got := box.wait(t, 2); len(got) != 2 {
		t.Fatalf("zmieniony problem nie wysłał nowej wiadomości: %q", got)
	}
}

func TestNilNotifierIsSafe(t *testing.T) {
	var n *Notifier
	n.Problem("x", "y")
	n.Resolved("x", "y")
}
