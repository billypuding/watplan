package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigValidation(t *testing.T) {
	cases := []struct {
		name, json, want string
	}{
		{"zły JSON", `[{"id":`, "config"},
		{"złe id", `[{"id":"A B","url":"https://x/a.htm","start_year":2026}]`, "nieprawidłowe id"},
		{"puste id", `[{"id":"","url":"https://x/a.htm","start_year":2026}]`, "nieprawidłowe id"},
		{"powtórzone id", `[{"id":"A1","url":"https://x/a.htm","start_year":2026},{"id":"A1","url":"https://x/b.htm","start_year":2026}]`, "powtórzone id"},
		{"brak url", `[{"id":"A1","start_year":2026}]`, "nie ma adresu url"},
		{"brak roku", `[{"id":"A1","url":"https://x/a.htm"}]`, "start_year"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := LoadConfig(writeFile(t, "plans.json", c.json))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("błąd %v, chcę zawierający %q", err, c.want)
			}
		})
	}
}

func TestLoadConfigDefaultsNameAndReportsMissingFile(t *testing.T) {
	plans, err := LoadConfig(writeFile(t, "plans.json", `[{"id":"A1","url":"https://x/a.htm","start_year":2026},{"id":"B2","name":"Grupa B","url":"https://x/b.htm","start_year":2026}]`))
	if err != nil {
		t.Fatal(err)
	}
	if plans[0].Name != "A1" || plans[1].Name != "Grupa B" {
		t.Fatalf("nazwy %q, %q", plans[0].Name, plans[1].Name)
	}
	if _, err := LoadConfig(filepath.Join(t.TempDir(), "brak.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("brak pliku: %v, chcę fs.ErrNotExist (main startuje wtedy z pustą listą)", err)
	}
}

func TestLoadSources(t *testing.T) {
	sources, err := LoadSources(writeFile(t, "sources.json", `[{"faculty":"WML","index_url":"https://x/index.xml","start_year":2026,"optional":true}]`))
	if err != nil || len(sources) != 1 || !sources[0].Optional {
		t.Fatalf("%v %+v", err, sources)
	}
	for name, content := range map[string]string{
		"zły JSON":       `{`,
		"brak faculty":   `[{"index_url":"https://x/index.xml"}]`,
		"brak index_url": `[{"faculty":"WML"}]`,
	} {
		if _, err := LoadSources(writeFile(t, "sources.json", content)); err == nil {
			t.Errorf("%s: brak błędu", name)
		}
	}
}

func TestRegistryCopiesAndIsSafeConcurrently(t *testing.T) {
	initial := []PlanConfig{{ID: "A1"}, {ID: "B2"}}
	reg := NewRegistry(initial)
	initial[0].ID = "ZMIENIONE"
	if _, ok := reg.Get("A1"); !ok {
		t.Fatal("rejestr nie skopiował listy startowej")
	}
	list := reg.List()
	list[1].ID = "ZMIENIONE"
	if _, ok := reg.Get("B2"); !ok {
		t.Fatal("List zwraca wspólną tablicę zamiast kopii")
	}
	if _, ok := reg.Get("NIEMA"); ok {
		t.Fatal("Get znalazł nieistniejący plan")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			reg.Set([]PlanConfig{{ID: fmt.Sprintf("P%d", i)}})
		}(i)
		go func() {
			defer wg.Done()
			reg.List()
			reg.Get("P1")
		}()
	}
	wg.Wait()
	if n := len(reg.List()); n != 1 {
		t.Fatalf("po Set jest %d planów", n)
	}
}

func TestMergeExtra(t *testing.T) {
	plans := []PlanConfig{
		{ID: "WML1", Faculty: "WML", URL: "https://x/WML1.htm"},
		{ID: "WCY1", Faculty: "WCY", URL: "https://y/WCY1"},
	}
	found := []PlanConfig{
		{ID: "WML1", Faculty: "WML", URL: "https://x/WML1.htm"},
		{ID: "WML1", Faculty: "WML", URL: "https://x/inne/WML1.htm", Format: "pdf", StartYear: 2026},
		{ID: "WML1", Faculty: "WML", URL: "https://x/inne/WML1.htm"},
		{ID: "WCY2", Faculty: "WCY", URL: "https://y/WCY2"},
	}
	out, added := MergeExtra(plans, found)
	if added != 2 {
		t.Fatalf("dodano %d, chcę 2 (nowa grupa i jedno dodatkowe źródło)", added)
	}
	ids := []string{}
	for _, p := range out {
		ids = append(ids, p.ID)
	}
	if strings.Join(ids, ",") != "WCY1,WCY2,WML1" {
		t.Fatalf("kolejność %v", ids)
	}
	if extra := out[2].Extra; len(extra) != 1 || extra[0].URL != "https://x/inne/WML1.htm" || extra[0].Format != "pdf" {
		t.Fatalf("extra %+v", extra)
	}
	if len(plans[0].Extra) != 0 {
		t.Fatal("MergeExtra zmienił wejściową listę")
	}
}

func TestCommittedConfigIsValid(t *testing.T) {
	plans, err := LoadConfig("../../plans.json")
	if err != nil {
		t.Fatalf("plans.json: %v", err)
	}
	sources, err := LoadSources("../../sources.json")
	if err != nil {
		t.Fatalf("sources.json: %v", err)
	}
	if len(plans) < 500 {
		t.Fatalf("plans.json ma tylko %d grup", len(plans))
	}
	faculties := map[string]bool{}
	for _, s := range sources {
		faculties[s.Faculty] = true
		if !strings.HasPrefix(s.IndexURL, "https://") {
			t.Errorf("źródło %s bez https: %s", s.Faculty, s.IndexURL)
		}
	}
	perFaculty := map[string]int{}
	for _, p := range plans {
		if !faculties[p.Faculty] {
			t.Errorf("plan %s ma wydział %q, którego nie ma w sources.json", p.ID, p.Faculty)
		}
		if !strings.HasPrefix(p.URL, "https://") && !strings.HasPrefix(p.URL, "http://") {
			t.Errorf("plan %s ma zły adres %q", p.ID, p.URL)
		}
		perFaculty[p.Faculty]++
	}
	for f := range faculties {
		if perFaculty[f] == 0 {
			t.Errorf("wydział %s nie ma żadnej grupy w plans.json", f)
		}
	}
}
