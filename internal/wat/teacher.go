package wat

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

type teacherEntry struct {
	norm string
	name string
}

type teacherIndex struct {
	exact         map[string]string
	norm          map[string]string
	normAmbiguous map[string]bool
	entries       []teacherEntry
	byKey         map[string]string
	ambiguous     map[string]bool
}

var polishFold = strings.NewReplacer(
	"ą", "a", "ć", "c", "ę", "e", "ł", "l", "ń", "n",
	"ó", "o", "ś", "s", "ź", "z", "ż", "z",
)

var titleTokens = map[string]struct{}{
	"dr": {}, "inz": {}, "mgr": {}, "prof": {}, "hab": {}, "doc": {},
	"nadzw": {}, "wat": {}, "dyd": {}, "lek": {}, "zw": {}, "st": {},
	"wykl": {}, "lic": {}, "tech": {}, "plk": {}, "pplk": {}, "mjr": {},
	"kpt": {}, "por": {}, "ppor": {}, "gen": {}, "bryg": {}, "kmdr": {},
	"cz": {}, "chor": {}, "ks": {}, "adiunkt": {}, "rez": {},
}

var (
	initialsRe    = regexp.MustCompile(`^\p{Lu}\.\s*(\p{Lu}\.\s*)?\p{Lu}[\p{L}\-]+$|^\p{Lu}[\p{L}\-]+\s+\p{Lu}\.(\s*\p{Lu}\.)?$`)
	personTokenRe = regexp.MustCompile(`^\p{Lu}[\p{L}'\-]*\.?$`)
	splitRe       = regexp.MustCompile(`\s*[;,/]\s*|\s+i\s+`)
)

func normKey(s string) string {
	s = polishFold.Replace(strings.ToLower(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isTeacherName(name string) bool {
	tokens := rawTokens(name)
	if len(tokens) == 0 {
		return false
	}
	_, ok := titleTokens[strings.Trim(tokens[0], "-")]
	return ok
}

func looksLikePersonName(name string) bool {
	fields := strings.Fields(name)
	if len(fields) < 2 || len(fields) > 4 {
		return false
	}
	long := 0
	for _, f := range fields {
		if !personTokenRe.MatchString(f) {
			return false
		}
		letters := strings.TrimRight(f, ".")
		size := len([]rune(letters))
		if size >= 2 && letters == strings.ToUpper(letters) {
			return false
		}
		if size >= 3 {
			long++
		}
	}
	return long >= 1
}

func isTeacherEntry(abbr, name string, evidence map[string]*abbrEvidence) bool {
	if isTeacherName(name) {
		return true
	}
	if !looksLikePersonName(name) {
		return false
	}
	ev := evidence[normKey(abbr)]
	if ev == nil {
		return false
	}
	return ev.bold || (ev.leading > 0 && ev.other == 0)
}

func rawTokens(s string) []string {
	s = polishFold.Replace(strings.ToLower(s))
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && r != '-'
	})
}

func personTokens(s string) []string {
	raw := rawTokens(s)
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.Trim(t, "-")
		if t == "" {
			continue
		}
		if _, isTitle := titleTokens[t]; isTitle {
			continue
		}
		out = append(out, t)
	}
	return out
}

func personKeys(tokens []string, forValue bool) []string {
	if len(tokens) == 0 {
		return nil
	}
	sorted := append([]string(nil), tokens...)
	sort.Strings(sorted)
	keys := []string{"full:" + strings.Join(sorted, " ")}
	if len(tokens) >= 2 {
		for i, t := range tokens {
			if len([]rune(t)) < 2 {
				continue
			}
			initials := make([]string, 0, len(tokens)-1)
			for j, o := range tokens {
				if j != i {
					initials = append(initials, string([]rune(o)[0]))
				}
			}
			sort.Strings(initials)
			keys = append(keys, "si:"+t+"|"+strings.Join(initials, ""))
			if forValue && len([]rune(t)) >= 3 {
				keys = append(keys, "sn:"+t)
			}
		}
	} else if !forValue && len([]rune(tokens[0])) >= 3 {
		keys = append(keys, "sn:"+tokens[0])
	}
	return keys
}

