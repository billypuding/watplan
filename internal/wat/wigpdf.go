package wat

import (
	"errors"
	"io"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/billypuding/watplan/internal/model"
)

const FormatWIGPDF = "wig-pdf"

var (
	wigEventRe = regexp.MustCompile(`^(.+?)\s*\(([^()]+)\)$`)
	wigCountRe = regexp.MustCompile(`^(\S+)\s+\d+$`)
	wigTypes   = map[string]string{"i": "inne"}
	wigMonths  = map[string]bool{
		"styczeń": true, "luty": true, "marzec": true, "kwiecień": true, "maj": true, "czerwiec": true,
		"lipiec": true, "sierpień": true, "wrzesień": true, "październik": true, "listopad": true, "grudzień": true,
	}
)

type wigRow struct {
	page    int
	y0, y1  float64
	weekday time.Weekday
	slot    string
}

type wigDateCol struct {
	x0, x1 float64
	date   time.Time
}

type wigLegend struct {
	subjects        map[string]string
	teachers        map[string]string
	subjectTeachers map[string]map[string]string
}

func ParseWIGPDF(r io.Reader, startYear int) ([]model.Event, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(string(data[:min(len(data), 8)]), "%PDF") {
		return nil, errors.New("plik nie jest PDF-em z planem WIG")
	}
	cells, err := readPDFCells(data)
	if err != nil {
		return nil, err
	}

	dayCol, slotCol, ok := wigLabelColumns(cells)
	if !ok {
		return nil, errors.New("PDF nie zawiera siatki planu Plansoft")
	}
	cells = mergePageSplits(cells, dayCol, slotCol)

	dates := wigDates(cells, dayCol, startYear)
	if len(dates) == 0 {
		return nil, errors.New("PDF nie zawiera dat zajęć")
	}
	legendX := 0.0
	for _, cols := range dates {
		for _, c := range cols {
			legendX = math.Max(legendX, c.x1)
		}
	}

	rows := wigRows(cells, dayCol, slotCol)
	legend := wigReadLegend(cells, legendX)

	var events []model.Event
	for _, cell := range cells {
		if cell.x0 < slotCol.x1-pdfTol || cell.x1 > legendX+pdfTol {
			continue
		}
		cellRows := wigCellRows(rows, cell)
		if len(cellRows) == 0 {
			continue
		}
		weekday := cellRows[0].weekday
		var cellDates []string
		for _, col := range dates[weekday] {
			if mid := (col.x0 + col.x1) / 2; mid > cell.x0 && mid < cell.x1 {
				cellDates = append(cellDates, col.date.Format(dateLayout))
			}
		}
		if len(cellDates) == 0 {
			continue
		}
		event, ok := wigEvent(cell, legend)
		if !ok {
			continue
		}
		event.Start = slotMap[cellRows[0].slot].Start
		event.End = slotMap[cellRows[len(cellRows)-1].slot].End
		for _, d := range cellDates {
			event.Date = d
			events = append(events, event)
		}
	}

	model.SortEvents(events)
	return events, nil
}

type wigColumn struct{ x0, x1 float64 }

func wigLabelColumns(cells []pdfCell) (day, slot wigColumn, ok bool) {
	for _, c := range cells {
		if _, isDay := dayMap[c.text(" ")]; isDay && day.x1 == 0 {
			day = wigColumn{c.x0, c.x1}
		}
		if _, isSlot := slotMap[c.text("")]; isSlot && slot.x1 == 0 {
			slot = wigColumn{c.x0, c.x1}
		}
	}
	return day, slot, day.x1 > 0 && slot.x1 > 0
}

