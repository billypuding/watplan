package server

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/store"
)

type budgetMetric string

const (
	metricGzip    budgetMetric = "gzip"
	metricBrotli  budgetMetric = "brotli"
	metricRaw     budgetMetric = "surowo"
	metricCompact budgetMetric = "gzip, woff2 surowo"
)

type budget struct {
	name   string
	metric budgetMetric
	limit  int
}

const (
	budgetFonts    = "czcionki (każdy plik)"
	budgetCritical = "ścieżka krytyczna /app"
	budgetOther    = "pozostałe pliki (każdy)"
	budgetPlan     = "/api/plan WMT26AX3S1"
)

var budgets = []budget{
	{"app.js", metricGzip, 22500},
	{"style.css", metricGzip, 9500},
	{"app.html", metricGzip, 5400},
	{"sw.js", metricGzip, 1600},
	{budgetFonts, metricRaw, 32500},
	{budgetCritical, metricCompact, 78000},
	{budgetOther, metricGzip, 16500},
	{budgetPlan, metricBrotli, 2100},
	{"vendor/maplibre-gl.mjs", metricBrotli, 250000},
	{"vendor/maplibre-gl-worker.mjs", metricBrotli, 135000},
	{"vendor/maplibre-gl.css", metricGzip, 11000},
}

type assetSize struct {
	raw, gzip, brotli int
}

func (s assetSize) compact(name string) int {
	if strings.HasSuffix(name, ".woff2") {
		return s.raw
	}
	return s.gzip
}

func (s assetSize) value(m budgetMetric, name string) int {
	switch m {
	case metricRaw:
		return s.raw
	case metricBrotli:
		return s.brotli
	case metricCompact:
		return s.compact(name)
	}
	return s.gzip
}

func measure(data []byte) assetSize {
	var g, b bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&g, 6)
	zw.Write(data)
	zw.Close()
	bw := brotli.NewWriterLevel(&b, brotli.BestCompression)
	bw.Write(data)
	bw.Close()
	return assetSize{raw: len(data), gzip: g.Len(), brotli: b.Len()}
}

var criticalRef = regexp.MustCompile(`<link rel="stylesheet" href="/([^"?]+)|<link rel="preload" href="/([^"?]+)[^>]*as="font"|<script (?:defer )?src="/([^"?]+)`)

func criticalFiles(html []byte) []string {
	var files []string
	for _, m := range criticalRef.FindAllSubmatch(html, -1) {
		for _, g := range m[1:] {
			if len(g) > 0 {
				files = append(files, string(g))
			}
		}
	}
	return files
}

func servedPlanBrotli(t *testing.T) int {
	t.Helper()
	dir := t.TempDir()
	raw, err := os.ReadFile("../../e2e/fixtures/data/WMT26AX3S1.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "WMT26AX3S1.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := config.NewRegistry([]config.PlanConfig{{ID: "WMT26AX3S1", URL: "http://example.invalid/"}})
	handler, err := NewServer(reg, st, nil, Options{Offline: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	serve := func() int {
		req := httptest.NewRequest(http.MethodGet, "/api/plan/WMT26AX3S1", nil)
		req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != "br" {
			t.Fatalf("/api/plan: status = %d, Content-Encoding = %q", rec.Code, rec.Header().Get("Content-Encoding"))
		}
		return rec.Body.Len()
	}
	size := serve()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		next := serve()
		if next < size {
			return next
		}
		size = next
	}
	return size
}

func TestSizeBudget(t *testing.T) {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := newAssetFS(sub)
	if err != nil {
		t.Fatal(err)
	}
	sizes := map[string]assetSize{}
	var names []string
	err = fs.WalkDir(sub, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(assets, name)
		if err != nil {
			return err
		}
		sizes[name] = measure(data)
		names = append(names, name)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)

	limits := map[string]budget{}
	for _, b := range budgets {
		limits[b.name] = b
	}
	check := func(label string, b budget, value int) {
		if value > b.limit {
			t.Errorf("%s: %d B (%s), limit %d B, przekroczony o %d B (%.1f%%)",
				label, value, b.metric, b.limit, value-b.limit, float64(value-b.limit)*100/float64(b.limit))
		}
	}

	t.Logf("%-34s %8s %8s %8s %8s", "plik", "surowo", "gzip", "brotli", "limit")
	for _, name := range names {
		s := sizes[name]
		b, own := limits[name]
		switch {
		case own:
		case path.Ext(name) == ".woff2":
			b = limits[budgetFonts]
		default:
			b = limits[budgetOther]
		}
		t.Logf("%-34s %8d %8d %8d %8s", name, s.raw, s.gzip, s.brotli, fmt.Sprintf("%d %s", b.limit, b.metric))
		check(name, b, s.value(b.metric, name))
	}

	html, err := fs.ReadFile(assets, "app.html")
	if err != nil {
		t.Fatal(err)
	}
	critical := append([]string{"app.html"}, criticalFiles(html)...)
	total := 0
	for _, name := range critical {
		s, ok := sizes[name]
		if !ok {
			t.Fatalf("ścieżka krytyczna: brak pliku %s", name)
		}
		total += s.compact(name)
	}
	for _, name := range critical {
		if strings.HasPrefix(name, "vendor/") || name == "map.js" || name == "campus.json" {
			t.Errorf("mapa trafiła na ścieżkę krytyczną: %s", name)
		}
	}
	if len(critical) < 5 {
		t.Fatalf("ścieżka krytyczna: za mało plików (%v)", critical)
	}
	t.Logf("%-34s %8s %8d %8s %8d (%s)", budgetCritical, "", total, "", limits[budgetCritical].limit, strings.Join(critical, " + "))
	check(budgetCritical+" ("+strings.Join(critical, " + ")+")", limits[budgetCritical], total)

	plan := servedPlanBrotli(t)
	t.Logf("%-34s %8s %8s %8d %8d", budgetPlan, "", "", plan, limits[budgetPlan].limit)
	check(budgetPlan, limits[budgetPlan], plan)
}
