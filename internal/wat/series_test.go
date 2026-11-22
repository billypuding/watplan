package wat

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/alert"
)

func TestFetchFailureSeriesAlert(t *testing.T) {
	sent := make(chan string, 20)
	f := &Fetcher{failures: map[string]int{}}
	f.SetAlert(alert.NewFunc(func(text string) error { sent <- text; return nil }))
	drain := func() []string {
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

	for i := 0; i < failureSeries-1; i++ {
		f.noteResult("wml.wat.edu.pl", errors.New("timeout"))
	}
	f.noteResult("wml.wat.edu.pl", fmt.Errorf("pobieranie: %w", ErrGone))
	if got := drain(); len(got) != 0 {
		t.Fatalf("alert przed progiem: %q", got)
	}
	f.noteResult("wml.wat.edu.pl", errors.New("proxyconnect http://user:tajne@pr.example:7777: odmowa"))
	f.noteResult("wml.wat.edu.pl", errors.New("timeout"))
	got := drain()
	if len(got) != 1 || !strings.Contains(got[0], "5 błędów z rzędu") {
		t.Fatalf("alert po serii: %q", got)
	}
	if strings.Contains(got[0], "tajne") || !strings.Contains(got[0], "//***@") {
		t.Fatalf("hasło w alercie: %q", got[0])
	}
	f.noteResult("wel.wat.edu.pl", errors.New("timeout"))
	f.noteResult("wml.wat.edu.pl", ErrNotModified)
	got = drain()
	if len(got) != 1 || !strings.HasPrefix(got[0], "✅") || !strings.Contains(got[0], "wml") {
		t.Fatalf("po udanym pobraniu: %q", got)
	}
	if f.failures["wel.wat.edu.pl"] != 1 {
		t.Fatalf("liczniki hostów się mieszają: %v", f.failures)
	}
}