func mergePageSplits(cells []pdfCell, dayCol, slotCol wigColumn) []pdfCell {
	topLabel := map[int]float64{}
	for _, c := range cells {
		if math.Abs(c.x0-slotCol.x0) < pdfTol {
			if _, ok := slotMap[c.text("")]; ok {
				topLabel[c.page] = math.Max(topLabel[c.page], c.y1)
			}
		}
	}

	headerBand := func(c pdfCell) bool {
		for _, o := range cells {
			if o.page != c.page || math.Abs(o.y0-c.y0) > pdfTol || math.Abs(o.y1-c.y1) > pdfTol {
				continue
			}
			if math.Abs(o.x0-dayCol.x0) < pdfTol || wigMonths[o.text(" ")] {
				return true
			}
		}
		return false
	}

	out := make([]pdfCell, 0, len(cells))
	for _, c := range cells {
		top, labelled := topLabel[c.page]
		if c.page == 1 || !labelled || c.y0 < top-pdfTol || headerBand(c) {
			out = append(out, c)
			continue
		}
		prev := -1
		for i := range out {
			p := out[i]
			if p.page == c.page-1 && math.Abs(p.x0-c.x0) < 2 && math.Abs(p.x1-c.x1) < 2 && (prev < 0 || p.y0 < out[prev].y0) {
				prev = i
			}
		}
		if prev >= 0 {
			for _, l := range c.lines {
				if !slices.Contains(out[prev].lines, l) {
					out[prev].lines = append(out[prev].lines, l)
				}
			}
		}
	}
	return out
}

func wigDates(cells []pdfCell, dayCol wigColumn, startYear int) map[time.Weekday][]wigDateCol {
	out := map[time.Weekday][]wigDateCol{}
	for _, head := range cells {
		if math.Abs(head.x0-dayCol.x0) > pdfTol {
			continue
		}
		weekday, ok := dayMap[head.text(" ")]
		if !ok {
			continue
		}
		var cols []wigDateCol
		for _, c := range cells {
			if c.page != head.page || math.Abs(c.y0-head.y0) > pdfTol || math.Abs(c.y1-head.y1) > pdfTol || c.x0 <= head.x1-pdfTol {
				continue
			}
			day, month, err := parseDayMonth(c.text(" "))
			if err != nil {
				continue
			}
			cols = append(cols, wigDateCol{x0: c.x0, x1: c.x1, date: time.Date(0, month, day, 0, 0, 0, 0, time.UTC)})
		}
		if len(cols) == 0 {
			continue
		}
		sort.Slice(cols, func(i, j int) bool { return cols[i].x0 < cols[j].x0 })
		year := startYear
		var prev time.Month
		valid := cols[:0]
		for _, col := range cols {
			if prev == 0 && col.date.Month() >= time.February && col.date.Month() < time.August {
				year = startYear + 1
			} else if col.date.Month() < prev {
				year++
			}
			prev = col.date.Month()
			col.date = time.Date(year, col.date.Month(), col.date.Day(), 0, 0, 0, 0, time.UTC)
			if col.date.Weekday() == weekday {
				valid = append(valid, col)
			}
		}
		out[weekday] = valid
	}
	return out
}

func wigRows(cells []pdfCell, dayCol, slotCol wigColumn) []wigRow {
	var rows []wigRow
	for _, s := range cells {
		if math.Abs(s.x0-slotCol.x0) > pdfTol {
			continue
		}
		slot := s.text("")
		if _, ok := slotMap[slot]; !ok {
			continue
		}
		for _, d := range cells {
			if d.page != s.page || math.Abs(d.x0-dayCol.x0) > pdfTol || d.midY() < s.y0 || d.midY() > s.y1 {
				continue
			}
			if weekday, ok := dayMap[d.text(" ")]; ok {
				rows = append(rows, wigRow{page: s.page, y0: s.y0, y1: s.y1, weekday: weekday, slot: slot})
				break
			}
		}
	}
	return rows
}

func wigCellRows(rows []wigRow, cell pdfCell) []wigRow {
	var out []wigRow
	for _, r := range rows {
		if r.page != cell.page {
			continue
		}
		if mid := (r.y0 + r.y1) / 2; mid > cell.y0 && mid < cell.y1 {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].y0 > out[j].y0 })
	for _, r := range out[1:] {
		if r.weekday != out[0].weekday {
			return out[:1]
		}
	}
	return out
}

