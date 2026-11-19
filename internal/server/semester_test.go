package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/store"
)

func weeklyEvents(from string, weeks int) []model.Event {
	start, _ := time.Parse("2006-01-02", from)
	var out []model.Event
	for i := 0; i < weeks; i++ {
		out = append(out, model.Event{Date: start.AddDate(0, 0, 7*i).Format("2006-01-02"), Start: "08:00", End: "09:35", Subject: "X"})
	}
	return out
}

func TestSemestersSkipExamWeeksAndFindPartialSummer(t *testing.T) {
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var plans []config.PlanConfig
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("WEL26EX%dS0", i)
		events := weeklyEvents("2026-10-01", 17)
		if i < 3 {
			events = append(events, model.Event{Date: "2027-02-03", Start: "10:00", End: "12:00", Subject: "Egzamin"})
			events = append(events, weeklyEvents("2027-02-23", 15)...)
		}
		if err := st.Save(id, model.Plan{Events: events}); err != nil {
			t.Fatal(err)
		}
		plans = append(plans, config.PlanConfig{ID: id})
	}
	list := buildSemesters(config.NewRegistry(plans), st)
	if len(list) != 2 {
		t.Fatalf("semestry %+v", list)
	}
	winter, summer := list[0], list[1]
	if winter.Name != "Semestr zimowy" || winter.Years != "2026/27" || len(winter.Weeks) != 17 || winter.Start != "2026-10-01" || winter.End != "2027-01-21" {
		t.Fatalf("zima %+v", winter)
	}
	if summer.Name != "Semestr letni" || summer.Years != "2026/27" || len(summer.Weeks) != 15 || summer.Start != "2027-02-23" {
		t.Fatalf("lato %+v", summer)
	}
	if got, _ := pickSemester(list, "2027-02-10"); got.Name != "Semestr letni" {
		t.Fatalf("w przerwie między semestrami chcę nadchodzący letni, mam %s", got.Name)
	}
	if got, _ := pickSemester(list, "2026-11-19"); got.Name != "Semestr zimowy" {
		t.Fatalf("w listopadzie chcę zimowy, mam %s", got.Name)
	}
}
