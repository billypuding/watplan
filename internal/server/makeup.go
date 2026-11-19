package server

import (
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/billypuding/watplan/internal/model"
)

var cohortRe = regexp.MustCompile(`^([A-Z]{3}\d{2})`)

type makeupMatch struct {
	Group   string `json:"group"`
	Date    string `json:"date"`
	Start   string `json:"start"`
	End     string `json:"end"`
	Room    string `json:"room,omitempty"`
	Teacher string `json:"teacher,omitempty"`
}

func nthOccurrence(events []model.Event, subject, kind string, n int) (model.Event, bool) {
	sorted := append([]model.Event(nil), events...)
	model.SortEvents(sorted)
	count := 0
	for _, e := range sorted {
		if e.Subject == subject && e.Type == kind {
			count++
			if count == n {
				return e, true
			}
		}
	}
	return model.Event{}, false
}

func (s *Server) handleMakeup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "wymagana metoda GET")
		return
	}
	q := r.URL.Query()
	plan, ok := s.registry.Get(q.Get("plan"))
	if !ok {
		writeError(w, http.StatusNotFound, "nie znaleziono planu")
		return
	}
	subject, kind := q.Get("subject"), q.Get("type")
	n, err := strconv.Atoi(q.Get("n"))
	if subject == "" || err != nil || n < 1 || n > 500 {
		writeError(w, http.StatusBadRequest, "podaj subject, type i n (numer zajęć)")
		return
	}
	m := cohortRe.FindStringSubmatch(strings.ToUpper(plan.ID))
	if m == nil {
		writeJSON(w, http.StatusOK, map[string]any{"cohort": "", "matches": []makeupMatch{}})
		return
	}
	cohort := m[1]

	matches := []makeupMatch{}
	checked := 0
	for _, p := range s.registry.List() {
		if p.ID == plan.ID || p.Faculty != plan.Faculty || !strings.HasPrefix(strings.ToUpper(p.ID), cohort) {
			continue
		}
		stored, err := s.store.Load(p.ID)
		if err != nil {
			continue
		}
		checked++
		if e, ok := nthOccurrence(stored.Events, subject, kind, n); ok {
			matches = append(matches, makeupMatch{Group: p.Name, Date: e.Date, Start: e.Start, End: e.End, Room: e.Room, Teacher: e.Teacher})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		return a.Group < b.Group
	})
	writeJSON(w, http.StatusOK, map[string]any{"cohort": cohort, "checked": checked, "matches": matches})
}
