package wat

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func parseWLOFixture(t *testing.T) []eventView {
	t.Helper()
	f, err := os.Open("testdata/plan_wlo.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	events, err := ParsePlan(f, 2026)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]eventView, len(events))
	for i, e := range events {
		out[i] = eventView{e.Date + " " + e.Start, e.Subject, e.Type, e.Teacher, e.Room}
	}
	return out
}

type eventView struct{ when, subject, typ, teacher, room string }

func TestPlansoftWLOCells(t *testing.T) {
	events := parseWLOFixture(t)
	if len(events) < 250 {
		t.Fatalf("zajęć %d", len(events))
	}
	want := map[string]eventView{
		"2026-10-01 08:00": {"", "Wychowanie fizyczne", "ćwiczenia", "mgr Czajko Zdzisław", ""},
		"2026-10-01 11:40": {"", "Topografia wojskowa", "wykład", "mgr inż. Oleksiak Marek", "111 58"},
		"2026-10-02 09:50": {"", "Wprowadzenie do studiowania", "wykład", "dr Łakoma Ewa", "aula/ 135"},
		"2026-10-02 13:30": {"", "Podstawy grafiki inżynierskiej", "wykład", "dr hab. inż. (prof. ucz.) Hutsaylyuk Volodymyr", "301/ S"},
		"2026-10-05 09:50": {"", "Język angielski", "ćwiczenia", "", "128 K ; 127 K ; 126 K ; 125 K ; 124 K ; 122 K ; 121 K ; 120 K"},
	}
	found := 0
	for _, e := range events {
		w, ok := want[e.when]
		if !ok || e.subject != w.subject {
			continue
		}
		found++
		if e.typ != w.typ || e.teacher != w.teacher || e.room != w.room {
			t.Errorf("%s %s: %+v, chcę %+v", e.when, e.subject, e, w)
		}
	}
	if found != len(want) {
		t.Fatalf("znaleziono %d z %d oczekiwanych zajęć", found, len(want))
	}
}

func TestPlansoftWLONoGroupsInFields(t *testing.T) {
	group := regexp.MustCompile(`(?i)WLO\\d\\d|\\dPL/\\d+KP`)
	for _, e := range parseWLOFixture(t) {
		if group.MatchString(e.room) || group.MatchString(e.teacher) || group.MatchString(e.subject) {
			t.Errorf("%s: lista grup w polach: %+v", e.when, e)
		}
		if strings.HasSuffix(e.subject, ")") && strings.Contains(e.subject, "(") && e.typ == "" {
			t.Errorf("%s: nierozdzielony przedmiot i rodzaj: %q", e.when, e.subject)
		}
		if regexp.MustCompile(`^T\\d`).MatchString(e.room) {
			t.Errorf("%s: znacznik turnusu jako sala: %q", e.when, e.room)
		}
	}
}

func TestAbbreviationMatching(t *testing.T) {
	ix := newTeacherIndex(map[string]string{
		"a": "mgr Czajko Zdzisław", "b": "mgr inż. Oleksiak Marek", "c": "dr Łakoma Ewa",
		"d": "dr hab. inż.(prof. ucz.) Wysocki Józef", "e": "dr hab. inż. (prof. ucz.) Wysocki Józef",
		"f": "dr Klimek Krzysztof", "g": "dr Klimczak Anna",
	})
	for abbr, want := range map[string]string{
		"ZCzajko": "mgr Czajko Zdzisław",
		"OleM":    "mgr inż. Oleksiak Marek",
		"Ł.E.":    "dr Łakoma Ewa",
		"Wys.J":   "dr hab. inż. (prof. ucz.) Wysocki Józef",
		"Klim":    "",
		"Klimek":  "dr Klimek Krzysztof",
		"XyZ":     "",
	} {
		got, _ := ix.byAbbreviation(abbr)
		if got != want {
			t.Errorf("%q → %q, chcę %q", abbr, got, want)
		}
	}
}
