package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/store"
)

func calendarHandler(t *testing.T) http.Handler {
	t.Helper()
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	events := []model.Event{
		{Date: "2026-11-20", Start: "08:00", End: "09:35", Subject: "Matematyka 2", Type: "wykład", Teacher: "dr hab. Kowalska Anna", Room: "19 36"},
		{Date: "2026-11-19", Start: "13:30", End: "15:05", Subject: "Matematyka 2", Type: "wykład", Room: "19 36"},
		{Date: "2026-11-19", Start: "16:00", End: "17:35", Subject: "Język obcy, poziom B2; grupa \\ 2", Type: "ćwiczenia", Room: "233 K"},
	}
	if err := st.Save("WMT26AX3S1", model.Plan{Events: events, FetchedAt: "2026-11-19 10:00:00", ModifiedAt: "2026-11-18 12:00:00"}); err != nil {
		t.Fatal(err)
	}
	reg := config.NewRegistry([]config.PlanConfig{{ID: "WMT26AX3S1", Name: "WMT26AX3S1", Faculty: "WMT"}})
	h, err := NewServer(reg, st, nil, Options{Offline: true, CanonicalHost: "watplan.me"}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func getCal(h http.Handler, target string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = "watplan.me"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCalendarFeed(t *testing.T) {
	h := calendarHandler(t)
	rec := getCal(h, "/cal/WMT26AX3S1.ics", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/calendar") {
		t.Fatalf("Content-Type %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"BEGIN:VCALENDAR\r\n",
		"X-WR-CALNAME:WAT WMT26AX3S1\r\n",
		"REFRESH-INTERVAL;VALUE=DURATION:PT3H\r\n",
		"DTSTART;TZID=Europe/Warsaw:20261119T133000\r\n",
		"DTEND;TZID=Europe/Warsaw:20261120T093500\r\n",
		"SUMMARY:Matematyka 2 (wykład)\r\n",
		"LOCATION:Sala 19 36\r\n",
		`SUMMARY:Język obcy\, poziom B2\; grupa \\ 2 (ćwiczenia)`,
		"DTSTAMP:20261118T110000Z\r\n",
		"END:VCALENDAR\r\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("brak %q w kalendarzu", want)
		}
	}
	unfolded := strings.ReplaceAll(body, "\r\n ", "")
	if !strings.Contains(unfolded, `DESCRIPTION:Wykład 1/2\nPlan: https://watplan.me/app?plan=WMT26AX3S1`) {
		t.Errorf("pierwszy wykład nie ma numeru 1/2:\n%s", unfolded)
	}
	if !strings.Contains(unfolded, `DESCRIPTION:Wykład 2/2\nProwadzący: dr hab. Kowalska Anna`) {
		t.Errorf("drugi wykład nie ma numeru 2/2 i prowadzącego")
	}
	if strings.Contains(body, "VALARM") {
		t.Error("przypomnienie bez parametru alarm")
	}
	for _, line := range strings.Split(body, "\r\n") {
		if len(line) > 75 {
			t.Errorf("linia dłuższa niż 75 bajtów: %q", line)
		}
	}
	if strings.Count(body, "BEGIN:VEVENT") != 3 {
		t.Errorf("zajęć %d, chcę 3", strings.Count(body, "BEGIN:VEVENT"))
	}
}

func TestCalendarFeedIsStableAndCacheable(t *testing.T) {
	h := calendarHandler(t)
	first := getCal(h, "/cal/WMT26AX3S1.ics", nil)
	time.Sleep(1100 * time.Millisecond)
	second := getCal(h, "/cal/WMT26AX3S1.ics", nil)
	if first.Body.String() != second.Body.String() {
		t.Fatal("ten sam plan daje różne kalendarze")
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("brak ETag")
	}
	if rec := getCal(h, "/cal/WMT26AX3S1.ics", map[string]string{"If-None-Match": etag}); rec.Code != http.StatusNotModified {
		t.Fatalf("status %d, chcę 304", rec.Code)
	}
}

func TestCalendarFeedAlarmAndDownload(t *testing.T) {
	h := calendarHandler(t)
	rec := getCal(h, "/cal/WMT26AX3S1.ics?alarm=15&download=1", nil)
	body := rec.Body.String()
	if strings.Count(body, "BEGIN:VALARM") != 3 || !strings.Contains(body, "TRIGGER:-PT15M\r\n") {
		t.Fatalf("brak przypomnień 15 min:\n%s", body)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, `filename="plan-WMT26AX3S1.ics"`) {
		t.Fatalf("Content-Disposition %q", cd)
	}
	for _, bad := range []string{"/cal/WMT26AX3S1.ics?alarm=-5", "/cal/WMT26AX3S1.ics?alarm=abc"} {
		if rec := getCal(h, bad, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, chcę 400", bad, rec.Code)
		}
	}
	for _, missing := range []string{"/cal/NIEMA26XX1S0.ics", "/cal/WMT26AX3S1"} {
		if rec := getCal(h, missing, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, chcę 404", missing, rec.Code)
		}
	}
}

func TestICSFoldKeepsMultibyteCharacters(t *testing.T) {
	line := "SUMMARY:" + strings.Repeat("ąęł", 30)
	folded := icsFold(line)
	for _, part := range strings.Split(folded, "\r\n") {
		if len(part) > 75 {
			t.Fatalf("część dłuższa niż 75 bajtów: %d", len(part))
		}
	}
	if strings.ReplaceAll(folded, "\r\n ", "") != line {
		t.Fatal("zawinięcie zmienia treść")
	}
}
