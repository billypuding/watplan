package server

import (
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/store"
)

const (
	semesterTTL       = time.Hour
	semesterGapDays   = 21
	semesterWeekShare = 0.5
	semesterDayShare  = 0.10
)

type semesterInfo struct {
	Name  string   `json:"name"`
	Years string   `json:"years"`
	Start string   `json:"start"`
	End   string   `json:"end"`
	Weeks []string `json:"weeks"`
}

type semesterCache struct {
	mu    sync.Mutex
	list  []semesterInfo
	built time.Time
}

func mondayOf(d time.Time) time.Time {
	wd := (int(d.Weekday()) + 6) % 7
	return d.AddDate(0, 0, -wd)
}

func buildSemesters(reg *config.Registry, st *store.Store) []semesterInfo {
	weekPlans := map[string]int{}
	dayPlans := map[string]int{}
	var planWeeks []map[string]bool
	plans := 0
	for _, p := range reg.List() {
		stored, err := st.Load(p.ID)
		if err != nil || len(stored.Events) == 0 {
			continue
		}
		plans++
		weeks := map[string]bool{}
		days := map[string]bool{}
		for _, e := range stored.Events {
			d, err := time.Parse("2006-01-02", e.Date)
			if err != nil {
				continue
			}
			weeks[mondayOf(d).Format("2006-01-02")] = true
			days[e.Date] = true
		}
		for w := range weeks {
			weekPlans[w]++
		}
		planWeeks = append(planWeeks, weeks)
		for d := range days {
			dayPlans[d]++
		}
	}
	if plans == 0 {
		return nil
	}

	active := func(week string) int {
		center, _ := time.Parse("2006-01-02", week)
		n := 0
		for _, set := range planWeeks {
			for d := -28; d <= 35; d += 7 {
				if set[center.AddDate(0, 0, d).Format("2006-01-02")] {
					n++
					break
				}
			}
		}
		return n
	}
	var weeks []string
	for w, n := range weekPlans {
		near := active(w)
		if float64(near) >= 0.05*float64(plans) && float64(n) >= semesterWeekShare*float64(near) {
			weeks = append(weeks, w)
		}
	}
	sort.Strings(weeks)

	var segments [][]string
	for _, w := range weeks {
		if n := len(segments); n > 0 {
			last := segments[n-1][len(segments[n-1])-1]
			a, _ := time.Parse("2006-01-02", last)
			b, _ := time.Parse("2006-01-02", w)
			if b.Sub(a).Hours()/24 <= semesterGapDays {
				segments[n-1] = append(segments[n-1], w)
				continue
			}
		}
		segments = append(segments, []string{w})
	}

	out := make([]semesterInfo, 0, len(segments))
	for _, seg := range segments {
		first, _ := time.Parse("2006-01-02", seg[0])
		last, _ := time.Parse("2006-01-02", seg[len(seg)-1])
		start, end := seg[0], last.AddDate(0, 0, 6).Format("2006-01-02")
		segPlans := float64(active(seg[len(seg)/2]))
		for d := first; !d.After(first.AddDate(0, 0, 6)); d = d.AddDate(0, 0, 1) {
			if float64(dayPlans[d.Format("2006-01-02")]) >= semesterDayShare*segPlans {
				start = d.Format("2006-01-02")
				break
			}
		}
		for d := last.AddDate(0, 0, 6); !d.Before(last); d = d.AddDate(0, 0, -1) {
			if float64(dayPlans[d.Format("2006-01-02")]) >= semesterDayShare*segPlans {
				end = d.Format("2006-01-02")
				break
			}
		}
		name, year := "Semestr letni", first.Year()-1
		if first.Month() >= time.August || first.Month() == time.January {
			name = "Semestr zimowy"
			if first.Month() >= time.August {
				year = first.Year()
			}
		}
		out = append(out, semesterInfo{
			Name:  name,
			Years: strconv.Itoa(year) + "/" + strconv.Itoa((year+1)%100),
			Start: start,
			End:   end,
			Weeks: seg,
		})
	}
	return out
}

func pickSemester(list []semesterInfo, today string) (semesterInfo, bool) {
	if len(list) == 0 {
		return semesterInfo{}, false
	}
	for _, s := range list {
		if today <= s.End {
			return s, true
		}
	}
	return list[len(list)-1], true
}

func (s *Server) semesters() []semesterInfo {
	s.semester.mu.Lock()
	defer s.semester.mu.Unlock()
	if s.semester.list == nil || time.Since(s.semester.built) > semesterTTL {
		s.semester.list = buildSemesters(s.registry, s.store)
		s.semester.built = time.Now()
	}
	return s.semester.list
}

func (s *Server) handleSemester(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "wymagana metoda GET")
		return
	}
	today := time.Now().In(warsawLoc).Format("2006-01-02")
	if d := r.URL.Query().Get("today"); dateRe.MatchString(d) {
		today = d
	}
	sem, ok := pickSemester(s.semesters(), today)
	if !ok {
		writeError(w, http.StatusNotFound, "brak danych o semestrze")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=600")
	writeJSON(w, http.StatusOK, sem)
}
