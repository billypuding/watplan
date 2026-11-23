package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/store"
)

func backupServer(t *testing.T, run func() (string, int, int, error)) http.Handler {
	t.Helper()
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewServer(config.NewRegistry(nil), st, nil, Options{Offline: true, StatusToken: "sekret", Backup: run}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func postBackup(h http.Handler, token, method string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/backup", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestBackupEndpoint(t *testing.T) {
	calls := 0
	h := backupServer(t, func() (string, int, int, error) {
		calls++
		return "backups/2026-11-23.json.gz", 734, 2482705, nil
	})
	if rec := postBackup(h, "", http.MethodPost); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bez tokenu: %d", rec.Code)
	}
	if rec := postBackup(h, "zly", http.MethodPost); rec.Code != http.StatusUnauthorized {
		t.Fatalf("zły token: %d", rec.Code)
	}
	if rec := postBackup(h, "sekret", http.MethodGet); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", rec.Code)
	}
	if calls != 0 {
		t.Fatalf("kopia uruchomiona bez uprawnień (%d razy)", calls)
	}
	rec := postBackup(h, "sekret", http.MethodPost)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"keys":734`) || calls != 1 {
		t.Fatalf("z tokenem: %d %s", rec.Code, rec.Body.String())
	}
}

func TestBackupEndpointErrorsAndConcurrency(t *testing.T) {
	if rec := postBackup(backupServer(t, nil), "sekret", http.MethodPost); rec.Code != http.StatusNotFound {
		t.Fatalf("bez konfiguracji: %d", rec.Code)
	}
	failing := backupServer(t, func() (string, int, int, error) { return "", 0, 0, errors.New("403 z R2") })
	if rec := postBackup(failing, "sekret", http.MethodPost); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "403 z R2") {
		t.Fatalf("błąd kopii: %d %s", rec.Code, rec.Body.String())
	}
	started, release := make(chan struct{}), make(chan struct{})
	slow := backupServer(t, func() (string, int, int, error) {
		close(started)
		<-release
		return "k", 1, 1, nil
	})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); postBackup(slow, "sekret", http.MethodPost) }()
	<-started
	if rec := postBackup(slow, "sekret", http.MethodPost); rec.Code != http.StatusConflict {
		t.Fatalf("druga kopia w trakcie pierwszej: %d", rec.Code)
	}
	close(release)
	wg.Wait()
}
