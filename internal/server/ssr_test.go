package server

import (
	"github.com/billypuding/watplan/internal/model"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRenderSSRDay(t *testing.T) {
	raw, err := os.ReadFile("../../e2e/fixtures/data/WMT26AX3S1.json")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 19, 10, 15, 0, 0, warsawLoc)
	page := renderSSR("WMT26AX3S1", raw, nil, now)
	if page == nil {
		t.Fatal("brak SSR dla planu z zajęciami w bieżącym semestrze")
	}
	for _, want := range []string{
		`<div class="day-title"><h1>Czwartek</h1><p>19 listopada · <button type="button" class="week-link" aria-haspopup="dialog">tydzień `,
		`<button type="button" class="day-pill has sel today" aria-pressed="true" aria-label="Cz 19: czwartek, 19 listopada">`,
		`<div class="ev-row"><div class="ev-time"><b>`,
		`<button type="button" class="ev-card tint"><div class="ev-subject">`,
		`<span class="count">`,
	} {
		if !strings.Contains(page.view, want) {
			t.Errorf("widok bez %q", want)
		}
	}
	if !strings.Contains(page.info, `<h2 id="planInfoTitle">Przedmioty grupy WMT26AX3S1 w semestrze zimowym</h2>`) || !strings.Contains(page.info, "<li><b>") {
		t.Errorf("sekcja przedmiotów: %.200s", page.info)
	}
	if strings.Contains(string(page.data), "</") || len(page.data) < len(raw)/2 {
		t.Fatal("dane JSON nie są bezpieczne do wstawienia w <script>")
	}
}

func TestRenderSSRFreeDayAndOutsideSemester(t *testing.T) {
	raw, err := os.ReadFile("../../e2e/fixtures/data/WMT26AX3S1.json")
	if err != nil {
		t.Fatal(err)
	}
	sunday := time.Date(2026, 11, 22, 12, 0, 0, 0, warsawLoc)
	if page := renderSSR("WMT26AX3S1", raw, nil, sunday); page == nil || !strings.Contains(page.view, `<div class="empty-day"><b>Wolne</b>Brak zajęć tego dnia.<br><button type="button" class="link-btn">Następne zajęcia: pon. 23 lis</button>`) {
		t.Fatalf("niedziela bez zajęć: %+v", page)
	}
	summer := time.Date(2027, 4, 10, 12, 0, 0, 0, warsawLoc)
	if page := renderSSR("WMT26AX3S1", raw, nil, summer); page != nil {
		t.Fatal("w semestrze bez zajęć SSR powinien się wyłączyć")
	}
	if renderSSR("X", []byte("nie json"), nil, sunday) != nil {
		t.Fatal("zepsuty JSON powinien wyłączyć SSR")
	}
}

func TestSSRHelpersMatchFrontend(t *testing.T) {
	cases := map[string]string{"2026-11-19": "zima", "2027-02-21": "zima", "2027-02-22": "lato", "2027-07-30": "lato", "2027-08-01": "zima"}
	for iso, want := range cases {
		if got := ssrSemester(iso); got != want {
			t.Errorf("ssrSemester(%s) = %s, chcę %s", iso, got, want)
		}
	}
	if got := ssrWeekNumber("2026-11-16", []string{"2026-11-02", "2026-11-09", "2026-11-16", "2027-02-22"}); got != "tydzień 3/3" {
		t.Errorf("numer tygodnia %q", got)
	}
	if got := ssrFormatMinutes(95); got != "1 h 35 min" {
		t.Errorf("%q", got)
	}
	if room, more := ssrSplitRooms("233 K ; 232, 100"); room != "233 K" || more != 2 {
		t.Errorf("%q %d", room, more)
	}
}

func TestSSRChipAndStaleBanner(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, warsawLoc)
	change := func(at, since string, n int) []model.Change {
		c := model.Change{At: at, Since: since}
		for i := 0; i < n; i++ {
			c.Added = append(c.Added, model.Event{})
		}
		return []model.Change{c}
	}
	cases := []struct {
		name    string
		history []model.Change
		want    string
	}{
		{"brak historii", nil, ""},
		{"stara zmiana", change("2026-11-10 10:00:00", "", 3), ""},
		{"godzinę temu", change("2026-11-23 11:00:00", "", 1), "1 zmiana w planie · 1 godz. temu"},
		{"wczoraj", change("2026-11-22 12:00:00", "", 2), "2 zmiany w planie · wczoraj"},
		{"od dawna niesprawdzany", change("2026-11-23 11:58:00", "2026-11-20 09:00:00", 5), "5 zmian w planie · od 20 listopada"},
	}
	for _, c := range cases {
		if got := ssrChipText(c.history, now); got != c.want {
			t.Errorf("%s: %q, chcę %q", c.name, got, c.want)
		}
	}
	if got := ssrStaleBanner("2026-11-22 13:00:00", now); got != "" {
		t.Errorf("pobrany 23 h temu nie jest nieaktualny: %q", got)
	}
	if got := ssrStaleBanner("2026-11-21 09:05:00", now); !strings.HasPrefix(got, "Plan pobrany 21 listopada o 09:05") {
		t.Errorf("baner: %q", got)
	}
	raw, _ := os.ReadFile("../../e2e/fixtures/data/WMT26AX3S1.json")
	page := renderSSR("WMT26AX3S1", raw, change("2026-11-19 09:00:00", "", 2), time.Date(2026, 11, 19, 10, 15, 0, 0, warsawLoc))
	if !page.chip || !strings.Contains(page.view, `</p></div><button id="changeChip" class="change-chip" type="button"><span class="pulse"></span>2 zmiany w planie · 1 godz. temu</button><div class="week-strip">`) {
		t.Fatalf("pasek zmian nie jest pod tytułem dnia: %.400s", page.view)
	}
}

func TestSSRMarksLiveClass(t *testing.T) {
	raw, _ := os.ReadFile("../../e2e/fixtures/data/WMT26AX3S1.json")
	during := time.Date(2026, 11, 19, 14, 0, 0, 0, warsawLoc)
	page := renderSSR("WMT26AX3S1", raw, nil, during)
	if !strings.Contains(page.view, `<button type="button" class="ev-card tint live"><div class="ev-subject">Matematyka 2</div>`) || !strings.Contains(page.view, `<span class="remaining">zostało 1 h 5 min</span>`) {
		t.Fatalf("trwające zajęcia bez paska: %.600s", page.view)
	}
	if strings.Count(page.view, " live\"") != 1 {
		t.Fatalf("tylko jedne zajęcia mogą trwać")
	}
	before := renderSSR("WMT26AX3S1", raw, nil, time.Date(2026, 11, 19, 10, 15, 0, 0, warsawLoc))
	if strings.Contains(before.view, " live\"") || !strings.Contains(before.view, `<div class="ev-live">`) {
		t.Fatal("przed zajęciami: bez klasy live, ale ze strukturą paska jak w JS")
	}
}
