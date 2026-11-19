package server

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
)

const icsRefresh = "PT3H"

type icsOptions struct {
	alarm   int
	baseURL string
	stamp   time.Time
}

func icsEscape(text string) string {
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`)
	return r.Replace(text)
}

func icsFold(line string) string {
	const limit = 75
	if len(line) <= limit {
		return line
	}
	var b strings.Builder
	width := 0
	for _, r := range line {
		n := utf8.RuneLen(r)
		if width+n > limit {
			b.WriteString("\r\n ")
			width = 1
		}
		b.WriteRune(r)
		width += n
	}
	return b.String()
}

func icsLocal(date, clock string) string {
	return strings.ReplaceAll(date, "-", "") + "T" + strings.ReplaceAll(clock, ":", "") + "00"
}

func typeLabel(t string) string {
	if t == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(t)
	return strings.ToUpper(string(r)) + t[size:]
}

func eventUID(planID string, e model.Event) string {
	sum := sha1.Sum([]byte(strings.Join([]string{planID, e.Date, e.Start, e.Subject, e.Type}, "|")))
	return hex.EncodeToString(sum[:8])
}

func buildICS(plan config.PlanConfig, events []model.Event, opts icsOptions) []byte {
	sorted := append([]model.Event(nil), events...)
	model.SortEvents(sorted)

	totals := map[string]int{}
	for _, e := range sorted {
		totals[e.Subject+"|"+e.Type]++
	}

	name := plan.Name
	if name == "" {
		name = plan.ID
	}
	planURL := opts.baseURL + "/app?plan=" + plan.ID
	stamp := opts.stamp.UTC().Format("20060102T150405Z")

	lines := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//WATplan//PL",
		"CALSCALE:GREGORIAN",
		"METHOD:PUBLISH",
		"NAME:" + icsEscape("WAT "+name),
		"X-WR-CALNAME:" + icsEscape("WAT "+name),
		"X-WR-CALDESC:" + icsEscape("Plan zajęć grupy "+name+" z watplan.me"),
		"X-WR-TIMEZONE:Europe/Warsaw",
		"REFRESH-INTERVAL;VALUE=DURATION:" + icsRefresh,
		"X-PUBLISHED-TTL:" + icsRefresh,
		"URL:" + planURL,
		"BEGIN:VTIMEZONE",
		"TZID:Europe/Warsaw",
		"BEGIN:STANDARD",
		"DTSTART:19701025T030000",
		"TZOFFSETFROM:+0200",
		"TZOFFSETTO:+0100",
		"RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU",
		"END:STANDARD",
		"BEGIN:DAYLIGHT",
		"DTSTART:19700329T020000",
		"TZOFFSETFROM:+0100",
		"TZOFFSETTO:+0200",
		"RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU",
		"END:DAYLIGHT",
		"END:VTIMEZONE",
	}

	seen := map[string]int{}
	counter := map[string]int{}
	for _, e := range sorted {
		key := e.Subject + "|" + e.Type
		counter[key]++

		uid := eventUID(plan.ID, e)
		seen[uid]++
		if n := seen[uid]; n > 1 {
			uid = fmt.Sprintf("%s-%d", uid, n)
		}

		summary := e.Subject
		if e.Type != "" {
			summary += " (" + e.Type + ")"
		}
		var desc []string
		if e.Type != "" {
			desc = append(desc, fmt.Sprintf("%s %d/%d", typeLabel(e.Type), counter[key], totals[key]))
		}
		if e.Teacher != "" {
			desc = append(desc, "Prowadzący: "+e.Teacher)
		}
		desc = append(desc, "Plan: "+planURL)

		lines = append(lines,
			"BEGIN:VEVENT",
			"UID:"+uid+"-"+plan.ID+"@watplan.me",
			"DTSTAMP:"+stamp,
			"DTSTART;TZID=Europe/Warsaw:"+icsLocal(e.Date, e.Start),
			"DTEND;TZID=Europe/Warsaw:"+icsLocal(e.Date, e.End),
			"SUMMARY:"+icsEscape(summary),
		)
		if e.Room != "" {
			lines = append(lines, "LOCATION:"+icsEscape("Sala "+e.Room))
		}
		lines = append(lines, "DESCRIPTION:"+icsEscape(strings.Join(desc, "\n")))
		if opts.alarm > 0 {
			lines = append(lines,
				"BEGIN:VALARM",
				"ACTION:DISPLAY",
				fmt.Sprintf("TRIGGER:-PT%dM", opts.alarm),
				"DESCRIPTION:"+icsEscape(summary),
				"END:VALARM",
			)
		}
		lines = append(lines, "END:VEVENT")
	}
	lines = append(lines, "END:VCALENDAR")

	var b strings.Builder
	for _, l := range lines {
		b.WriteString(icsFold(l))
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}
