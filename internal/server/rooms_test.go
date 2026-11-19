package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/store"
)

func TestParseRoom(t *testing.T) {
	cases := map[string]roomKey{
		"19 36":     {"36", "19"},
		"233 K":     {"K", "233"},
		"51/ 62":    {"62", "51"},
		"aula/ 135": {"135", "aula"},
		"3.8 75":    {"75", "3.8"},
		"4 63A":     {"63A", "4"},
		"S2 72":     {"72", "S2"},
	}
	for raw, want := range cases {
		got, ok := parseRoom(raw)
		if !ok || got != want {
			t.Errorf("%q = %+v %v, chcę %+v", raw, got, ok, want)
		}
	}
	for _, bad := range []string{"ONLINE", "12", "M1(w)", "50%", "WLO26FW4S0 1", "Inauguracja WEL 6.10"} {
		if got, ok := parseRoom(bad); ok {
			t.Errorf("%q rozpoznane jako %+v", bad, got)
		}
	}
}

func TestRoomsEndpoint(t *testing.T) {
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	day := "2026-11-20"
	_ = st.Save("A", model.Plan{Events: []model.Event{
		{Date: day, Start: "08:00", End: "09:35", Room: "19 36"},
		{Date: day, Start: "13:30", End: "15:05", Room: "19 36"},
		{Date: day, Start: "09:50", End: "11:25", Room: "20 36; 233 K"},
	}})
	_ = st.Save("B", model.Plan{Events: []model.Event{
		{Date: "2026-11-21", Start: "08:00", End: "09:35", Room: "21 36"},
	}})
	reg := config.NewRegistry([]config.PlanConfig{{ID: "A"}, {ID: "B"}})
	h, err := NewServer(reg, st, nil, Options{Offline: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/rooms?date="+day+"&from=09:50&to=11:25", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var resp struct {
		Free      []freeBuilding `json:"free"`
		Buildings []string       `json:"buildings"`
		HasData   bool           `json:"has_data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.HasData || len(resp.Buildings) != 2 || resp.Buildings[0] != "36" || resp.Buildings[1] != "K" {
		t.Fatalf("budynki %v, dane %v", resp.Buildings, resp.HasData)
	}
	if len(resp.Free) != 1 || resp.Free[0].Building != "36" {
		t.Fatalf("wolne %+v", resp.Free)
	}
	rooms := resp.Free[0].Rooms
	if len(rooms) != 2 || rooms[0].Room != "21" || rooms[1].Room != "19" {
		t.Fatalf("sale w bud. 36: %+v", rooms)
	}
	if rooms[1].FreeFrom != "09:35" || rooms[1].FreeUntil != "13:30" || rooms[0].FreeFrom != "07:30" {
		t.Fatalf("okna wolnego czasu: %+v", rooms)
	}

	for _, bad := range []string{"/api/rooms?date=x&from=09:50&to=11:25", "/api/rooms?date=" + day + "&from=11:25&to=09:50"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, bad, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", bad, rec.Code)
		}
	}
}
