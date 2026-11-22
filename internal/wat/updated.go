package wat

import (
	"regexp"
	"time"
)

var (
	indexUpdatedRe = regexp.MustCompile(`(?i)(?:lastupdatetext="|class="lastUpdate"[^>]*>\s*)Aktualizacja:\s*([0-9.: -]{16,19})`)
	planUpdatedRe  = regexp.MustCompile(`Data aktualizacji:\s*([0-9.: -]{16,19})`)
	updatedLayouts = []string{"02.01.2006 15:04:05", "2006-01-02 15:04:05", "02.01.2006 15:04", "2006-01-02 15:04"}
)

var warsaw = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		return time.Local
	}
	return loc
}()

func ParseUpdated(text string) (time.Time, bool) {
	for _, layout := range updatedLayouts {
		if t, err := time.ParseInLocation(layout, text, warsaw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func IndexUpdated(body []byte) (time.Time, bool) {
	return firstUpdated(indexUpdatedRe, body)
}

func PlanUpdated(body []byte) (time.Time, bool) {
	return firstUpdated(planUpdatedRe, body)
}

func firstUpdated(re *regexp.Regexp, body []byte) (time.Time, bool) {
	m := re.FindSubmatch(body)
	if m == nil {
		return time.Time{}, false
	}
	return ParseUpdated(string(trimSpaces(m[1])))
}

func trimSpaces(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '-') {
		b = b[:len(b)-1]
	}
	return b
}
