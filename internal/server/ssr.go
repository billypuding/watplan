package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/billypuding/watplan/internal/model"
)

const (
	ssrSemesterGapDays = 21
	ssrLongGapMin      = 30
)

var (
	ssrDayNames   = []string{"Poniedziałek", "Wtorek", "Środa", "Czwartek", "Piątek", "Sobota", "Niedziela"}
	ssrDayShort   = []string{"Pn", "Wt", "Śr", "Cz", "Pt", "So", "Nd"}
	ssrMonthsGen  = []string{"stycznia", "lutego", "marca", "kwietnia", "maja", "czerwca", "lipca", "sierpnia", "września", "października", "listopada", "grudnia"}
	ssrMonthsShrt = []string{"sty", "lut", "mar", "kwi", "maj", "cze", "lip", "sie", "wrz", "paź", "lis", "gru"}
	ssrDayAbbr    = []string{"pon.", "wt.", "śr.", "czw.", "pt.", "sob.", "niedz."}
)

type ssrPage struct {
	view string
	info string
	data []byte
}

func ssrDate(iso string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02", iso, time.UTC)
	return t
}

func ssrDayIndex(t time.Time) int {
	return (int(t.Weekday()) + 6) % 7
}

func ssrMonday(iso string) string {
	t := ssrDate(iso)
	return t.AddDate(0, 0, -ssrDayIndex(t)).Format("2006-01-02")
}

func ssrSummerStart(year int) string {
	d := time.Date(year, 2, 22, 0, 0, 0, 0, time.UTC)
	return d.AddDate(0, 0, -ssrDayIndex(d)).Format("2006-01-02")
}

func ssrSemester(iso string) string {
	t := ssrDate(iso)
	if t.Month() >= 8 {
		return "zima"
	}
	if iso < ssrSummerStart(t.Year()) {
		return "zima"
	}
	return "lato"
}

func ssrLongDate(iso string) string {
	t := ssrDate(iso)
	return fmt.Sprintf("%d %s", t.Day(), ssrMonthsGen[t.Month()-1])
}

func ssrShortDate(iso string) string {
	t := ssrDate(iso)
	return fmt.Sprintf("%s %d %s", ssrDayAbbr[ssrDayIndex(t)], t.Day(), ssrMonthsShrt[t.Month()-1])
}

func ssrPlural(n int, one, few, many string) string {
	if n == 1 {
		return one
	}
	d, dd := n%10, n%100
	if d >= 2 && d <= 4 && (dd < 12 || dd > 14) {
		return few
	}
	return many
}

func ssrClasses(n int) string {
	return fmt.Sprintf("%d %s", n, ssrPlural(n, "zajęcie", "zajęcia", "zajęć"))
}

func ssrMinutes(t string) int {
	var h, m int
	fmt.Sscanf(t, "%d:%d", &h, &m)
	return h*60 + m
}

func ssrFormatMinutes(mins int) string {
	if mins < 60 {
		return fmt.Sprintf("%d min", mins)
	}
	if mins%60 == 0 {
		return fmt.Sprintf("%d h", mins/60)
	}
	return fmt.Sprintf("%d h %d min", mins/60, mins%60)
}

