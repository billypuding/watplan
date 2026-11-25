package durable

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func testTurso(t *testing.T) (*Turso, string) {
	t.Helper()
	url := os.Getenv("PLANWAT_TEST_DB_URL")
	if url == "" {
		t.Skip("PLANWAT_TEST_DB_URL nie ustawiony (np. http://127.0.0.1:8080 z kontenera libsql-server)")
	}
	db, err := OpenTurso(url, os.Getenv("PLANWAT_TEST_DB_TOKEN"))
	if err != nil {
		t.Fatalf("OpenTurso: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, fmt.Sprintf("test-%d", time.Now().UnixNano())
}

func TestTursoPutAllRoundTrip(t *testing.T) {
	db, run := testTurso(t)
	binary := []byte{0, 1, 2, 0xff, 0}
	polish := []byte(`{"subject":"Wprowadzenie do metrologii – ćwiczenia, sala 133/65"}`)
	large := bytes.Repeat([]byte("x"), 512<<10)
	for key, value := range map[string][]byte{
		run + "/plan/A":    []byte("pierwsza"),
		run + "/plan/BIN":  binary,
		run + "/plan/PL":   polish,
		run + "/plan/BIG":  large,
		run + "/planx/OUT": []byte("inny prefiks"),
	} {
		if err := db.Put(key, value); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
	}
	if err := db.Put(run+"/plan/A", []byte("druga")); err != nil {
		t.Fatalf("nadpisanie: %v", err)
	}

	all, err := db.All(run + "/plan/")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		keys := []string{}
		for k := range all {
			keys = append(keys, k)
		}
		t.Fatalf("All zwrócił %d kluczy %v, chcę 4 (bez %s/planx/)", len(all), keys, run)
	}
	checks := map[string][]byte{run + "/plan/A": []byte("druga"), run + "/plan/BIN": binary, run + "/plan/PL": polish, run + "/plan/BIG": large}
	for key, want := range checks {
		if !bytes.Equal(all[key], want) {
			t.Errorf("%s: %d B, chcę %d B", key, len(all[key]), len(want))
		}
	}
	if none, err := db.All(run + "/brak/"); err != nil || len(none) != 0 {
		t.Fatalf("pusty prefiks: %v %v", none, err)
	}
}

func TestTursoPrefixIsLiteral(t *testing.T) {
	db, run := testTurso(t)
	for _, key := range []string{run + "/a%b/1", run + "/a_b/1", run + "/axb/1"} {
		if err := db.Put(key, []byte("v")); err != nil {
			t.Fatal(err)
		}
	}
	for _, prefix := range []string{run + "/a%b/", run + "/a_b/"} {
		all, err := db.All(prefix)
		if err != nil || len(all) != 1 {
			t.Errorf("All(%q) = %d kluczy, %v; %% i _ nie mogą działać jak wzorce", prefix, len(all), err)
		}
	}
}

func TestTursoReopenAndQueue(t *testing.T) {
	db, run := testTurso(t)
	again, err := OpenTurso(os.Getenv("PLANWAT_TEST_DB_URL"), os.Getenv("PLANWAT_TEST_DB_TOKEN"))
	if err != nil {
		t.Fatalf("drugie otwarcie (tabela już istnieje): %v", err)
	}
	again.Close()

	q := newQueue(db, 5*time.Millisecond, 50*time.Millisecond)
	for i := 0; i < 20; i++ {
		q.Put(Keyed(run+"/q", fmt.Sprintf("P%02d", i)), []byte(strings.Repeat("v", i+1)))
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		all, err := q.All(run + "/q/")
		if _, saved := q.Status(); err == nil && len(all) == 20 && saved == 20 {
			if string(all[run+"/q/P19"]) != strings.Repeat("v", 20) {
				t.Fatalf("P19 = %q", all[run+"/q/P19"])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("po 20 s w bazie jest %d z 20 kluczy (%v)", len(all), err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if failed, saved := q.Status(); failed != "" || saved != 20 {
		t.Fatalf("status %q, zapisanych %d", failed, saved)
	}
}
