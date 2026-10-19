package wat

import (
	"io"
	"log"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/billypuding/watplan/internal/model"
)

var typeMap = map[string]string{
	"w":   "wykład",
	"W":   "wykład",
	"ć":   "ćwiczenia",
	"Ćw":  "ćwiczenia",
	"Ćw.": "ćwiczenia",
	"ćw":  "ćwiczenia",
	"ćw.": "ćwiczenia",
	"L":   "laboratorium",
	"S":   "seminarium",
	"P":   "projekt",
	"Z":   "zaliczenie",
	"Zp":  "zaliczenie poprawkowe",
	"SSW": "szkolenie",
	"I":   "inne",
	"Ing": "inne",
	"Św.": "święto",
	"DYS": "dyspozycja",
}

var ignoredFields = map[string]bool{
	"X":   true,
	"REZ": true,
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

	evidence := collectEvidence(grid, legendCol)
	subjects, teachers, subjectTeachers, legendTeachers := buildLegend(grid, legendCol, evidence)
	for code, name := range inferTeacherCodes(grid, legendCol, subjects, legendTeachers) {
		if teachers[code] == "" {
			teachers[code] = name
		}
	}
	index := newTeacherIndex(teachers)

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
			cellEvents, err := eventsFromCell(cell, block, subjects, index, subjectTeachers)
			if err != nil {
				log.Printf("pominięto uszkodzoną komórkę: %v", err)
				continue
			}
			events = append(events, cellEvents...)
		}
	}

	model.SortEvents(events)
	return events, nil
}

func eventsFromCell(cell Cell, block dayBlock, subjects map[string]string, index *teacherIndex, subjectTeachers map[string]map[string]string) ([]model.Event, error) {
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

	event, ok, err := parseCellFields(cell, subjects, index, subjectTeachers)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
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

func parseCellFields(cell Cell, subjects map[string]string, index *teacherIndex, subjectTeachers map[string]map[string]string) (model.Event, bool, error) {
	color, _ := cell.Node.Attr("bgcolor")
	event := model.Event{Color: strings.ToUpper(color)}

	var teachers []string
	var unknown []cellField
	var rawSubjectAbbr string
	var rawTypeAbbr string

	fields := collectFields(cell.Node)
	for i, f := range fields {
		field := f.text
		teacherName, strong := index.strong(field)

		if i == 0 {
			rawSubjectAbbr = field
		}
		if typeMap[field] != "" {
			rawTypeAbbr = field
		}

		switch {
		case subjects[field] != "":
			event.Subject = subjects[field]
			rawSubjectAbbr = field
		case typeMap[field] != "":
			event.Type = typeMap[field]
		case ignoredFields[field]:
		case field == "E" || field == "Ep":
			if event.Type != "" {
				event.Type += " (" + field + ")"
			} else {
				event.Type = "Egzamin"
			}
		case strong:
			teachers = appendUnique(teachers, teacherName)
		case isRoomField(field):
			if event.Room != "" {
				event.Room += ", " + normalizeRoom(field)
			} else {
				event.Room = normalizeRoom(field)
			}
		default:
			if names := resolveTeachers(field, index); len(names) > 0 {
				teachers = appendUnique(teachers, names...)
			} else {
				unknown = append(unknown, f)
			}
		}
	}

	if event.Subject == "" {
		for i, f := range unknown {
			if !f.bold {
				event.Subject = f.text
				unknown = append(unknown[:i], unknown[i+1:]...)
				break
			}
		}
		if event.Subject == "" && len(unknown) > 0 {
			event.Subject = unknown[0].text
			unknown = unknown[1:]
		}
	}

	if len(teachers) == 0 {
		for _, f := range unknown {
			if f.bold && plausibleAbbr(f.text) {
				teachers = append(teachers, f.text)
				break
			}
		}
	}

	if len(teachers) == 0 {
		teachers = scanTeacherTokens(cell, index)
	}

	if len(teachers) == 0 && rawSubjectAbbr != "" && rawTypeAbbr != "" {
		if tm, ok := subjectTeachers[rawSubjectAbbr]; ok {
			if teacherName, has := tm[rawTypeAbbr]; has {
				teachers = append(teachers, teacherName)
			}
		}
	}

	event.Teacher = strings.Join(teachers, ", ")

	if event.Subject == "" {
		rawText := strings.TrimSpace(cell.Text)
		if rawText == "" || placeholderOnly(fields) {
			return model.Event{}, false, nil
		}
		event.Subject = rawText
	}

	return event, true, nil
}

func placeholderOnly(fields []cellField) bool {
	if len(fields) == 0 {
		return false
	}
	for _, f := range fields {
		if typeMap[f.text] == "" && !ignoredFields[f.text] && f.text != "E" && f.text != "Ep" {
			return false
		}
	}
	return true
}
