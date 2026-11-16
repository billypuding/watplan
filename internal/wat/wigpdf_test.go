package wat

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/billypuding/watplan/internal/model"
)

const wigFixture = "testdata/wig_WIG26BX1S1.pdf"

func parseWIGFixture(t *testing.T) []model.Event {
	t.Helper()
	f, err := os.Open(wigFixture)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	events, err := Parse(FormatWIGPDF, f, 2026)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return events
}

func TestParseWIGMatchesLegendHours(t *testing.T) {
	events := parseWIGFixture(t)
	if len(events) != 217 {
		t.Fatalf("zdarzeń = %d, chcę 217", len(events))
	}

	hours := map[string]int{}
	for _, e := range events {
		hours[e.Subject+"|"+e.Type+"|"+e.Teacher] += 2
	}
	want := map[string]int{
		"Etyka zawodowa|wykład|dr Ptaszek Stanisław":                                                             14,
		"Etyka zawodowa|ćwiczenia|dr Ptaszek Stanisław":                                                          4,
		"Matematyka 1|wykład|dr Jakubowska Matylda":                                                              30,
		"Matematyka 1|ćwiczenia|dr Jakubowska Matylda":                                                           38,
		"Matematyka 2|wykład|dr Jakubowska Matylda":                                                              34,
		"Matematyka 2|ćwiczenia|dr Jakubowska Matylda":                                                           34,
		"Wprowadzenie do informatyki|wykład|dr inż. Chaładyniak Dariusz":                                         16,
		"Wprowadzenie do informatyki|laboratorium|dr inż. Chaładyniak Dariusz":                                   22,
		"Podstawy grafiki inżynierskiej|wykład|dr inż. (prof. ucz.) Mierzyński Janusz":                           14,
		"Podstawy grafiki inżynierskiej|ćwiczenia|dr inż. (prof. ucz.) Mierzyński Janusz":                        18,
		"Podstawy grafiki inżynierskiej|ćwiczenia|dr inż. (prof. ucz.) Mierzyński Janusz, mgr inż. Siwek Konrad": 6,
		"Podstawy zarządzania i przedsiębiorczości|wykład|dr hab. Panek Joanna":                                  18,
		"Podstawy zarządzania i przedsiębiorczości|ćwiczenia|dr inż. Staruch Marcin":                             16,
		"Wprowadzenie do metrologii|wykład|mgr inż. Wasielczuk Mieczysław":                                       18,
		"Wprowadzenie do metrologii|ćwiczenia|mgr inż. Wasielczuk Mieczysław":                                    20,
		"Wychowanie fizyczne|ćwiczenia|mgr Olszewski Radosław":                                                   30,
		"Język angielski|ćwiczenia|":                                                                             30,
		"Inauguracja|inne|":                                                                                      4,
	}
	for k, v := range want {
		if hours[k] != v {
			t.Errorf("%s: %d h, chcę %d h", k, hours[k], v)
		}
	}
}

func TestParseWIGEventDetails(t *testing.T) {
	events := parseWIGFixture(t)
	find := func(date, start, subject string) *model.Event {
		for i := range events {
			if e := &events[i]; e.Date == date && e.Start == start && e.Subject == subject {
				return e
			}
		}
		return nil
	}

	pgi := find("2026-11-30", "08:00", "Podstawy grafiki inżynierskiej")
	if pgi == nil {
		t.Fatal("brak PGI 30.11 o 08:00")
	}
	if pgi.End != "09:35" || pgi.Type != "ćwiczenia" || pgi.Room != "29 54, 30 54" || pgi.Color != "#FFFF80" ||
		pgi.Teacher != "dr inż. (prof. ucz.) Mierzyński Janusz, mgr inż. Siwek Konrad" {
		t.Errorf("PGI: %+v", *pgi)
	}

	split := find("2026-10-07", "09:50", "Wprowadzenie do informatyki")
	if split == nil || split.Teacher != "dr inż. Chaładyniak Dariusz" || split.Room != "118 57" {
		t.Errorf("zajęcia przecięte końcem strony: %+v", split)
	}

	lang := find("2026-10-07", "19:40", "Język angielski")
	if lang == nil || strings.HasSuffix(lang.Room, " 12") || !strings.HasPrefix(lang.Room, "134 K, 133 K") {
		t.Errorf("lektorat: %+v", lang)
	}

	if jan := find("2027-01-25", "08:00", "Matematyka 1"); jan != nil && jan.Date[:4] != "2027" {
		t.Errorf("styczeń w złym roku: %+v", jan)
	}
	for _, e := range events {
		if e.Date[5:7] == "01" && e.Date[:4] != "2027" {
			t.Fatalf("styczeń w złym roku: %+v", e)
		}
	}
}

func TestParseWIGRejectsOtherFiles(t *testing.T) {
	if _, err := Parse(FormatWIGPDF, strings.NewReader("<html>nie PDF</html>"), 2026); err == nil {
		t.Fatal("chcę błąd dla pliku, który nie jest PDF-em")
	}
	if _, err := Parse(FormatWIGPDF, strings.NewReader("%PDF-1.4 uszkodzony"), 2026); err == nil {
		t.Fatal("chcę błąd dla uszkodzonego PDF-a")
	}
}

func TestFetchKeepsPDFBytes(t *testing.T) {
	pdf, err := os.ReadFile(wigFixture)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(pdf)
	}))
	t.Cleanup(srv.Close)

	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	body, err := f.FetchHTML(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, pdf) {
		t.Fatalf("pobrany PDF różni się od oryginału (%d B zamiast %d B)", len(body), len(pdf))
	}
}
