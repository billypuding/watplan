package wat

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/billypuding/watplan/internal/model"
)

const ParserVersion = 3

var misparsedRoomRe = regexp.MustCompile(`\b[A-Z]{3}\d{2}[A-Z]{2}\d[A-Z]\d\b|\b\d[Pp][Ll]/\d+[Kk][Pp]\b`)

var shortTeacherRe = regexp.MustCompile(`^\p{Lu}[\p{L}.]{0,6}$`)

func Misparsed(events []model.Event) bool {
	for _, e := range events {
		if misparsedRoomRe.MatchString(e.Room) {
			return true
		}
	}
	return false
}

func shortTeachers(events []model.Event) bool {
	for _, e := range events {
		for _, t := range strings.Split(e.Teacher, ", ") {
			if shortTeacherRe.MatchString(t) {
				return true
			}
		}
	}
	return false
}

func NeedsReparse(plan model.Plan) bool {
	if plan.Parser >= ParserVersion {
		return false
	}
	return Misparsed(plan.Events) || plan.Parser == 2 && shortTeachers(plan.Events)
}

var (
	plansoftSubjectRe = regexp.MustCompile(`^(.+?)\s*\(([^()]{1,3})\)$`)
	plansoftGroupRe   = regexp.MustCompile(`(?i)^[0-9a-ząćęłńóśźż]{2,}(?:/[0-9a-z]+)?$`)
	plansoftShareRe   = regexp.MustCompile(`^\d{1,3}%$`)
	plansoftMarkerRe  = regexp.MustCompile(`^(?:T\d+(?:\.\d+)?[A-ZĆŁŚ]*|WYK-\d+|[A-ZĆŁ]{1,3}-\d+)$`)
	plansoftTypes     = map[string]string{
		"w": "wykład", "W": "wykład",
		"ć": "ćwiczenia", "Ć": "ćwiczenia",
		"l": "laboratorium", "L": "laboratorium",
		"p": "projekt", "P": "projekt",
		"s": "seminarium", "S": "seminarium",
		"e": "egzamin", "E": "egzamin",
		"z": "zaliczenie", "Z": "zaliczenie",
		"i": "inne", "I": "inne",
	}
)

func isPlansoftGroupList(field string) bool {
	parts := strings.Split(field, ";")
	withDigit := 0
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !plansoftGroupRe.MatchString(p) {
			return false
		}
		if strings.ContainsAny(p, "0123456789") {
			withDigit++
		}
	}
	return withDigit > 0
}

var plansoftChunkRe = regexp.MustCompile(`\p{Lu}[\p{Ll}]*|\p{Ll}+`)

func abbrChunks(abbr string) []string {
	var out []string
	for _, part := range strings.Split(abbr, ".") {
		out = append(out, plansoftChunkRe.FindAllString(part, -1)...)
	}
	return out
}

func nameWords(name string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(name, func(r rune) bool { return r == ' ' || r == '(' || r == ')' || r == '-' }) {
		r := []rune(w)
		if len(r) >= 2 && unicode.IsUpper(r[0]) && !strings.HasSuffix(w, ".") {
			out = append(out, strings.ToLower(w))
		}
	}
	return out
}

func chunkFits(chunk, word string, loose bool) bool {
	if strings.HasPrefix(word, chunk) {
		return true
	}
	c, w := []rune(chunk), []rune(word)
	if !loose || len(c) < 2 || len(w) == 0 || w[0] != c[0] {
		return false
	}
	j := 1
	for _, r := range w[1:] {
		if j < len(c) && r == c[j] {
			j++
		}
	}
	return j == len(c)
}

func chunksMatch(chunks, words []string, used []bool, loose bool) bool {
	if len(chunks) == 0 {
		return true
	}
	c := strings.ToLower(chunks[0])
	for i, w := range words {
		if !used[i] && chunkFits(c, w, loose) {
			used[i] = true
			if chunksMatch(chunks[1:], words, used, loose) {
				return true
			}
			used[i] = false
		}
	}
	return false
}

func (ix *teacherIndex) byAbbreviation(abbr string) (string, bool) {
	chunks := abbrChunks(abbr)
	if len(chunks) == 0 {
		return "", false
	}
	seen := map[string]bool{}
	var names []string
	for _, name := range ix.exact {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for _, name := range ix.names {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if name, ok, ambiguous := matchAbbreviation(chunks, names, false); ok || ambiguous {
		return name, ok
	}
	name, ok, _ := matchAbbreviation(chunks, names, true)
	return name, ok
}

func matchAbbreviation(chunks, names []string, loose bool) (string, bool, bool) {
	found, foundKey := "", ""
	for _, name := range names {
		words := nameWords(name)
		if len(words) < len(chunks) || !chunksMatch(chunks, words, make([]bool, len(words)), loose) {
			continue
		}
		key := strings.Join(words, " ")
		if found != "" && foundKey != key {
			return "", false, true
		}
		if found == "" {
			found, foundKey = name, key
		}
	}
	return found, found != "", false
}

func looksLikePlansoftRoom(field string) bool {
	return isRoomField(field) || strings.Contains(field, "/") || strings.HasPrefix(strings.ToLower(field), "aula")
}

func parsePlansoftCell(fields []cellField, subjects map[string]string, index *teacherIndex, color string) (model.Event, bool) {
	for len(fields) > 0 && plansoftShareRe.MatchString(strings.TrimSpace(fields[0].text)) {
		fields = fields[1:]
	}
	if len(fields) < 2 {
		return model.Event{}, false
	}
	m := plansoftSubjectRe.FindStringSubmatch(fields[0].text)
	if m == nil || !isPlansoftGroupList(fields[1].text) {
		return model.Event{}, false
	}
	event := model.Event{Color: color, Subject: m[1], Type: plansoftTypes[m[2]]}
	if full := subjects[m[1]]; full != "" {
		event.Subject = full
	}
	if event.Type == "" {
		event.Type = m[2]
	}
	var teachers, rooms []string
	for _, f := range fields[2:] {
		text := strings.TrimSpace(f.text)
		if text == "" || plansoftMarkerRe.MatchString(text) || isPlansoftGroupList(text) {
			continue
		}
		if name, ok := index.match(text); ok && len(rooms) == 0 {
			teachers = appendUnique(teachers, name)
			continue
		}
		if len(teachers) == 0 && len(rooms) == 0 && !looksLikePlansoftRoom(text) {
			if name, ok := index.byAbbreviation(text); ok {
				text = name
			}
			teachers = append(teachers, text)
			continue
		}
		rooms = append(rooms, normalizeRoom(text))
	}
	event.Teacher = strings.Join(teachers, ", ")
	event.Room = strings.Join(rooms, ", ")
	return event, true
}
