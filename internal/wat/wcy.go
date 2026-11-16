package wat

import (
	"errors"
	"html"
	"io"
	"maps"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/billypuding/watplan/internal/model"
)

const FormatWCY = "wcy"

var (
	wcyInfoRe  = regexp.MustCompile(`^(.*?) - \((.*?)\)(?:\s*-\s*(.*))?$`)
	wcyBreakRe = regexp.MustCompile(`(?i)<br\s*/?>`)
	wcyBlockRe = regexp.MustCompile(`\bblock(\d+)\b`)
	wcyDays    = map[string]time.Weekday{
		"pon": time.Monday, "wt": time.Tuesday, "sr": time.Wednesday, "czw": time.Thursday,
		"pt": time.Friday, "sob": time.Saturday, "nd": time.Sunday,
	}
	wcySpacesRe = regexp.MustCompile(`\s+`)
	wcySkipped  = map[string]bool{
		"rezerwacja":                true,
		"przerwa międzysemestralna": true,
	}
	wcyDefaultBlocks = map[string][2]string{
		"block1": {"08:00", "09:35"},
		"block2": {"09:50", "11:25"},
		"block3": {"11:40", "13:15"},
		"block4": {"13:30", "15:05"},
		"block5": {"16:00", "17:35"},
		"block6": {"17:50", "19:25"},
		"block7": {"19:40", "21:15"},
	}
)

func Parse(format string, r io.Reader, startYear int) ([]model.Event, error) {
	switch format {
	case FormatWCY:
		return ParseWCY(r)
	case FormatWIGPDF:
		return ParseWIGPDF(r, startYear)
	}
	return ParsePlan(r, startYear)
}

func ParseWCY(r io.Reader) ([]model.Event, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	if doc.Find(".lessons").Length() == 0 && doc.Find(".rozklad_container").Length() == 0 {
		return nil, errors.New("strona nie zawiera rozkładu WCY")
	}

	blocks := wcyBlockTimes(doc)
	var events []model.Event
	doc.Find(".lessons .lesson").Each(func(_ int, lesson *goquery.Selection) {
		date := strings.ReplaceAll(strings.TrimSpace(lesson.Find(".date").Text()), "_", "-")
		day, err := time.Parse("2006-01-02", date)
		if err != nil {
			return
		}
		times, ok := blocks.of(day.Weekday(), strings.TrimSpace(lesson.Find(".block_id").Text()))
		if !ok {
			return
		}

		m := wcyInfoRe.FindStringSubmatch(cleanText(lesson.Find(".info").Text()))
		if m == nil {
			return
		}
		subject := strings.TrimRight(strings.TrimSpace(m[1]), ".")
		kind := wcyKind(m[2])
		teacher := strings.TrimSpace(m[3])
		if subject == "" || subject == "-" || wcySkipped[kind] {
			return
		}
		if teacher == "-" {
			teacher = ""
		}

		events = append(events, model.Event{
			Date:    date,
			Start:   times[0],
			End:     times[1],
			Subject: subject,
			Type:    wcyType(kind),
			Teacher: teacher,
			Room:    wcyRoom(lesson.Find(".name")),
			Color:   strings.ToUpper(strings.TrimSpace(lesson.Find(".colorp").Text())),
		})
	})

	model.SortEvents(events)
	return events, nil
}

type wcyBlocks map[time.Weekday]map[string][2]string

func (b wcyBlocks) of(day time.Weekday, block string) ([2]string, bool) {
	if t, ok := b[day][block]; ok {
		return t, true
	}
	t, ok := wcyDefaultBlocks[block]
	return t, ok
}

func wcyBlockTimes(doc *goquery.Document) wcyBlocks {
	blocks := wcyBlocks{}
	doc.Find(".day_v1").Each(func(_ int, d *goquery.Selection) {
		class, _ := d.Attr("class")
		var day time.Weekday
		found := false
		for _, c := range strings.Fields(class) {
			if wd, ok := wcyDays[c]; ok {
				day, found = wd, true
			}
		}
		if !found {
			return
		}
		times := make(map[string][2]string, len(wcyDefaultBlocks))
		maps.Copy(times, wcyDefaultBlocks)
		d.Find(".day_blocks .block_nr").Each(func(_ int, b *goquery.Selection) {
			bc, _ := b.Attr("class")
			m := wcyBlockRe.FindStringSubmatch(bc)
			start := strings.TrimSpace(b.Find(".hr1").Text())
			end := strings.TrimSpace(b.Find(".hr2").Text())
			if m != nil && len(start) == 5 && len(end) == 5 {
				times["block"+m[1]] = [2]string{start, end}
			}
		})
		blocks[day] = times
	})
	return blocks
}

func wcyKind(raw string) string {
	kind := strings.ToLower(wcySpacesRe.ReplaceAllString(strings.TrimSpace(raw), " "))
	return strings.TrimRight(kind, ". ")
}

func wcyType(kind string) string {
	switch {
	case kind == "wykład":
		return "wykład"
	case strings.HasPrefix(kind, "ćwicz"):
		return "ćwiczenia"
	case strings.HasPrefix(kind, "labor"):
		return "laboratorium"
	case strings.HasPrefix(kind, "zaliczenie poprawkowe"), strings.HasPrefix(kind, "zp"):
		return "zaliczenie poprawkowe"
	case kind == "inny":
		return "inne"
	}
	return kind
}

func wcyRoom(name *goquery.Selection) string {
	inner, err := name.Html()
	if err != nil {
		return ""
	}
	parts := wcyBreakRe.Split(inner, -1)
	if len(parts) < 3 {
		return ""
	}
	room := strings.TrimSpace(html.UnescapeString(parts[2]))
	room = strings.TrimRight(room, ", ")
	room = strings.ReplaceAll(room, "/ ", " ")
	if room == "-" {
		return ""
	}
	return wcySpacesRe.ReplaceAllString(room, " ")
}
