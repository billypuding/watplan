package wat

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/billypuding/watplan/internal/model"
)

const (
	firstWeekCol = 2
	slotsPerDay  = 7
	dateLayout   = "2006-01-02"
)

type timeSlot struct {
	Start string
	End   string
}

type pos struct{ r, c int }

type Cell struct {
	Text    string
	ColSpan int
	RowSpan int
	Row     int
	Col     int
	Node    *goquery.Selection
}

type dayBlock struct {
	Weekday   time.Weekday
	HeaderRow int
	Dates     map[int]time.Time
}

var dayMap = map[string]time.Weekday{
	"pon.":   time.Monday,
	"wt.":    time.Tuesday,
	"śr.":    time.Wednesday,
	"czw.":   time.Thursday,
	"pt.":    time.Friday,
	"sob.":   time.Saturday,
	"niedz.": time.Sunday,
}

var monthMap = map[string]time.Month{
	"I":    time.January,
	"II":   time.February,
	"III":  time.March,
	"IV":   time.April,
	"V":    time.May,
	"VI":   time.June,
	"VII":  time.July,
	"VIII": time.August,
	"IX":   time.September,
	"X":    time.October,
	"XI":   time.November,
	"XII":  time.December,
}

var typeMap = map[string]string{
	"w": "wykład",
	"ć": "ćwiczenia",
	"L": "laboratorium",
}

var slotLabels = []string{"1-2", "3-4", "5-6", "7-8", "9-10", "11-12", "13-14"}

var slotMap = map[string]timeSlot{
	"1-2":   {"08:00", "09:35"},
	"3-4":   {"09:50", "11:25"},
	"5-6":   {"11:40", "13:15"},
	"7-8":   {"13:30", "15:05"},
	"9-10":  {"16:00", "17:35"},
	"11-12": {"17:50", "19:25"},
	"13-14": {"19:40", "21:15"},
}

var teacherTitles = []string{
	"dr ", "mgr ", "prof. ", "inż. ", "doc. ",
	"kpt. ", "por. ", "ppor. ", "ppłk ", "mjr ", "płk ", "gen. ",
}

func ParsePlan(r io.Reader, startYear int) ([]model.Event, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}

	grid := buildGrid(doc)
	width := gridWidth(grid)
	legendCol := width - 2

	blocks, err := findDays(grid, legendCol, startYear)
	if err != nil {
		return nil, err
	}
	subjects, teachers := buildLegend(grid, legendCol)

	var events []model.Event
	for _, row := range grid {
		if len(row) == 0 {
			continue
		}
		block, ok := blockForRow(blocks, row[0].Row)
		if !ok {
			continue
		}
		for _, cell := range row {
			cellEvents, err := eventsFromCell(cell, block, subjects, teachers)
			if err != nil {
				return nil, err
			}
			events = append(events, cellEvents...)
		}
	}

	model.SortEvents(events)
	return events, nil
}

func buildGrid(doc *goquery.Document) [][]Cell {
	occupied := make(map[pos]bool)
	var grid [][]Cell

	doc.Find("table tr").Each(func(i int, tr *goquery.Selection) {
		col := 0
		var row []Cell

		tr.ChildrenFiltered("td, th").Each(func(_ int, td *goquery.Selection) {
			for occupied[pos{i, col}] {
				col++
			}

			colspan := spanAttr(td, "colspan")
			rowspan := spanAttr(td, "rowspan")

			row = append(row, Cell{
				Text:    strings.TrimSpace(td.Text()),
				ColSpan: colspan,
				RowSpan: rowspan,
				Row:     i,
				Col:     col,
				Node:    td,
			})

			for r := 0; r < rowspan; r++ {
				for c := 0; c < colspan; c++ {
					occupied[pos{i + r, col + c}] = true
				}
			}
			col += colspan
		})

		grid = append(grid, row)
	})

	return grid
}

func spanAttr(s *goquery.Selection, name string) int {
	val, ok := s.Attr(name)
	if !ok {
		return 1
	}
	num, err := strconv.Atoi(val)
	if err != nil || num < 1 {
		return 1
	}
	return num
}

func gridWidth(grid [][]Cell) int {
	width := 0
	for _, row := range grid {
		for _, cell := range row {
			if end := cell.Col + cell.ColSpan; end > width {
				width = end
			}
		}
	}
	return width
}

func findDays(grid [][]Cell, legendCol, startYear int) ([]dayBlock, error) {
	var blocks []dayBlock

	for _, row := range grid {
		if len(row) == 0 {
			continue
		}
		weekday, ok := dayMap[row[0].Text]
		if !ok {
			continue
		}

		block := dayBlock{
			Weekday:   weekday,
			HeaderRow: row[0].Row,
			Dates:     make(map[int]time.Time),
		}

		year := startYear
		var prevMonth time.Month
		for _, cell := range row {
			if cell.Col < firstWeekCol || cell.Col >= legendCol || cell.Text == "" {
				continue
			}

			day, month, err := parseDayMonth(cell.Text)
			if err != nil {
				return nil, err
			}
			if month < prevMonth {
				year++
			}
			prevMonth = month

			date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
			if date.Weekday() != weekday {
				return nil, fmt.Errorf("data %s nie wypada w dniu %q (zły start_year?)", date.Format(dateLayout), row[0].Text)
			}
			block.Dates[cell.Col] = date
		}

		blocks = append(blocks, block)
	}

	return blocks, nil
}

