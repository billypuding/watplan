package wat

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
)

func parseWCYFixture(t *testing.T) []model.Event {
	t.Helper()
	f, err := os.Open("testdata/wcy_WME26BX2S1.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	events, err := Parse(FormatWCY, f, 2026)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return events
}

func TestParseWCYCounts(t *testing.T) {
	events := parseWCYFixture(t)
	if len(events) != 233 {
		t.Fatalf("zdarzeń = %d, chcę 233", len(events))
	}

	byType := map[string]int{}
	for _, e := range events {
		byType[e.Type]++
		if e.Subject == "" || e.Subject == "-" {
			t.Errorf("%s %s: pusty przedmiot", e.Date, e.Start)
		}
	}
	want := map[string]int{
		"wykład":                91,
		"ćwiczenia":             116,
		"laboratorium":          11,
		"zaliczenie poprawkowe": 11,
		"egzamin":               2,
		"egzamin poprawkowy":    2,
	}
	for k, v := range want {
		if byType[k] != v {
			t.Errorf("typ %q: %d, chcę %d", k, byType[k], v)
		}
	}
}

func TestParseWCYEventDetails(t *testing.T) {
	events := parseWCYFixture(t)
	find := func(date, start, subject string) *model.Event {
		for i := range events {
			e := &events[i]
			if e.Date == date && e.Start == start && e.Subject == subject {
				return e
			}
		}
		return nil
	}

	math := find("2026-10-12", "09:50", "Matematyka 1")
	if math == nil {
		t.Fatal("brak Matematyki 1 w poniedziałek 12.10 o 09:50")
	}
	if math.End != "11:25" || math.Type != "wykład" || math.Teacher != "Ploch Jerzy" || math.Room != "301 S" || math.Color != "#CD5C5C" {
		t.Errorf("Matematyka 1: %+v", *math)
	}

	lang := find("2026-10-08", "16:00", "Język obcy (II) międzywydziałowy")
	if lang == nil {
		t.Fatal("brak lektoratu 08.10 o 16:00")
	}
	if lang.Room != "233 K, 232 K" || lang.Teacher != "" || lang.Type != "ćwiczenia" {
		t.Errorf("lektorat: %+v", *lang)
	}

	if lab := find("2026-10-28", "16:00", "Wprowadzenie do informatyki"); lab == nil || lab.Type != "laboratorium" || lab.Room != "53 62" {
		t.Errorf("laboratorium: %+v", lab)
	}
}

func TestParseWCYWeekendBlockTimes(t *testing.T) {
	for _, e := range parseWCYFixture(t) {
		day, err := time.Parse("2006-01-02", e.Date)
		if err != nil {
			t.Fatal(err)
		}
		weekend := day.Weekday() == time.Saturday || day.Weekday() == time.Sunday
		if weekend && (e.Start == "16:00" || e.Start == "17:50" || e.Start == "19:40") {
			t.Errorf("%s (weekend): godzina dnia powszedniego %s", e.Date, e.Start)
		}
		if !weekend && (e.Start == "15:20" || e.Start == "17:10" || e.Start == "19:00") {
			t.Errorf("%s (dzień powszedni): godzina weekendowa %s", e.Date, e.Start)
		}
	}
}

func TestParseWCYRejectsOtherPages(t *testing.T) {
	if _, err := Parse(FormatWCY, strings.NewReader("<html><body>Brak</body></html>"), 2026); err == nil {
		t.Fatal("chcę błąd dla strony bez rozkładu")
	}
}

func TestDiscoverWCYFiltersByPrefix(t *testing.T) {
	page, err := os.ReadFile("testdata/wcy_WME26BX2S1.html")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	}))
	t.Cleanup(srv.Close)

	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		prefix string
		want   int
	}{{"WCY", 162}, {"WME", 16}, {"", 178}}
	for _, c := range cases {
		plans, err := f.DiscoverPlans(config.Source{Faculty: "WCY", IndexURL: srv.URL + "/pl/rozklad", Format: FormatWCY, Prefix: c.prefix, StartYear: 2026})
		if err != nil {
			t.Fatalf("prefix %q: %v", c.prefix, err)
		}
		if len(plans) != c.want {
			t.Errorf("prefix %q: planów %d, chcę %d", c.prefix, len(plans), c.want)
		}
		for _, p := range plans {
			if p.ID == "WCY26" || p.ID == "WCY25" {
				t.Errorf("prefix %q: ucięty kod podgrupy %q", c.prefix, p.ID)
			}
			if !strings.HasPrefix(p.ID, c.prefix) || p.Format != FormatWCY || p.URL != srv.URL+"/pl/rozklad?grupa_id="+p.ID {
				t.Errorf("prefix %q: zły plan %+v", c.prefix, p)
				break
			}
		}
	}
}
