package wat

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

type abbrEvidence struct {
	leading int
	other   int
	bold    bool
}

func collectEvidence(grid [][]Cell, legendCol int) map[string]*abbrEvidence {
	out := make(map[string]*abbrEvidence)
	for _, row := range grid {
		for _, cell := range row {
			if cell.Col < firstWeekCol || cell.Col >= legendCol || cell.Text == "" {
				continue
			}
			for i, f := range collectFields(cell.Node) {
				key := normKey(f.text)
				if key == "" {
					continue
				}
				ev := out[key]
				if ev == nil {
					ev = &abbrEvidence{}
					out[key] = ev
				}
				if f.bold {
					ev.bold = true
				}
				if i == 0 {
					ev.leading++
				} else {
					ev.other++
				}
			}
		}
	}
	return out
}

func buildLegend(grid [][]Cell, legendCol int, evidence map[string]*abbrEvidence) (map[string]string, map[string]string, map[string]map[string]string) {
	subjects := make(map[string]string)
	teachers := make(map[string]string)
	subjectTeachers := make(map[string]map[string]string)

	var currentSubject string

	for _, row := range grid {
		var abbr, name string

		for _, cell := range row {
			if mergeAttr, exists := cell.Node.Attr("mergeWith"); exists {
				abbr = cleanText(cell.Text)
				name = extractTextFromHTMLString(mergeAttr)
				if abbr != "" && name != "" {
					break
				}
			}
		}

		if abbr == "" || name == "" {
			for _, cell := range row {
				if cell.Col == legendCol {
					abbr = cleanText(cell.Text)
				} else if cell.Col == legendCol+1 {
					name = cleanText(cell.Text)
				}
			}
		}

		if abbr == "" || name == "" || abbr == "&nbsp" {
			continue
		}

		parts := strings.Fields(abbr)
		if len(parts) >= 2 && isTypeToken(parts[0]) && isNumeric(parts[1]) {
			classType := parts[0]
			if currentSubject != "" && name != "" {
				if subjectTeachers[currentSubject] == nil {
					subjectTeachers[currentSubject] = make(map[string]string)
				}
				subjectTeachers[currentSubject][classType] = name
				teachers[abbr] = name
			}
			continue
		}

		if isTeacherEntry(abbr, name, evidence) {
			teachers[abbr] = name
		} else {
			currentSubject = abbr
			subjects[abbr] = name
		}
	}

	return subjects, teachers, subjectTeachers
}

func isTypeToken(s string) bool {
	_, exists := typeMap[s]
	return exists
}

func isNumeric(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func extractTextFromHTMLString(htmlStr string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		return ""
	}
	text := doc.Text()
	re := regexp.MustCompile(`\d+$`)
	return cleanText(re.ReplaceAllString(text, ""))
}