func parseDayMonth(s string) (int, time.Month, error) {
	parts := strings.Fields(s)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("nieprawidłowy format daty: %q", s)
	}

	day, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("nieprawidłowy dzień %q w dacie %q", parts[0], s)
	}

	month, ok := monthMap[parts[1]]
	if !ok {
		return 0, 0, fmt.Errorf("nieprawidłowy miesiąc %q w dacie %q", parts[1], s)
	}

	return day, month, nil
}

func buildLegend(grid [][]Cell, legendCol int) (subjects, teachers map[string]string) {
	subjects = make(map[string]string)
	teachers = make(map[string]string)

	for _, row := range grid {
		var abbr, name string
		for _, cell := range row {
			switch cell.Col {
			case legendCol:
				abbr = cell.Text
			case legendCol + 1:
				name = cell.Text
			}
		}
		if abbr == "" || name == "" {
			continue
		}

		if isTeacherName(name) {
			teachers[abbr] = name
		} else {
			subjects[abbr] = name
		}
	}

	return subjects, teachers
}

func isTeacherName(name string) bool {
	for _, title := range teacherTitles {
		if strings.HasPrefix(name, title) {
			return true
		}
	}
	return false
}

func blockForRow(blocks []dayBlock, row int) (dayBlock, bool) {
	for _, b := range blocks {
		if row > b.HeaderRow && row <= b.HeaderRow+slotsPerDay {
			return b, true
		}
	}
	return dayBlock{}, false
}

func eventsFromCell(cell Cell, block dayBlock, subjects, teachers map[string]string) ([]model.Event, error) {
	if cell.Col < firstWeekCol || cell.Text == "" {
		return nil, nil
	}

	var dates []string
	for c := cell.Col; c < cell.Col+cell.ColSpan; c++ {
		if date, ok := block.Dates[c]; ok {
			dates = append(dates, date.Format(dateLayout))
		}
	}
	if len(dates) == 0 {
		return nil, nil
	}

	event, err := parseCellFields(cell, subjects, teachers)
	if err != nil {
		return nil, err
	}

	event.Start, event.End, err = cellTimes(cell, block)
	if err != nil {
		return nil, err
	}

	events := make([]model.Event, 0, len(dates))
	for _, date := range dates {
		event.Date = date
		events = append(events, event)
	}
	return events, nil
}

func parseCellFields(cell Cell, subjects, teachers map[string]string) (model.Event, error) {
	color, _ := cell.Node.Attr("bgcolor")
	event := model.Event{Color: strings.ToUpper(color)}

	var firstUnknown string

	cell.Node.Find("font").Each(func(_ int, s *goquery.Selection) {
		field := strings.TrimSpace(s.Text())
		if field == "" {
			return
		}

		switch {
		case subjects[field] != "":
			event.Subject = subjects[field]
		case typeMap[field] != "":
			event.Type = typeMap[field]
		case isRoomField(field):
			if event.Room != "" {
				event.Room += ", " + normalizeRoom(field)
			} else {
				event.Room = normalizeRoom(field)
			}
		case field == "E" || field == "Ep":
			if event.Type != "" {
				event.Type += " (" + field + ")"
			} else {
				event.Type = "Egzamin"
			}
		default:
			if name, ok := matchTeacher(field, teachers); ok {
				event.Teacher = name
			} else {
				if firstUnknown == "" {
					firstUnknown = field
				}
			}
		}
	})

	if event.Subject == "" {
		if firstUnknown != "" {
			event.Subject = firstUnknown
		} else {
			rawText := strings.TrimSpace(cell.Text)
			if rawText != "" {
				event.Subject = rawText
			} else {
				return model.Event{}, fmt.Errorf("wiersz %d, kolumna %d: brak przedmiotu w komórce", cell.Row, cell.Col)
			}
		}
	}

	return event, nil
}

func cellTimes(cell Cell, block dayBlock) (start, end string, err error) {
	first := cell.Row - block.HeaderRow - 1
	last := first + cell.RowSpan - 1
	if first < 0 || last < first || last >= len(slotLabels) {
		return "", "", fmt.Errorf("wiersz %d, kolumna %d: zajęcia poza siatką godzin", cell.Row, cell.Col)
	}
	return slotMap[slotLabels[first]].Start, slotMap[slotLabels[last]].End, nil
}

func normalizeRoom(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, ";", " ; ")), " ")
}

func matchTeacher(code string, teachers map[string]string) (string, bool) {
	code = strings.TrimRight(code, ";")
	if code == "" {
		return "", false
	}
	if name, ok := teachers[code]; ok {
		return name, true
	}

	var match string
	count := 0
	for abbr, name := range teachers {
		if strings.HasPrefix(abbr, code) {
			match = name
			count++
		}
	}
	if count != 1 {
		return "", false
	}
	return match, true
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
