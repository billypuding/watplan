package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/store"
)

func TestMakeupFindsSameClassInCohort(t *testing.T) {
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lab := func(date, start, room string) model.Event {
		return model.Event{Date: date, Start: start, End: "09:35", Subject: "Fizyka", Type: "laboratorium", Room: room}
	}
	_ = st.Save("WMT26AX1S1", model.Plan{Events: []model.Event{lab("2026-11-02", "08:00", "1 36"), lab("2026-11-09", "08:00", "1 36")}})
	_ = st.Save("WMT26AX2S1", model.Plan{Events: []model.Event{lab("2026-11-11", "11:40", "2 36"), lab("2026-11-04", "13:30", "2 36")}})
	_ = st.Save("WMT26AX3S1", model.Plan{Events: []model.Event{lab("2026-11-05", "16:00", "3 36")}})
	_ = st.Save("WMT25AX1S1", model.Plan{Events: []model.Event{lab("2026-11-03", "08:00", "9 36"), lab("2026-11-10", "08:00", "9 36")}})
	_ = st.Save("WEL26AX1S1", model.Plan{Events: []model.Event{lab("2026-11-03", "08:00", "8 36"), lab("2026-11-10", "08:00", "8 36")}})
	reg := config.NewRegistry([]config.PlanConfig{
		{ID: "WMT26AX1S1", Name: "WMT26AX1S1", Faculty: "WMT"},
		{ID: "WMT26AX2S1", Name: "WMT26AX2S1", Faculty: "WMT"},
		{ID: "WMT26AX3S1", Name: "WMT26AX3S1", Faculty: "WMT"},
		{ID: "WMT25AX1S1", Name: "WMT25AX1S1", Faculty: "WMT"},
		{ID: "WEL26AX1S1", Name: "WEL26AX1S1", Faculty: "WEL"},
	})
	h, err := NewServer(reg, st, nil, Options{Offline: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	q := url.Values{"plan": {"WMT26AX1S1"}, "subject": {"Fizyka"}, "type": {"laboratorium"}, "n": {"2"}}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/makeup?"+q.Encode(), nil))
	var resp struct {
		Cohort  string        `json:"cohort"`
		Matches []makeupMatch `json:"matches"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	if resp.Cohort != "WMT26" || len(resp.Matches) != 1 {
		t.Fatalf("odpowiedź %+v", resp)
	}
	if m := resp.Matches[0]; m.Group != "WMT26AX2S1" || m.Date != "2026-11-11" || m.Room != "2 36" {
		t.Fatalf("drugie laboratorium w innej grupie: %+v", m)
	}

	bad := httptest.NewRecorder()
	h.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/api/makeup?plan=WMT26AX1S1&subject=Fizyka&n=0", nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("n=0: status %d", bad.Code)
	}
}