func wigReadLegend(cells []pdfCell, legendX float64) wigLegend {
	legend := wigLegend{
		subjects:        map[string]string{},
		teachers:        map[string]string{},
		subjectTeachers: map[string]map[string]string{},
	}

	var abbrs, names []pdfCell
	for _, c := range cells {
		switch {
		case math.Abs(c.x0-legendX) < pdfTol:
			abbrs = append(abbrs, c)
		case c.x0 > legendX+pdfTol:
			names = append(names, c)
		}
	}

	current := ""
	for _, a := range abbrs {
		var name pdfCell
		found := false
		for _, n := range names {
			if n.page == a.page && n.midY() > a.y0 && n.midY() < a.y1 {
				name, found = n, true
				break
			}
		}
		if !found || len(a.lines) == 0 {
			continue
		}
		code := a.lines[0].text
		full := name.text(" ")
		if name.lines[0].bold {
			legend.subjects[code] = full
			current = code
			continue
		}
		legend.teachers[code] = full
		if current == "" || len(a.lines) < 2 {
			continue
		}
		if m := wigCountRe.FindStringSubmatch(a.lines[1].text); m != nil {
			if legend.subjectTeachers[current] == nil {
				legend.subjectTeachers[current] = map[string]string{}
			}
			legend.subjectTeachers[current][m[1]] = full
		}
	}
	return legend
}

func wigEvent(cell pdfCell, legend wigLegend) (model.Event, bool) {
	var head []string
	var rest []pdfLine
	for i, l := range cell.lines {
		if l.bold && len(rest) == 0 {
			head = append(head, l.text)
			continue
		}
		rest = cell.lines[i:]
		break
	}
	if len(head) == 0 {
		return model.Event{}, false
	}

	abbr, code := strings.Join(head, " "), ""
	if m := wigEventRe.FindStringSubmatch(abbr); m != nil {
		abbr, code = strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
	}

	event := model.Event{Subject: abbr, Color: cell.color}
	if name := legend.subjects[abbr]; name != "" {
		event.Subject = name
	}
	switch {
	case typeMap[code] != "":
		event.Type = typeMap[code]
	case wigTypes[code] != "":
		event.Type = wigTypes[code]
	case code == "E" || code == "Ep":
		event.Type = "egzamin"
		if code == "Ep" {
			event.Type = "egzamin poprawkowy"
		}
	default:
		event.Type = code
	}

	var roomText, teacherText []string
	for _, l := range rest {
		if strings.ContainsAny(l.text, "0123456789") || (len(roomText) > 0 && len(teacherText) == 0 && strings.HasSuffix(roomText[len(roomText)-1], ";")) {
			roomText = append(roomText, l.text)
		} else {
			teacherText = append(teacherText, l.text)
		}
	}
	event.Room = wigRooms(strings.Join(roomText, " "))

	var teachers []string
	for t := range strings.SplitSeq(strings.Join(teacherText, " "), ";") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if name := legend.teachers[t]; name != "" {
			t = name
		}
		teachers = appendUnique(teachers, t)
	}
	if len(teachers) == 0 {
		if name := legend.subjectTeachers[abbr][code]; name != "" {
			teachers = append(teachers, name)
		}
	}
	event.Teacher = strings.Join(teachers, ", ")
	return event, true
}

func wigRooms(s string) string {
	var rooms []string
	for r := range strings.SplitSeq(s, ";") {
		r = strings.Join(strings.Fields(strings.ReplaceAll(r, "/", " ")), " ")
		if r != "" {
			rooms = append(rooms, r)
		}
	}
	if n := len(rooms); n > 2 && len(strings.Fields(rooms[n-1])) < len(strings.Fields(rooms[0])) {
		rooms = rooms[:n-1]
	}
	return strings.Join(rooms, ", ")
}
