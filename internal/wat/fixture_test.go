package wat

import (
	"os"
	"strings"
	"testing"
)

func TestFixtureMathTeachers(t *testing.T) {
	f, err := os.Open("testdata/plan_wat.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	events, err := ParsePlan(f, 2026)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("brak zdarzeń")
	}

	want := map[string]string{
		"Matematyka 1|wykład":    "dr hab. Czuchry Ewa",
		"Matematyka 1|ćwiczenia": "dr Ogorzały Justyna",
		"Matematyka 2|wykład":    "dr hab. Czuchry Ewa",
		"Matematyka 2|ćwiczenia": "dr Ogorzały Justyna",
	}
	seen := map[string]bool{}
	for _, e := range events {
		if !strings.HasPrefix(e.Subject, "Matematyka") {
			continue
		}
		key := e.Subject + "|" + e.Type
		seen[key] = true
		if w, ok := want[key]; ok && e.Teacher != w {
			t.Errorf("%s %s: prowadzący %q, chcę %q", e.Date, key, e.Teacher, w)
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("brak zajęć %s", k)
		}
	}
}
