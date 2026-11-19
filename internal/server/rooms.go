package server

import (
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/store"
)

var (
	roomRe      = regexp.MustCompile(`^(.+?)/?\s+(\d{1,3}[A-Z]?|[A-Z])$`)
	groupCodeRe = regexp.MustCompile(`^[A-Z]{3}\d{2}`)
	roomSplitRe = regexp.MustCompile(`\s*[;,]\s*`)
	clockRe     = regexp.MustCompile(`^\d{2}:\d{2}$`)
	dateRe      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

type roomKey struct {
	building string
	room     string
}

type span struct{ from, to int }

type planRooms map[string]map[roomKey][]span

type roomIndex struct {
	perPlan map[string]planRooms
	rooms   map[roomKey]int
}

type roomsCache struct {
	mu  sync.RWMutex
	idx *roomIndex
}

func parseRoom(raw string) (roomKey, bool) {
	raw = strings.TrimSpace(raw)
	m := roomRe.FindStringSubmatch(raw)
	if m == nil {
		return roomKey{}, false
	}
	room := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m[1]), "/"))
	if room == "" || len(room) > 16 || groupCodeRe.MatchString(room) || strings.ContainsAny(room, "()%") {
		return roomKey{}, false
	}
	return roomKey{building: m[2], room: room}, true
}

func clockMin(t string) int {
	if len(t) != 5 {
		return -1
	}
	h, err1 := strconv.Atoi(t[:2])
	m, err2 := strconv.Atoi(t[3:])
	if err1 != nil || err2 != nil {
		return -1
	}
	return h*60 + m
}

func minClock(m int) string {
	return strconv.Itoa(m/60/10) + strconv.Itoa(m/60%10) + ":" + strconv.Itoa(m%60/10) + strconv.Itoa(m%10)
}

func roomsOf(events []model.Event) planRooms {
	out := planRooms{}
	for _, e := range events {
		from, to := clockMin(e.Start), clockMin(e.End)
		if from < 0 || to <= from {
			continue
		}
		for _, part := range roomSplitRe.Split(e.Room, -1) {
			key, ok := parseRoom(part)
			if !ok {
				continue
			}
			day := out[e.Date]
			if day == nil {
				day = map[roomKey][]span{}
				out[e.Date] = day
			}
			day[key] = append(day[key], span{from, to})
		}
	}
	return out
}

func (idx *roomIndex) set(id string, pr planRooms) {
	for _, day := range idx.perPlan[id] {
		for key := range day {
			idx.rooms[key]--
			if idx.rooms[key] <= 0 {
				delete(idx.rooms, key)
			}
		}
	}
	for _, day := range pr {
		for key := range day {
			idx.rooms[key]++
		}
	}
	if len(pr) == 0 {
		delete(idx.perPlan, id)
		return
	}
	idx.perPlan[id] = pr
}

func buildRoomIndex(reg *config.Registry, st *store.Store) *roomIndex {
	idx := &roomIndex{perPlan: map[string]planRooms{}, rooms: map[roomKey]int{}}
	for _, p := range reg.List() {
		plan, err := st.Load(p.ID)
		if err != nil {
			continue
		}
		idx.set(p.ID, roomsOf(plan.Events))
	}
	return idx
}

func (c *roomsCache) update(id string, plan model.Plan) {
	pr := roomsOf(plan.Events)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.idx != nil {
		c.idx.set(id, pr)
	}
}

func (s *Server) withRooms(fn func(*roomIndex)) {
	s.rooms.mu.RLock()
	if s.rooms.idx != nil {
		defer s.rooms.mu.RUnlock()
		fn(s.rooms.idx)
		return
	}
	s.rooms.mu.RUnlock()
	s.rooms.mu.Lock()
	if s.rooms.idx == nil {
		s.rooms.idx = buildRoomIndex(s.registry, s.store)
	}
	s.rooms.mu.Unlock()
	s.withRooms(fn)
}