func ssrTypeLabel(t string) string {
	if t == "" {
		return "Zajęcia"
	}
	r := []rune(t)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

func ssrSplitRooms(room string) (string, int) {
	var rooms []string
	for _, part := range strings.FieldsFunc(room, func(r rune) bool { return r == ';' || r == ',' }) {
		if p := strings.TrimSpace(part); p != "" {
			rooms = append(rooms, p)
		}
	}
	if len(rooms) == 0 {
		return "", 0
	}
	return rooms[0], len(rooms) - 1
}

type ssrEvent struct {
	model.Event
	n, total int
}

func ssrWeekNumber(monday string, weeks []string) string {
	var segments [][]string
	for _, w := range weeks {
		if n := len(segments); n > 0 {
			last := segments[n-1]
			if ssrDate(w).Sub(ssrDate(last[len(last)-1])).Hours()/24 <= ssrSemesterGapDays {
				segments[n-1] = append(last, w)
				continue
			}
		}
		segments = append(segments, []string{w})
	}
	for _, seg := range segments {
		for i, w := range seg {
			if w == monday {
				return fmt.Sprintf("tydzień %d/%d", i+1, len(seg))
			}
		}
	}
	switch {
	case len(weeks) == 0:
		return "tydzień bez zajęć"
	case monday < weeks[0]:
		return "przed semestrem"
	case monday > weeks[len(weeks)-1]:
		return "po semestrze"
	}
	return "tydzień bez zajęć"
}

const ssrIconBuilding = `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 21V5a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v16"/><path d="M16 9h2a2 2 0 0 1 2 2v10"/><path d="M2 21h20"/><path d="M8 7h4M8 11h4M8 15h4"/></svg>`
const ssrIconLeft = `<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m15 18-6-6 6-6"/></svg>`
const ssrIconRight = `<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 18 6-6-6-6"/></svg>`
const ssrIconUser = `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/></svg>`

func renderSSR(planID string, raw []byte, now time.Time) *ssrPage {
	var plan model.Plan
	if json.Unmarshal(raw, &plan) != nil {
		return nil
	}
	today := now.In(warsawLoc).Format("2006-01-02")
	semester := ssrSemester(today)
	var events []model.Event
	for _, e := range plan.Events {
		if ssrSemester(e.Date) == semester {
			events = append(events, e)
		}
	}
	if len(events) == 0 {
		return nil
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Date+events[i].Start < events[j].Date+events[j].Start })

	series := map[string][]int{}
	for i, e := range events {
		key := e.Subject + "\x00" + e.Type
		series[key] = append(series[key], i)
	}
	numbered := make([]ssrEvent, len(events))
	for _, idx := range series {
		for n, i := range idx {
			numbered[i] = ssrEvent{Event: events[i], n: n + 1, total: len(idx)}
		}
	}
	byDate := map[string][]ssrEvent{}
	weekSet := map[string]bool{}
	for _, e := range numbered {
		byDate[e.Date] = append(byDate[e.Date], e)
		weekSet[ssrMonday(e.Date)] = true
	}
	weeks := make([]string, 0, len(weekSet))
	for w := range weekSet {
		weeks = append(weeks, w)
	}
	sort.Strings(weeks)

	selected := today
	if first, last := events[0].Date, events[len(events)-1].Date; today < first {
		selected = first
	} else if today > last {
		selected = last
	}
	day := byDate[selected]
	sel := ssrDate(selected)

	esc := html.EscapeString
	var b strings.Builder
	b.WriteString(`<div class="day-title"><h1>` + ssrDayNames[ssrDayIndex(sel)] + `</h1><p>` + ssrLongDate(selected) + ` · <button type="button" class="week-link" aria-haspopup="dialog">` + ssrWeekNumber(ssrMonday(selected), weeks) + `</button>`)
	if len(day) > 0 {
		b.WriteString(" · " + ssrClasses(len(day)))
	}
	b.WriteString(`</p></div><div class="week-strip"><button type="button" class="arrow" aria-label="Poprzedni tydzień">` + ssrIconLeft + `</button>`)
	monday := ssrDate(ssrMonday(selected))
	for i := 0; i < 7; i++ {
		d := monday.AddDate(0, 0, i)
		iso := d.Format("2006-01-02")
		cls := []string{"day-pill"}
		has := len(byDate[iso]) > 0
		if has {
			cls = append(cls, "has")
		} else {
			cls = append(cls, "empty")
		}
		if iso == selected {
			cls = append(cls, "sel")
		}
		if iso == today {
			cls = append(cls, "today")
		}
		label := fmt.Sprintf("%s %d: %s, %s", ssrDayShort[i], d.Day(), strings.ToLower(ssrDayNames[i]), ssrLongDate(iso))
		if !has {
			label += ", brak zajęć"
		}
		fmt.Fprintf(&b, `<button type="button" class="%s" aria-pressed="%t" aria-label="%s"><span class="dn">%s</span><span class="dd">%d</span><span class="dot"></span></button>`, strings.Join(cls, " "), iso == selected, esc(label), ssrDayShort[i], d.Day())
	}
	b.WriteString(`<button type="button" class="arrow" aria-label="Następny tydzień">` + ssrIconRight + `</button></div>`)
	if len(day) == 0 {
		b.WriteString(`<div class="empty-day"><b>Wolne</b>Brak zajęć tego dnia.`)
		dates := make([]string, 0, len(byDate))
		for d := range byDate {
			dates = append(dates, d)
		}
		sort.Strings(dates)
		for _, d := range dates {
			if d > selected {
				b.WriteString(`<br><button type="button" class="link-btn">Następne zajęcia: ` + ssrShortDate(d) + `</button>`)
				break
			}
		}
		b.WriteString(`</div>`)
	} else {
		b.WriteString(`<div class="day-list">`)
		for i, e := range day {
			if i > 0 {
				if gap := ssrMinutes(e.Start) - ssrMinutes(day[i-1].End); gap >= ssrLongGapMin {
					b.WriteString(`<div class="gap-row">przerwa ` + ssrFormatMinutes(gap) + `</div>`)
				}
			}
			room, more := ssrSplitRooms(e.Room)
			fmt.Fprintf(&b, `<div class="ev-row"><div class="ev-time"><b>%s</b><span>%s</span></div><button type="button" class="ev-card tint"><div class="ev-subject">%s</div><div class="ev-type sub"><span class="dot"></span>%s<span class="count">%d/%d</span></div>`, esc(e.Start), esc(e.End), esc(e.Subject), esc(ssrTypeLabel(e.Type)), e.n, e.total)
			if room != "" || e.Teacher != "" {
				b.WriteString(`<div class="ev-meta sub">`)
				if room != "" {
					b.WriteString(`<span class="ev-place" title="` + esc(e.Room) + `">` + ssrIconBuilding + `<b>` + esc(room) + `</b>`)
					if more > 0 {
						fmt.Fprintf(&b, " +%d %s", more, ssrPlural(more, "sala", "sale", "sal"))
					}
					b.WriteString(`</span>`)
				}
				if e.Teacher != "" {
					b.WriteString(`<span class="ev-person">` + ssrIconUser + esc(e.Teacher) + `</span>`)
				}
				b.WriteString(`</div>`)
			}
			b.WriteString(`</button></div>`)
		}
		b.WriteString(`</div>`)
	}

	type subject struct {
		name     string
		types    map[string]int
		teachers map[string]bool
		order    []string
	}
	subjects := map[string]*subject{}
	var names []string
	for _, e := range events {
		s := subjects[e.Subject]
		if s == nil {
			s = &subject{name: e.Subject, types: map[string]int{}, teachers: map[string]bool{}}
			subjects[e.Subject] = s
			names = append(names, e.Subject)
		}
		t := strings.ToLower(ssrTypeLabel(e.Type))
		if s.types[t] == 0 {
			s.order = append(s.order, t)
		}
		s.types[t]++
		if e.Teacher != "" {
			s.teachers[e.Teacher] = true
		}
	}
	sort.Strings(names)
	semName := "zimowym"
	if semester == "lato" {
		semName = "letnim"
	}
	var info strings.Builder
	fmt.Fprintf(&info, `<section class="plan-info" aria-labelledby="planInfoTitle"><h2 id="planInfoTitle">Przedmioty grupy %s w semestrze %s</h2><ul>`, esc(planID), semName)
	for _, n := range names {
		s := subjects[n]
		var kinds []string
		for _, t := range s.order {
			kinds = append(kinds, fmt.Sprintf("%s %d", t, s.types[t]))
		}
		teachers := make([]string, 0, len(s.teachers))
		for t := range s.teachers {
			teachers = append(teachers, t)
		}
		sort.Strings(teachers)
		info.WriteString(`<li><b>` + esc(n) + `</b> – ` + esc(strings.Join(kinds, ", ")))
		if len(teachers) > 0 {
			info.WriteString(`; ` + esc(strings.Join(teachers, ", ")))
		}
		info.WriteString(`</li>`)
	}
	info.WriteString(`</ul></section>`)

	var data bytes.Buffer
	json.HTMLEscape(&data, raw)
	return &ssrPage{view: b.String(), info: info.String(), data: data.Bytes()}
}
