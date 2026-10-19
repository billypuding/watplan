package wat

import (
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

func inferTeacherCodes(grid [][]Cell, legendCol int, subjects map[string]string, legendTeachers map[string][]string) map[string]string {
	if len(legendTeachers) == 0 {
		return nil
	}

	bySubjectKey := make(map[string]map[string]map[string]bool, len(legendTeachers))
	for subj, names := range legendTeachers {
		byKey := make(map[string]map[string]bool)
		for _, name := range names {
			for _, key := range teacherCodeKeys(name) {
				if byKey[key] == nil {
					byKey[key] = make(map[string]bool)
				}
				byKey[key][name] = true
			}
		}
		if len(byKey) > 0 {
			bySubjectKey[subj] = byKey
		}
	}
	if len(bySubjectKey) == 0 {
		return nil
	}

	votes := make(map[string]map[string]int)
	for _, row := range grid {
		for _, cell := range row {
			if cell.Col < firstWeekCol || cell.Col >= legendCol || cell.Text == "" {
				continue
			}
			subj, codes := cellSubjectAndCodes(cell.Node, subjects)
			byKey := bySubjectKey[subj]
			if len(byKey) == 0 {
				continue
			}
			unique := singleName(legendTeachers[subj])
			for _, code := range codes {
				if !plausibleCode(code) {
					continue
				}
				name := ""
				if hits := byKey[normKey(code)]; len(hits) == 1 {
					for n := range hits {
						name = n
					}
				} else {
					name = unique
				}
				if name == "" {
					continue
				}
				if votes[code] == nil {
					votes[code] = make(map[string]int)
				}
				votes[code][name]++
			}
		}
	}

	out := make(map[string]string)
	for code, tally := range votes {
		if len(tally) != 1 {
			continue
		}
		for name := range tally {
			out[code] = name
		}
	}
	return out
}

func cellSubjectAndCodes(sel *goquery.Selection, subjects map[string]string) (string, []string) {
	var subj string
	var codes []string
	for _, f := range collectFields(sel) {
		if subjects[f.text] != "" {
			subj = f.text
			continue
		}
		if typeMap[f.text] != "" || ignoredFields[f.text] || f.text == "E" || f.text == "Ep" || isRoomField(f.text) {
			continue
		}
		for _, part := range strings.FieldsFunc(f.text, func(r rune) bool {
			return r == ';' || r == ',' || r == '/'
		}) {
			if p := strings.TrimSpace(part); p != "" {
				codes = append(codes, p)
			}
		}
	}
	return subj, codes
}

func singleName(names []string) string {
	var only string
	for _, n := range names {
		if only == "" {
			only = n
		} else if only != n {
			return ""
		}
	}
	return only
}

func plausibleCode(code string) bool {
	letters := 0
	for _, r := range code {
		switch {
		case unicode.IsDigit(r):
			return false
		case unicode.IsLetter(r):
			letters++
		case r == '.' || r == '-' || r == ' ' || r == '\'':
		default:
			return false
		}
	}
	return letters >= 2 && letters <= 8
}

func teacherCodeKeys(name string) []string {
	parts := personCodeParts(name)
	if len(parts) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var keys []string
	add := func(k string) {
		if n := len([]rune(k)); n < 2 || n > 8 || seen[k] {
			return
		}
		seen[k] = true
		keys = append(keys, k)
	}

	if len(parts) == 1 {
		f := parts[0]
		cs := consonantKey(f)
		for k := 2; k <= len([]rune(f)); k++ {
			add(prefix(f, k))
		}
		for k := 2; k <= len([]rune(cs)); k++ {
			add(prefix(cs, k))
		}
		return keys
	}

	giv := parts[len(parts)-1]
	surParts := parts[:len(parts)-1]
	sur := strings.Join(surParts, "")
	surCons := consonantKey(sur)

	for _, sp := range surParts {
		sc := consonantKey(sp)
		for k := 1; k <= minInt(len([]rune(sp)), 5); k++ {
			add(prefix(sp, k) + firstRune(giv))
			add(firstRune(giv) + prefix(sp, k))
		}
		for k := 3; k <= len([]rune(sp)); k++ {
			add(prefix(sp, k))
		}
		for k := 2; k <= len([]rune(sc)); k++ {
			add(prefix(sc, k))
		}
	}

	for k := 1; k <= minInt(len([]rune(sur)), 5); k++ {
		add(prefix(sur, k) + firstRune(giv))
		add(firstRune(giv) + prefix(sur, k))
	}
	for k := 1; k <= minInt(len([]rune(giv)), 4); k++ {
		for m := 1; m <= minInt(len([]rune(sur)), 4); m++ {
			add(prefix(giv, k) + prefix(sur, m))
			add(prefix(sur, k) + prefix(giv, m))
		}
	}
	for k := 3; k <= len([]rune(sur)); k++ {
		add(prefix(sur, k))
	}
	for k := 2; k <= len([]rune(surCons)); k++ {
		add(prefix(surCons, k))
	}
	for k := 3; k <= len([]rune(giv)); k++ {
		add(prefix(giv, k))
	}
	add(firstRune(sur) + firstRune(giv))
	add(firstRune(giv) + firstRune(sur))
	if len(surParts) > 1 {
		var b strings.Builder
		for _, p := range surParts {
			b.WriteString(prefix(p, 3))
		}
		b.WriteString(firstRune(giv))
		add(b.String())
	}
	return keys
}

func personCodeParts(name string) []string {
	var parts []string
	for _, tok := range strings.FieldsFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || r == '.'
	}) {
		if _, isTitle := titleTokens[normKey(tok)]; isTitle {
			continue
		}
		for _, p := range strings.FieldsFunc(tok, func(r rune) bool { return r == '-' }) {
			if f := normKey(p); f != "" {
				parts = append(parts, f)
			}
		}
	}
	return parts
}

func consonantKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		if !strings.ContainsRune("aeiouy", r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func prefix(s string, k int) string {
	r := []rune(s)
	if k > len(r) {
		k = len(r)
	}
	return string(r[:k])
}

func firstRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[0])
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
