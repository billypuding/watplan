package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/store"
)

func BenchmarkPlanGzip(b *testing.B) {
	dir := b.TempDir()
	data, err := os.ReadFile("../../e2e/fixtures/data/WMT26AX3S1.json")
	if err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "WMT26AX3S1.json"), data, 0o644); err != nil {
		b.Fatal(err)
	}
	st, err := store.NewStore(dir)
	if err != nil {
		b.Fatal(err)
	}
	reg := config.NewRegistry([]config.PlanConfig{{ID: "WMT26AX3S1", URL: "http://example.invalid/"}})
	handler, err := NewServer(reg, st, nil, Options{Offline: true}).Handler()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/plan/WMT26AX3S1", nil)
		req.Header.Set("Accept-Encoding", "gzip, br")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatal(rec.Code)
		}
	}
}

func BenchmarkCheckedAt(b *testing.B) {
	data, err := os.ReadFile("../../e2e/fixtures/data/WMT26AX3S1.json")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !readPlanTimes(data).HasChecked {
			b.Fatal("brak czasu")
		}
	}
}
