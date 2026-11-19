package wat

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/billypuding/watplan/internal/config"
)

func TestPlanFromURLPDFLinks(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		src     config.Source
		wantID  string
		wantURL string
		wantOK  bool
	}{
		{
			name:    "html link",
			raw:     "https://wel.wat.edu.pl/planyzajec/zima/WEL24EX1S0.htm",
			src:     config.Source{Faculty: "WEL", StartYear: 2026},
			wantID:  "WEL24EX1S0",
			wantURL: "https://wel.wat.edu.pl/planyzajec/zima/WEL24EX1S0.htm",
			wantOK:  true,
		},
		{
			name:    "pdf index rewritten to htm",
			raw:     "https://wtc.wat.edu.pl/Plany/WTC26CX1S1.pdf",
			src:     config.Source{Faculty: "WTC", StartYear: 2026, PDFLinks: true},
			wantID:  "WTC26CX1S1",
			wantURL: "https://wtc.wat.edu.pl/Plany/WTC26CX1S1.htm",
			wantOK:  true,
		},
		{
			name:    "pdf index rewritten drops query",
			raw:     "https://wtc.wat.edu.pl/Plany/WTC26M1.pdf?x=1",
			src:     config.Source{Faculty: "WTC", StartYear: 2026, PDFLinks: true},
			wantID:  "WTC26M1",
			wantURL: "https://wtc.wat.edu.pl/Plany/WTC26M1.htm",
			wantOK:  true,
		},
		{
			name:    "wrapped postgraduate group",
			raw:     "https://www.wim.wat.edu.pl/wp-content/uploads/rozklady/zima/(WME26A1N5)_-_SP_BRDiRzS.htm",
			src:     config.Source{Faculty: "WIM", StartYear: 2026},
			wantID:  "WME26A1N5",
			wantURL: "https://www.wim.wat.edu.pl/wp-content/uploads/rozklady/zima/(WME26A1N5)_-_SP_BRDiRzS.htm",
			wantOK:  true,
		},
		{
			name:    "language course",
			raw:     "https://wel.wat.edu.pl/planyzajec/zima/1PL26KP.htm",
			src:     config.Source{Faculty: "WEL", StartYear: 2026},
			wantID:  "1PL26KP",
			wantURL: "https://wel.wat.edu.pl/planyzajec/zima/1PL26KP.htm",
			wantOK:  true,
		},
		{
			name:    "named faculty group",
			raw:     "https://wel.wat.edu.pl/planyzajec/zima/WEL_Erasmus.htm",
			src:     config.Source{Faculty: "WEL", StartYear: 2026},
			wantID:  "WEL-ERASMUS",
			wantURL: "https://wel.wat.edu.pl/planyzajec/zima/WEL_Erasmus.htm",
			wantOK:  true,
		},
		{
			name:   "named group of other faculty ignored",
			raw:    "https://wel.wat.edu.pl/planyzajec/zima/WIM_Erasmus.htm",
			src:    config.Source{Faculty: "WEL", StartYear: 2026},
			wantOK: false,
		},
		{
			name:   "teacher ignored",
			raw:    "https://www.wim.wat.edu.pl/wp-content/uploads/rozklady/zima/mgr_Gorska_Anna.htm",
			src:    config.Source{Faculty: "WIM", StartYear: 2026},
			wantOK: false,
		},
		{
			name:   "room ignored",
			raw:    "https://www.wim.wat.edu.pl/wp-content/uploads/rozklady/zima/108_54.htm",
			src:    config.Source{Faculty: "WIM", StartYear: 2026},
			wantOK: false,
		},
		{
			name:   "wrapped room ignored",
			raw:    "https://www.wim.wat.edu.pl/wp-content/uploads/rozklady/zima/(2.01)_54A.htm",
			src:    config.Source{Faculty: "WIM", StartYear: 2026},
			wantOK: false,
		},
		{
			name:   "pdf without flag ignored",
			raw:    "https://wel.wat.edu.pl/planyzajec/zima/WEL24EX1S0.pdf",
			src:    config.Source{Faculty: "WEL", StartYear: 2026},
			wantOK: false,
		},
		{
			name:   "teacher pdf ignored",
			raw:    "https://wtc.wat.edu.pl/Plany/dr_hab_Perkowski_Pawel.pdf",
			src:    config.Source{Faculty: "WTC", StartYear: 2026, PDFLinks: true},
			wantOK: false,
		},
		{
			name:   "room pdf ignored",
			raw:    "https://wtc.wat.edu.pl/Plany/027_24.pdf",
			src:    config.Source{Faculty: "WTC", StartYear: 2026, PDFLinks: true},
			wantOK: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u, err := url.Parse(c.raw)
			if err != nil {
				t.Fatalf("url.Parse: %v", err)
			}
			plan, ok := planFromURL(u, c.src)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, chcę %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if plan.ID != c.wantID {
				t.Errorf("ID = %q, chcę %q", plan.ID, c.wantID)
			}
			if c.wantURL != "" && plan.URL != c.wantURL {
				t.Errorf("URL = %q, chcę %q", plan.URL, c.wantURL)
			}
			if plan.Faculty != c.src.Faculty {
				t.Errorf("Faculty = %q, chcę %q", plan.Faculty, c.src.Faculty)
			}
		})
	}
}

func TestDiscoverFallsBackToSiblingIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/planzajec/zima/index.html" {
			_, _ = w.Write([]byte(`<a href="WLO26AX1S1.htm">WLO26AX1S1</a>`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	plans, err := f.DiscoverPlans(config.Source{Faculty: "WLO", IndexURL: srv.URL + "/planzajec/zima/index.xml", StartYear: 2026})
	if err != nil {
		t.Fatalf("DiscoverPlans: %v", err)
	}
	if len(plans) != 1 || plans[0].ID != "WLO26AX1S1" || plans[0].URL != srv.URL+"/planzajec/zima/WLO26AX1S1.htm" {
		t.Fatalf("plany %+v", plans)
	}

	_, err = f.DiscoverPlans(config.Source{Faculty: "WLO", IndexURL: srv.URL + "/inne/index.xml", StartYear: 2026})
	if !errors.Is(err, ErrGone) {
		t.Fatalf("bez obu plików chcę ErrGone, mam %v", err)
	}
}