func (idx *roomIndex) busyOn(date string) map[roomKey][]span {
	busy := map[roomKey][]span{}
	for _, pr := range idx.perPlan {
		for key, spans := range pr[date] {
			busy[key] = append(busy[key], spans...)
		}
	}
	return busy
}

type freeRoom struct {
	Room      string `json:"room"`
	FreeFrom  string `json:"free_from"`
	FreeUntil string `json:"free_until"`
	Minutes   int    `json:"minutes"`
}

type freeBuilding struct {
	Building string     `json:"building"`
	Rooms    []freeRoom `json:"rooms"`
	Total    int        `json:"total"`
}

const (
	dayStart = 7*60 + 30
	dayEnd   = 21*60 + 30
)

func freeRooms(idx *roomIndex, date string, from, to int, building string) []freeBuilding {
	day := idx.busyOn(date)
	byBuilding := map[string]*freeBuilding{}
	for key := range idx.rooms {
		if building != "" && key.building != building {
			continue
		}
		b := byBuilding[key.building]
		if b == nil {
			b = &freeBuilding{Building: key.building}
			byBuilding[key.building] = b
		}
		b.Total++
		start, end := dayStart, dayEnd
		taken := false
		for _, sp := range day[key] {
			if sp.from < to && sp.to > from {
				taken = true
				break
			}
			if sp.to <= from && sp.to > start {
				start = sp.to
			}
			if sp.from >= to && sp.from < end {
				end = sp.from
			}
		}
		if taken {
			continue
		}
		b.Rooms = append(b.Rooms, freeRoom{Room: key.room, FreeFrom: minClock(start), FreeUntil: minClock(end), Minutes: end - start})
	}

	out := make([]freeBuilding, 0, len(byBuilding))
	for _, b := range byBuilding {
		if len(b.Rooms) == 0 {
			continue
		}
		sort.Slice(b.Rooms, func(i, j int) bool {
			if b.Rooms[i].Minutes != b.Rooms[j].Minutes {
				return b.Rooms[i].Minutes > b.Rooms[j].Minutes
			}
			return naturalLess(b.Rooms[i].Room, b.Rooms[j].Room)
		})
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return naturalLess(out[i].Building, out[j].Building) })
	return out
}

func naturalLess(a, b string) bool {
	na, ea := strconv.Atoi(strings.TrimRight(a, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"))
	nb, eb := strconv.Atoi(strings.TrimRight(b, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"))
	switch {
	case ea == nil && eb == nil && na != nb:
		return na < nb
	case ea == nil && eb != nil:
		return true
	case ea != nil && eb == nil:
		return false
	}
	return a < b
}

func (s *Server) handleRooms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "wymagana metoda GET")
		return
	}
	q := r.URL.Query()
	date, fromS, toS := q.Get("date"), q.Get("from"), q.Get("to")
	if !dateRe.MatchString(date) || !clockRe.MatchString(fromS) || !clockRe.MatchString(toS) {
		writeError(w, http.StatusBadRequest, "podaj date=RRRR-MM-DD, from=GG:MM i to=GG:MM")
		return
	}
	from, to := clockMin(fromS), clockMin(toS)
	if from < 0 || to <= from {
		writeError(w, http.StatusBadRequest, "godzina końca musi być po godzinie początku")
		return
	}
	var all []string
	var free []freeBuilding
	plans, hasData := 0, false
	s.withRooms(func(idx *roomIndex) {
		buildings := map[string]bool{}
		for key := range idx.rooms {
			buildings[key.building] = true
		}
		for b := range buildings {
			all = append(all, b)
		}
		free = freeRooms(idx, date, from, to, q.Get("building"))
		plans = len(idx.perPlan)
		for _, pr := range idx.perPlan {
			if len(pr[date]) > 0 {
				hasData = true
				break
			}
		}
	})
	sort.Slice(all, func(i, j int) bool { return naturalLess(all[i], all[j]) })

	writeJSON(w, http.StatusOK, map[string]any{
		"date":      date,
		"from":      fromS,
		"to":        toS,
		"free":      free,
		"buildings": all,
		"plans":     plans,
		"has_data":  hasData,
	})
}
