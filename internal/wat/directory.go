package wat

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/billypuding/watplan/internal/model"
)

type Directory struct {
	names []string
}

func NewDirectory(names []string) *Directory {
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] || !strings.Contains(n, " ") || len(nameWords(n)) < 2 {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	return &Directory{names: out}
}

func TeacherNames(events []model.Event) []string {
	var out []string
	for _, e := range events {
		for _, t := range strings.Split(e.Teacher, ", ") {
			if strings.Contains(t, " ") {
				out = append(out, t)
			}
		}
	}
	return out
}

func (d *Directory) Resolve(abbr string) (string, bool) {
	if d == nil || !shortTeacherRe.MatchString(abbr) {
		return "", false
	}
	chunks := abbrChunks(abbr)
	if len(chunks) == 0 || len(chunks) == 1 && utf8.RuneCountInString(chunks[0]) < 4 {
		return "", false
	}
	name, ok, _ := matchAbbreviation(chunks, d.names, false)
	return name, ok
}

func ResolveTeachers(events []model.Event, d *Directory) ([]model.Event, int) {
	if d == nil || len(d.names) == 0 {
		return events, 0
	}
	var out []model.Event
	changed := 0
	for i, e := range events {
		parts := strings.Split(e.Teacher, ", ")
		hit := false
		for j, t := range parts {
			if name, ok := d.Resolve(t); ok {
				parts[j] = name
				hit = true
			}
		}
		if !hit {
			continue
		}
		if out == nil {
			out = append([]model.Event(nil), events...)
		}
		out[i].Teacher = strings.Join(parts, ", ")
		changed++
	}
	if out == nil {
		return events, 0
	}
	return out, changed
}
