package wat

import (
	"strings"
	"testing"
)

func markerTable(row string) string {
	return `<table>` +
		`<tr><td>pon.</td><td>&nbsp</td><td>28 IX</td><td>05 X</td><td>12 X</td><td>19 X</td><td>26 X</td><td>&nbsp</td><td>&nbsp</td></tr>` +
		`<tr><td>pon.</td><td>1-2</td>` + row + `</tr>` +
		`</table>`
}

func parseSubjects(t *testing.T, html string) []string {
	t.Helper()
	events, err := ParsePlan(strings.NewReader(html), 2026)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Subject+"|"+e.Type)
	}
	return out
}

func TestPlaceholderCellsAreSkipped(t *testing.T) {
	cells := []string{"I", "Ing", "REZ", "SSW", "X", "E", "Ep", "Św."}
	row := `<td>Przedmiot</td><td>&nbsp</td>`
	for _, c := range cells {
		row += "<td>" + c + "</td>"
	}

	got := parseSubjects(t, markerTable(row))
	if len(got) != 1 || got[0] != "Przedmiot|" {
		t.Fatalf("zdarzenia = %v, chcę tylko [\"Przedmiot|\"]", got)
	}
}

func TestTypeMarkerWithSubjectIsKept(t *testing.T) {
	row := `<td>Przedmiot<BR/><FONT>I</FONT></td><td>&nbsp</td><td>&nbsp</td><td>&nbsp</td><td>&nbsp</td><td>&nbsp</td><td>&nbsp</td>`

	got := parseSubjects(t, markerTable(row))
	if len(got) != 1 || got[0] != "Przedmiot|inne" {
		t.Fatalf("zdarzenia = %v, chcę [\"Przedmiot|inne\"]", got)
	}
}
