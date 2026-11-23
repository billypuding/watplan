package store

import (
	"bytes"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/durable"
	"github.com/billypuding/watplan/internal/model"
)

func TestRestoreFromTursoAfterDeploy(t *testing.T) {
	url := os.Getenv("PLANWAT_TEST_DB_URL")
	if url == "" {
		t.Skip("PLANWAT_TEST_DB_URL nie ustawiony")
	}
	db, err := durable.OpenTurso(url, os.Getenv("PLANWAT_TEST_DB_TOKEN"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	id := fmt.Sprintf("TEST%d", time.Now().UnixNano())
	meta := fmt.Sprintf("test-meta-%d", time.Now().UnixNano())

	before, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	queue := durable.NewQueue(db)
	before.SetMirror(queue)
	plan := model.Plan{
		FetchedAt:  "2026-11-23 12:00:00",
		ModifiedAt: "2026-11-19 23:20:21",
		Events:     []model.Event{{Date: "2026-11-24", Start: "13:30", End: "15:05", Subject: "Matematyka 2", Type: "wykład", Room: "19 36"}},
	}
	if err := before.Save(id, plan); err != nil {
		t.Fatal(err)
	}
	if err := before.SaveMeta(meta, []byte(`{"saved":3}`)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, saved := queue.Status(); saved >= 2 {
			break
		}
		if time.Now().After(deadline) {
			failed, saved := queue.Status()
			t.Fatalf("kolejka nie zapisała danych: %q, %d", failed, saved)
		}
		time.Sleep(50 * time.Millisecond)
	}

	after, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	restored, _, err := after.Restore(db)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restored < 1 {
		t.Fatalf("odtworzono %d planów", restored)
	}
	got, err := after.Load(id)
	if err != nil {
		t.Fatalf("plan po odtworzeniu: %v", err)
	}
	if got.FetchedAt != plan.FetchedAt || len(got.Events) != 1 || got.Events[0].Room != "19 36" {
		t.Fatalf("odtworzony plan %+v", got)
	}
	data, err := after.LoadMeta(meta)
	if err != nil || !bytes.Equal(data, []byte(`{"saved":3}`)) {
		t.Fatalf("meta po odtworzeniu: %q %v", data, err)
	}
}
