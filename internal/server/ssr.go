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
	view  string
	info  string
	data  []byte
	stale bool
	chip  bool
}

const (
	ssrStaleAfter = 24 * time.Hour
	ssrChipDays   = 7
)

func ssrStamp(text string) (time.Time, bool) {
	if len(text) < 16 {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02 15:04", text[:16], warsawLoc)
	return t, err == nil
}

func ssrAgo(at, now time.Time) string {
	min := int(now.Sub(at).Minutes() + 0.5)
	if min < 0 {
		min = 0
	}
	if min < 60 {
		if min <= 1 {
			return "przed chwilą"
		}
		return fmt.Sprintf("%d min temu", min)
	}
	hrs := int(float64(min)/60 + 0.5)
	if hrs < 24 {
		return fmt.Sprintf("%d godz. temu", hrs)
	}
	days := int(float64(hrs)/24 + 0.5)
	if days == 1 {
		return "wczoraj"
	}
	return fmt.Sprintf("%d dni temu", days)
}

func ssrStaleBanner(fetchedAt string, now time.Time) string {
	fetched, ok := ssrStamp(fetchedAt)
	if !ok || now.Sub(fetched) <= ssrStaleAfter {
		return ""
	}
	return fmt.Sprintf("Plan pobrany %d %s o %s – może być nieaktualny. W razie wątpliwości sprawdź oficjalny plan WAT.", fetched.Day(), ssrMonthsGen[fetched.Month()-1], fetched.Format("15:04"))
}

func ssrChipText(history []model.Change, now time.Time) string {
	if len(history) == 0 {
		return ""
	}
	last := history[0]
	at, ok := ssrStamp(last.At)
	if !ok || now.Sub(at) > ssrChipDays*24*time.Hour {
		return ""
	}
	n := last.Count()
	when := ssrAgo(at, now)
	if since, ok := ssrStamp(last.Since); ok && at.Sub(since) > 36*time.Hour {
		when = fmt.Sprintf("od %d %s", since.Day(), ssrMonthsGen[since.Month()-1])
	}
	return fmt.Sprintf("%d %s w planie · %s", n, ssrPlural(n, "zmiana", "zmiany", "zmian"), when)
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

var (
	ssrSlots      = [][2]string{{"08:00", "09:35"}, {"09:50", "11:25"}, {"11:40", "13:15"}, {"13:30", "15:05"}, {"16:00", "17:35"}, {"17:50", "19:25"}, {"19:40", "21:15"}}
	ssrSmallWords = map[string]bool{"i": true, "w": true, "z": true, "do": true, "na": true, "we": true, "ze": true, "o": true, "od": true, "dla": true, "oraz": true, "a": true, "-": true}
)

const ssrIconDown = `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg>`
const ssrIconBuilding13 = `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 21V5a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v16"/><path d="M16 9h2a2 2 0 0 1 2 2v10"/><path d="M2 21h20"/><path d="M8 7h4M8 11h4M8 15h4"/></svg>`
const ssrIconUser13 = `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/></svg>`

func ssrIsNumber(w string) bool {
	if w == "" {
		return false
	}
	for _, r := range w {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func ssrIsRoman(w string) bool {
	if w == "" {
		return false
	}
	for _, r := range w {
		if r != 'I' && r != 'V' && r != 'X' {
			return false
		}
	}
	return true
}

func ssrAbbreviate(subject string) string {
	cleaned := strings.Map(func(r rune) rune {
		if strings.ContainsRune("()[]{},.;:", r) {
			return ' '
		}
		return r
	}, subject)
	words := strings.Fields(cleaned)
	var big, nums []string
	for _, w := range words {
		if ssrIsNumber(w) || ssrIsRoman(w) {
			nums = append(nums, w)
		} else if !ssrSmallWords[strings.ToLower(w)] {
			big = append(big, w)
		}
	}
	var ab string
	if len(big) <= 1 {
		w := subject
		if len(big) == 1 {
			w = big[0]
		}
		r := []rune(w)
		n := 3
		if w == strings.ToUpper(w) {
			n = 5
		}
		if len(r) > n {
			r = r[:n]
		}
		ab = string(r)
	} else {
		var b strings.Builder
		for _, w := range words {
			if ssrIsNumber(w) || ssrIsRoman(w) {
				continue
			}
			first := []rune(w)[0:1]
			if ssrSmallWords[strings.ToLower(w)] {
				if w != "-" {
					b.WriteString(strings.ToLower(string(first)))
				}
				continue
			}
			b.WriteString(strings.ToUpper(string(first)))
		}
		ab = b.String()
	}
	if len(nums) > 0 {
		return ab + " " + strings.Join(nums, " ")
	}
	return ab
}

func ssrTypeShort(t string) string {
	t = strings.ToLower(t)
	for _, p := range [][2]string{{"wykład", "wykł."}, {"ćwicz", "ćw."}, {"labor", "lab."}, {"semin", "sem."}, {"projekt", "proj."}} {
		if strings.HasPrefix(t, p[0]) {
			return p[1]
		}
	}
	return t
}

func ssrSlotStart(t string) int {
	m, idx := ssrMinutes(t), 0
	for i, s := range ssrSlots {
		if ssrMinutes(s[0]) <= m {
			idx = i
		}
	}
	return idx
}

func ssrSlotEnd(t string) int {
	m := ssrMinutes(t)
	for i, s := range ssrSlots {
		if ssrMinutes(s[1]) >= m {
			return i
		}
	}
	return len(ssrSlots) - 1
}

func ssrWeekRange(monday string) string {
	m := ssrDate(monday)
	s := m.AddDate(0, 0, 6)
	if m.Month() == s.Month() {
		return fmt.Sprintf("%d–%d %s", m.Day(), s.Day(), ssrMonthsGen[s.Month()-1])
	}
	return fmt.Sprintf("%d %s – %d %s", m.Day(), ssrMonthsShrt[m.Month()-1], s.Day(), ssrMonthsShrt[s.Month()-1])
}

func ssrWeekView(selected, today string, byDate map[string][]ssrEvent, weeks []string, chip string, now time.Time) string {
	esc := html.EscapeString
	mondayISO := ssrMonday(selected)
	monday := ssrDate(mondayISO)
	var dates []string
	var events []ssrEvent
	for i := 0; i < 7; i++ {
		d := monday.AddDate(0, 0, i).Format("2006-01-02")
		dates = append(dates, d)
		events = append(events, byDate[d]...)
	}
	days := 5
	for _, e := range events {
		if ssrDayIndex(ssrDate(e.Date)) >= 5 {
			days = 7
		}
	}
	title := ssrWeekRange(mondayISO)
	subtitle := ssrWeekNumber(mondayISO, weeks) + " · " + ssrClasses(len(events))
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="nav-row ssr-w"><button type="button" class="round-btn" aria-label="Wstecz">%s</button><button type="button" class="nav-label pick" aria-haspopup="dialog" aria-label="%s"><b>%s%s</b><small>%s</small></button><button type="button" class="round-btn" aria-label="Dalej">%s</button></div>`,
		ssrIconLeft, esc(title+", "+subtitle+". Wybierz tydzień"), esc(title), ssrIconDown, esc(subtitle), ssrIconRight)
	if chip != "" {
		b.WriteString(`<button class="change-chip ssr-w" type="button" tabindex="-1" aria-hidden="true"><span class="pulse"></span>` + esc(chip) + `</button>`)
	}
	if len(events) == 0 {
		b.WriteString(`<div class="empty-day ssr-w"><b>Wolny tydzień</b>Brak zajęć w tym tygodniu.`)
		var all []string
		for d := range byDate {
			all = append(all, d)
		}
		sort.Strings(all)
		for _, d := range all {
			if d > dates[6] {
				b.WriteString(`<br><button type="button" class="link-btn">Następne zajęcia: ` + ssrShortDate(d) + `</button>`)
				break
			}
		}
		b.WriteString(`</div>`)
		return b.String()
	}
	minSlot, maxSlot := len(ssrSlots)-1, 0
	for _, e := range events {
		if s := ssrSlotStart(e.Start); s < minSlot {
			minSlot = s
		}
		if s := ssrSlotEnd(e.End); s > maxSlot {
			maxSlot = s
		}
	}
	grid := "week-grid ssr-w"
	if days == 7 {
		grid += " wd7"
	}
	b.WriteString(`<div class="` + grid + `">`)
	lastRow := maxSlot - minSlot + 2
	for s := minSlot; s <= maxSlot; s++ {
		for c := 0; c < days; c++ {
			fmt.Fprintf(&b, `<div class="wk-bg wr-s%d wc-%d"></div>`, s-minSlot+2, c+2)
		}
	}
	for i := 0; i < days; i++ {
		if dates[i] == today {
			fmt.Fprintf(&b, `<div class="wk-today wr-s1 wr-e%d wc-%d"></div>`, lastRow+1, i+2)
		}
	}
	for i := 0; i < days; i++ {
		d := ssrDate(dates[i])
		n := len(byDate[dates[i]])
		cls := "wk-head"
		if dates[i] == today {
			cls += " today"
		}
		count := "wolne"
		if n > 0 {
			count = ssrClasses(n)
		}
		fmt.Fprintf(&b, `<div class="%s wr-s1 wc-%d"><b class="short">%s</b><b class="long">%s</b><span class="dd"><span class="num">%d</span><span class="long"> %s</span></span><small class="long">%s</small></div>`,
			cls, i+2, ssrDayShort[i], ssrDayNames[i], d.Day(), ssrMonthsShrt[d.Month()-1], count)
	}
	for s := minSlot; s <= maxSlot; s++ {
		st, en := ssrSlots[s][0], ssrSlots[s][1]
		fmt.Fprintf(&b, `<div class="wk-time wr-s%d wc-1"><b>%s</b><span>%s</span></div>`, s-minSlot+2, st, en)
	}
	type bucket struct {
		key  string
		list []ssrEvent
	}
	var buckets []*bucket
	index := map[string]*bucket{}
	for _, e := range events {
		key := fmt.Sprintf("%s|%d", e.Date, ssrSlotStart(e.Start))
		bk := index[key]
		if bk == nil {
			bk = &bucket{key: key}
			index[key] = bk
			buckets = append(buckets, bk)
		}
		bk.list = append(bk.list, e)
	}
	for _, bk := range buckets {
		first := bk.list[0]
		col := ssrDayIndex(ssrDate(first.Date)) + 2
		rowStart := ssrSlotStart(first.Start) - minSlot + 2
		rowEnd := 0
		for _, e := range bk.list {
			if r := ssrSlotEnd(e.End) - minSlot + 3; r > rowEnd {
				rowEnd = r
			}
		}
		fmt.Fprintf(&b, `<div class="wk-slot wc-%d wr-s%d wr-e%d">`, col, rowStart, rowEnd)
		for _, e := range bk.list {
			room, more := ssrSplitRooms(e.Room)
			cls := "wk-cell tint"
			if start, end, ok := ssrEventTimes(e.Event); ok && !now.Before(start) && now.Before(end) {
				cls += " live"
			}
			title := e.Start + "–" + e.End + " " + e.Subject
			label := e.Subject + ", " + ssrTypeLabel(e.Type) + ", " + e.Start + "–" + e.End
			if e.Room != "" {
				title += ", sala " + e.Room
				label += ", sala " + e.Room
			}
			if e.Teacher != "" {
				title += ", " + e.Teacher
			}
			fmt.Fprintf(&b, `<button type="button" class="%s" title="%s" aria-label="%s"><span class="time sub">%s–%s</span><span class="ab">%s</span><span class="full">%s</span><span class="meta sub"><span class="kind"><span class="dot"></span>%s<span class="cnt"> %d/%d</span></span>`,
				cls, esc(title), esc(label+". Pokaż wszystkie"), esc(e.Start), esc(e.End), esc(ssrAbbreviate(e.Subject)), esc(e.Subject), esc(ssrTypeShort(e.Type)), e.n, e.total)
			if room != "" {
				b.WriteString(`<span class="room">` + ssrIconBuilding13 + esc(room))
				if more > 0 {
					fmt.Fprintf(&b, " +%d", more)
				}
				b.WriteString(`</span>`)
			}
			if e.Teacher != "" {
				b.WriteString(`<span class="teacher">` + ssrIconUser13 + esc(e.Teacher) + `</span>`)
			}
			b.WriteString(`</span><span class="wk-live"><span class="fill"></span></span></button>`)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func ssrEventTimes(e model.Event) (time.Time, time.Time, bool) {
	start, err1 := time.ParseInLocation("2006-01-02 15:04", e.Date+" "+e.Start, warsawLoc)
	end, err2 := time.ParseInLocation("2006-01-02 15:04", e.Date+" "+e.End, warsawLoc)
	return start, end, err1 == nil && err2 == nil
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

func renderSSR(planID string, raw []byte, history []model.Change, now time.Time) *ssrPage {
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
	stale := ssrStaleBanner(plan.FetchedAt, now)
	if stale != "" {
		b.WriteString(`<p id="staleBanner" class="stale-banner" role="status">` + esc(stale) + `</p>`)
	}
	b.WriteString(`<div class="day-title ssr-d"><h1>` + ssrDayNames[ssrDayIndex(sel)] + `</h1><p>` + ssrLongDate(selected) + ` · <button type="button" class="week-link" aria-haspopup="dialog">` + ssrWeekNumber(ssrMonday(selected), weeks) + `</button>`)
	if len(day) > 0 {
		b.WriteString(" · " + ssrClasses(len(day)))
	}
	b.WriteString(`</p></div>`)
	chip := ssrChipText(history, now)
	if chip != "" {
		b.WriteString(`<button id="changeChip" class="change-chip" type="button"><span class="pulse"></span>` + esc(chip) + `</button>`)
	}
	b.WriteString(`<div class="week-strip ssr-d"><button type="button" class="arrow" aria-label="Poprzedni tydzień">` + ssrIconLeft + `</button>`)
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
		b.WriteString(`<div class="empty-day ssr-d"><b>Wolne</b>Brak zajęć tego dnia.`)
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
		b.WriteString(`<div class="day-list ssr-d">`)
		for i, e := range day {
			if i > 0 {
				if gap := ssrMinutes(e.Start) - ssrMinutes(day[i-1].End); gap >= ssrLongGapMin {
					b.WriteString(`<div class="gap-row">przerwa ` + ssrFormatMinutes(gap) + `</div>`)
				}
			}
			room, more := ssrSplitRooms(e.Room)
			cardClass, remaining := "ev-card tint", ""
			if start, end, ok := ssrEventTimes(e.Event); ok && !now.Before(start) && now.Before(end) {
				cardClass += " live"
				remaining = "zostało " + ssrFormatMinutes(int(end.Sub(now).Minutes()+0.999))
			}
			fmt.Fprintf(&b, `<div class="ev-row"><div class="ev-time"><b>%s</b><span>%s</span></div><button type="button" class="%s"><div class="ev-subject">%s</div><div class="ev-type sub"><span class="dot"></span>%s<span class="count">%d/%d</span></div>`, esc(e.Start), esc(e.End), cardClass, esc(e.Subject), esc(ssrTypeLabel(e.Type)), e.n, e.total)
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
			b.WriteString(`<div class="ev-live"><div class="row"><span>Trwa teraz</span><span class="remaining">` + remaining + `</span></div><div class="track"><div class="fill"></div></div></div></button></div>`)
		}
		b.WriteString(`</div>`)
	}

	b.WriteString(ssrWeekView(selected, today, byDate, weeks, chip, now))

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
	fmt.Fprintf(&info, `<details class="plan-info"><summary><h2 id="planInfoTitle">Przedmioty grupy %s w semestrze %s</h2></summary><ul>`, esc(planID), semName)
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
	info.WriteString(`</ul></details>`)

	var data bytes.Buffer
	json.HTMLEscape(&data, raw)
	return &ssrPage{view: b.String(), info: info.String(), data: data.Bytes(), stale: stale != "", chip: chip != ""}
}
