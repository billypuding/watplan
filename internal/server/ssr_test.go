package server

import (
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
	page := renderSSR("WMT26AX3S1", raw, now)
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
	if page := renderSSR("WMT26AX3S1", raw, sunday); page == nil || !strings.Contains(page.view, `<div class="empty-day"><b>Wolne</b>Brak zajęć tego dnia.<br><button type="button" class="link-btn">Następne zajęcia: pon. 23 lis</button>`) {
		t.Fatalf("niedziela bez zajęć: %+v", page)
	}
	summer := time.Date(2027, 4, 10, 12, 0, 0, 0, warsawLoc)
	if page := renderSSR("WMT26AX3S1", raw, summer); page != nil {
		t.Fatal("w semestrze bez zajęć SSR powinien się wyłączyć")
	}
	if renderSSR("X", []byte("nie json"), sunday) != nil {
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
