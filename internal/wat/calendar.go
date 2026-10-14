package wat

import (
	"fmt"
	"strconv"
	"strings"
	"time"
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
				return nil, fmt.Errorf("data %s nie wypada w dniu %q", date.Format(dateLayout), row[0].Text)
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

func cellTimes(cell Cell, block dayBlock) (start, end string, err error) {
	first := cell.Row - block.HeaderRow - 1
	last := first + cell.RowSpan - 1
	if first < 0 || last < first || last >= len(slotLabels) {
		return "", "", fmt.Errorf("wiersz %d, kolumna %d: zajęcia poza siatką godzin", cell.Row, cell.Col)
	}
	return slotMap[slotLabels[first]].Start, slotMap[slotLabels[last]].End, nil
}

func blockForRow(blocks []dayBlock, row int) (dayBlock, bool) {
	for _, b := range blocks {
		if row > b.HeaderRow && row <= b.HeaderRow+slotsPerDay {
			return b, true
		}
	}
	return dayBlock{}, false
}
