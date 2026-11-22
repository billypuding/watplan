package updater

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/alert"
)

func collect(t *testing.T) (*alert.Notifier, func() []string) {
	t.Helper()
	sent := make(chan string, 20)
	n := alert.NewFunc(func(text string) error { sent <- text; return nil })
	return n, func() []string {
		var out []string
		for {
			select {
			case s := <-sent:
				out = append(out, s)
			case <-time.After(150 * time.Millisecond):
				return out
			}
		}
	}
}

func TestParserAlerts(t *testing.T) {
	var body atomic.Value
	u, _, cfg := newTestUpdater(t, &body)
	n, drain := collect(t)
	u.SetAlert(n)

	body.Store(planHTML(12))
	if _, err := u.update(cfg); err != nil {
		t.Fatal(err)
	}
	if got := drain(); len(got) != 0 {
		t.Fatalf("alerty po zwykłym odczycie: %q", got)
	}

	body.Store(planHTML(0))
	if _, err := u.update(cfg); err == nil {
		t.Fatal("chcę błąd dla odczytu bez zajęć")
	}
	if _, err := u.update(cfg); err == nil {
		t.Fatal("chcę błąd dla odczytu bez zajęć")
	}
	got := drain()
	if len(got) != 1 || !strings.Contains(got[0], "nie ma żadnych zajęć") {
		t.Fatalf("alerty po odczycie bez zajęć: %q", got)
	}

	body.Store(planHTML(12))
	if _, err := u.update(cfg); err != nil {
		t.Fatal(err)
	}
	got = drain()
	if len(got) != 1 || !strings.HasPrefix(got[0], "✅") {
		t.Fatalf("po poprawnym odczycie: %q", got)
	}

	body.Store(planHTML(3))
	res, err := u.update(cfg)
	if err != nil || !res.pending {
		t.Fatalf("spadek liczby zajęć: res = %+v, err = %v", res, err)
	}
	got = drain()
	if len(got) != 1 || !strings.Contains(got[0], "spadła z 12 do 3") {
		t.Fatalf("alert o spadku: %q", got)
	}
	body.Store(planHTML(12))
	if _, err := u.update(cfg); err != nil {
		t.Fatal(err)
	}
	if got := drain(); len(got) != 1 || !strings.HasPrefix(got[0], "✅") {
		t.Fatalf("po powrocie do normy: %q", got)
	}
}

func TestParserAlertOnceForManyPlans(t *testing.T) {
	u := &Updater{}
	n, drain := collect(t)
	u.SetAlert(n)
	for _, id := range []string{"A", "B", "C"} {
		u.markSuspect(id, "plan "+id+" bez zajęć")
	}
	if got := drain(); len(got) != 1 {
		t.Fatalf("alertów dla 3 planów: %d (%q), chcę 1", len(got), got)
	}
	u.clearSuspect("A")
	u.clearSuspect("B")
	if got := drain(); len(got) != 0 {
		t.Fatalf("rozwiązane, choć C dalej ma problem: %q", got)
	}
	u.clearSuspect("C")
	if got := drain(); len(got) != 1 || !strings.HasPrefix(got[0], "✅") {
		t.Fatalf("po naprawie wszystkich: %q", got)
	}
}
