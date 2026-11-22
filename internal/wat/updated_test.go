package wat

import (
	"os"
	"testing"
)

func TestIndexUpdatedFixtures(t *testing.T) {
	cases := []struct {
		file string
		want string
		ok   bool
	}{
		{"index_wml.xml", "2026-11-19 14:07:56", true},
		{"index_wel.xml", "2026-11-20 15:16:20", true},
		{"index_ioe.xml", "2026-11-20 14:57:48", true},
		{"index_wtc.xml", "2026-11-20 14:42:51", true},
		{"index_wlo.html", "2026-11-20 14:14:48", true},
		{"index_wim.html", "2026-11-22 09:39:50", true},
		{"index_wcy.html", "", false},
		{"wig_index.html", "", false},
	}
	for _, c := range cases {
		body, err := os.ReadFile("testdata/" + c.file)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := IndexUpdated(body)
		if ok != c.ok {
			t.Errorf("%s: ok = %v, chcę %v", c.file, ok, c.ok)
			continue
		}
		if ok && got.Format("2006-01-02 15:04:05") != c.want {
			t.Errorf("%s: %s, chcę %s", c.file, got.Format("2006-01-02 15:04:05"), c.want)
		}
		if ok && got.Location().String() != "Europe/Warsaw" {
			t.Errorf("%s: strefa %s", c.file, got.Location())
		}
	}
}

func TestPlanUpdatedFixtures(t *testing.T) {
	cases := []struct{ file, want string }{
		{"plan_wat.html", "2026-10-07 13:25:42"},
		{"wcy_WME26BX2S1.html", "2026-11-11 00:41:06"},
	}
	for _, c := range cases {
		body, err := os.ReadFile("testdata/" + c.file)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := PlanUpdated(body)
		if !ok || got.Format("2006-01-02 15:04:05") != c.want {
			t.Errorf("%s: %v %v, chcę %s", c.file, got, ok, c.want)
		}
	}
}

func TestParseUpdatedFormats(t *testing.T) {
	cases := map[string]string{
		"19.11.2026 14:07:56": "2026-11-19 14:07:56",
		"2026-11-11 00:41:06": "2026-11-11 00:41:06",
		"19.11.2026 14:07":    "2026-11-19 14:07:00",
	}
	for in, want := range cases {
		got, ok := ParseUpdated(in)
		if !ok || got.Format("2006-01-02 15:04:05") != want {
			t.Errorf("%q: %v %v, chcę %s", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "jutro", "32.13.2026 25:00:00"} {
		if _, ok := ParseUpdated(bad); ok {
			t.Errorf("%q: chcę błąd", bad)
		}
	}
	winter, _ := ParseUpdated("19.11.2026 14:07:56")
	summer, _ := ParseUpdated("19.06.2026 14:07:56")
	if winter.UTC().Hour() != 13 || summer.UTC().Hour() != 12 {
		t.Errorf("czas zimowy/letni: %v / %v", winter.UTC(), summer.UTC())
	}
}