func newTeacherIndex(teachers map[string]string) *teacherIndex {
	ix := &teacherIndex{
		exact:         make(map[string]string, len(teachers)),
		norm:          make(map[string]string, len(teachers)),
		normAmbiguous: make(map[string]bool),
		byKey:         make(map[string]string, len(teachers)*4),
		ambiguous:     make(map[string]bool),
	}
	for abbr, value := range teachers {
		abbr = cleanText(abbr)
		name := cleanText(value)
		if abbr == "" || name == "" {
			continue
		}
		ix.exact[abbr] = name

		if n := normKey(abbr); n != "" {
			if prev, ok := ix.norm[n]; ok && prev != name {
				ix.normAmbiguous[n] = true
			} else {
				ix.norm[n] = name
			}
			ix.entries = append(ix.entries, teacherEntry{norm: n, name: name})
		}

		for _, key := range personKeys(personTokens(name), true) {
			if existing, ok := ix.byKey[key]; ok && existing != name {
				ix.ambiguous[key] = true
				continue
			}
			ix.byKey[key] = name
		}
	}
	return ix
}

func (ix *teacherIndex) strong(field string) (string, bool) {
	code := strings.TrimRight(cleanText(field), "; ,")
	if code == "" {
		return "", false
	}
	if name, ok := ix.exact[code]; ok {
		return name, true
	}
	n := normKey(code)
	if n == "" || ix.normAmbiguous[n] {
		return "", false
	}
	name, ok := ix.norm[n]
	return name, ok
}

func (ix *teacherIndex) match(field string) (string, bool) {
	if name, ok := ix.strong(field); ok {
		return name, true
	}

	code := strings.TrimRight(cleanText(field), "; ,")
	if n := normKey(code); len([]rune(n)) >= 2 {
		seen := make(map[string]bool)
		var found string
		for _, e := range ix.entries {
			if strings.HasPrefix(e.norm, n) && !seen[e.name] {
				seen[e.name] = true
				found = e.name
			}
		}
		if len(seen) == 1 {
			return found, true
		}
	}

	for _, key := range personKeys(personTokens(code), false) {
		if ix.ambiguous[key] {
			continue
		}
		if name, ok := ix.byKey[key]; ok {
			return name, true
		}
	}
	return "", false
}

func looksLikeTeacher(field string) bool {
	if len(personTokens(field)) == 0 {
		return false
	}
	return isTeacherName(field) || initialsRe.MatchString(strings.TrimSpace(field))
}

func resolveTeachers(field string, ix *teacherIndex) []string {
	if name, ok := ix.match(field); ok {
		return []string{name}
	}
	parts := splitRe.Split(field, -1)
	var names []string
	if len(parts) > 1 {
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if normKey(p) == "" {
				continue
			}
			if name, ok := ix.match(p); ok {
				names = append(names, name)
			} else if looksLikeTeacher(p) {
				names = append(names, p)
			}
		}
		if len(names) > 0 {
			return names
		}
	}
	if looksLikeTeacher(field) {
		return []string{strings.TrimSpace(field)}
	}
	return nil
}

func appendUnique(list []string, items ...string) []string {
	for _, it := range items {
		dup := false
		for _, e := range list {
			if e == it {
				dup = true
				break
			}
		}
		if !dup {
			list = append(list, it)
		}
	}
	return list
}

func plausibleAbbr(s string) bool {
	if strings.ContainsAny(s, "0123456789") {
		return false
	}
	size := len([]rune(s))
	if size < 2 || size > 40 {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func scanTeacherTokens(cell Cell, ix *teacherIndex) []string {
	var found []string
	for _, f := range collectFields(cell.Node) {
		for _, tok := range strings.Fields(f.text) {
			tok = strings.Trim(tok, ";,")
			if len([]rune(tok)) < 2 {
				continue
			}
			if name, ok := ix.exact[tok]; ok {
				found = appendUnique(found, name)
			}
		}
	}
	return found
}

func normalizeRoom(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, ";", " ; ")), " ")
}

func isRoomField(field string) bool {
	if strings.ContainsAny(field, "0123456789") {
		return true
	}

	upper := strings.ToUpper(field)
	return strings.Contains(upper, "ONLINE") ||
		strings.Contains(upper, "AE S") ||
		strings.Contains(upper, "KINOWA") ||
		strings.Contains(upper, "HALA") ||
		strings.Contains(upper, "SALA")
}
