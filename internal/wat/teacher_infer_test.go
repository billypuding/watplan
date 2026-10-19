package wat

import (
	"fmt"
	"strings"
	"testing"
)

const inferRow = `<tr><td></td><td></td><td></td><td></td><td></td><td>%s</td><td>%s</td></tr>`

func inferTable(legendRows ...string) string {
	var b strings.Builder
	b.WriteString("<table>")
	b.WriteString(`<tr><td></td><td></td><td>28 IX</td><td>05 X</td><td>12 X</td><td></td><td></td></tr>`)
	b.WriteString(`<tr><td>pon.</td><td></td><td>28 IX</td><td>05 X</td><td>12 X</td><td></td><td></td></tr>`)
	b.WriteString(`<tr><td>pon.</td><td>1-2</td><td><B><FONT style="font-size:10px;">Ain</FONT></B><BR/><FONT style="font-size:8px;">w</FONT><BR/><FONT style="font-size:8px;">101</FONT><BR/><FONT style="font-size:8px;">BA</FONT></td><td></td><td></td><td></td><td></td></tr>`)
	for _, r := range legendRows {
		b.WriteString(r)
	}
	b.WriteString("</table>")
	return b.String()
}

func parseInfer(t *testing.T, html string) []string {
	t.Helper()
	events, err := ParsePlan(strings.NewReader(html), 2026)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	teachers := make([]string, 0, len(events))
	for _, e := range events {
		teachers = append(teachers, e.Subject+"|"+e.Teacher)
	}
	return teachers
}

func TestInferTeacherCodeFromLegend(t *testing.T) {
	html := inferTable(
		fmt.Sprintf(inferRow, "Ain", "Analiza instrumentalna"),
		fmt.Sprintf(inferRow, "", "Bąk Adam"),
	)

	got := parseInfer(t, html)
	if len(got) != 1 {
		t.Fatalf("zdarzeń = %d, chcę 1 (%v)", len(got), got)
	}
	if got[0] != "Analiza instrumentalna|Bąk Adam" {
		t.Errorf("otrzymano %q, chcę %q", got[0], "Analiza instrumentalna|Bąk Adam")
	}
}

func TestInferTeacherCodeAmbiguousStaysEmpty(t *testing.T) {
	html := inferTable(
		fmt.Sprintf(inferRow, "Ain", "Analiza instrumentalna"),
		fmt.Sprintf(inferRow, "", "Bąk Adam"),
		fmt.Sprintf(inferRow, "", "Bąk Andrzej"),
	)

	got := parseInfer(t, html)
	if len(got) != 1 {
		t.Fatalf("zdarzeń = %d, chcę 1 (%v)", len(got), got)
	}
	if got[0] != "Analiza instrumentalna|" {
		t.Errorf("otrzymano %q, chcę pustego nauczyciela", got[0])
	}
}

func TestTeacherCodeKeys(t *testing.T) {
	cases := []struct {
		name string
		code string
		want bool
	}{
		{"Bąk Adam", "BA", true},
		{"Budzyńska Edyta", "BuE", true},
		{"Wolańska Izabela", "IzWo", true},
		{"Hara Marcin", "HMa", true},
		{"Kuśmierek Krzysztof", "Kuś", true},
		{"Kuligowska Monika", "KulMo", true},
		{"Szemraj Tomasz", "T.Szem", true},
		{"Oszczak-Nowińska Agata", "Osz-Now. A", true},
		{"Grabka Michał", "Gr. M.", true},
		{"Nawała Jakub", "Nwł", true},
		{"Klimek Krzysztof", "Klim", true},
		{"Bąk Adam", "ZZZ", false},
	}

	for _, c := range cases {
		keys := map[string]bool{}
		for _, k := range teacherCodeKeys(c.name) {
			keys[k] = true
		}
		got := keys[normKey(c.code)]
		if got != c.want {
			t.Errorf("%s vs %q: dopasowanie = %v, chcę %v", c.name, c.code, got, c.want)
		}
	}
}
