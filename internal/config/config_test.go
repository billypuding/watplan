package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigAllowsEmptyList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plans.json")
	if err := os.WriteFile(path, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	plans, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("liczba planów = %d, chcę 0", len(plans))
	}
}

func TestSavePlansSkipsUnchangedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plans.json")
	plans := []PlanConfig{
		{ID: "ABC25X", Name: "ABC25X", URL: "https://example.com/a.htm", StartYear: 2025},
	}

	if err := SavePlans(path, plans); err != nil {
		t.Fatalf("SavePlans: %v", err)
	}

	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	if err := SavePlans(path, plans); err != nil {
		t.Fatalf("SavePlans (bez zmian): %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Errorf("plik nadpisany mimo braku zmian (mtime %s)", info.ModTime())
	}
}

func TestSavePlansWritesWhenContentChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plans.json")
	plans := []PlanConfig{
		{ID: "ABC25X", Name: "ABC25X", URL: "https://example.com/a.htm", StartYear: 2025},
	}

	if err := SavePlans(path, plans); err != nil {
		t.Fatalf("SavePlans: %v", err)
	}

	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	plans = append(plans, PlanConfig{ID: "DEF25Y", Name: "DEF25Y", URL: "https://example.com/b.htm", StartYear: 2025})
	if err := SavePlans(path, plans); err != nil {
		t.Fatalf("SavePlans (zmiana): %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.ModTime().Equal(old) {
		t.Error("brak zapisu mimo zmiany zawartości")
	}

	if _, err := LoadConfig(path); err != nil {
		t.Fatalf("LoadConfig po zapisie: %v", err)
	}
}

func TestReplaceFacultyKeepsOtherFaculties(t *testing.T) {
	plans := []PlanConfig{
		{ID: "WEL26EY4S0", Faculty: "WEL"},
		{ID: "WIG25OLD1", Faculty: "WIG"},
		{ID: "IOE26HX1S0", Faculty: "IOE"},
	}
	found := []PlanConfig{{ID: "WIG26BX2S1", Faculty: "WIG"}, {ID: "WIG26BX1S1", Faculty: "WIG"}}

	got := ReplaceFaculty(plans, "WIG", found)
	want := []string{"IOE26HX1S0", "WEL26EY4S0", "WIG26BX1S1", "WIG26BX2S1"}
	if len(got) != len(want) {
		t.Fatalf("planów %d, chcę %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("pozycja %d: %s, chcę %s", i, got[i].ID, id)
		}
	}
}
