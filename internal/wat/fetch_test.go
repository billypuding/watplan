package wat

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFetchHTMLDetectsWAFBlock(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		xInfo      bool
		shouldStop bool
	}{
		{
			name:       "403 z nagłówkiem Incapsuli",
			status:     http.StatusForbidden,
			body:       "Request unsuccessful. Incapsula incident ID: 1",
			xInfo:      true,
			shouldStop: true,
		},
		{
			name:       "403 Error 17 bez nagłówka",
			status:     http.StatusForbidden,
			body:       "<title>Access denied</title>Error 17",
			shouldStop: true,
		},
		{
			name:       "strona blokady ze statusem 200",
			status:     http.StatusOK,
			body:       "Access denied Error 17. Incapsula incident ID: 1",
			shouldStop: true,
		},
		{
			name:       "zwykłe 403 bez strony blokady",
			status:     http.StatusForbidden,
			body:       "Forbidden",
			shouldStop: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int64
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				if tc.xInfo {
					w.Header().Set("X-Iinfo", "62-966453-0 0NNN")
				}
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			t.Cleanup(srv.Close)

			t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
			fetcher, err := NewFetcher()
			if err != nil {
				t.Fatalf("NewFetcher: %v", err)
			}

			_, err = fetcher.FetchHTML(srv.URL)
			if tc.shouldStop {
				if !errors.Is(err, ErrBlocked) {
					t.Fatalf("FetchHTML = %v, chcę ErrBlocked", err)
				}
				if got := hits.Load(); got != 1 {
					t.Fatalf("zapytań = %d, chcę 1 (bez ponowień przy blokadzie)", got)
				}
				return
			}

			if errors.Is(err, ErrBlocked) {
				t.Fatalf("FetchHTML = %v, nie chcę ErrBlocked", err)
			}
			var se *statusError
			if !errors.As(err, &se) || se.status != tc.status {
				t.Fatalf("FetchHTML = %v, chcę statusError(%d)", err, tc.status)
			}
		})
	}
}

func TestProxyRejectionIsNotRetried(t *testing.T) {
	var hits atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(proxy.Close)

	t.Setenv("PLANWAT_PROXY", proxy.URL)
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	f, err := NewFetcher()
	if err != nil {
		t.Fatalf("NewFetcher: %v", err)
	}

	_, err = f.FetchHTML("https://wml.wat.edu.pl/plan.htm")
	if !errors.Is(err, ErrProxy) {
		t.Fatalf("FetchHTML = %v, chcę ErrProxy", err)
	}
	if !strings.Contains(err.Error(), "402") {
		t.Errorf("błąd %q powinien zawierać status 402", err)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("zapytań do proxy = %d, chcę 1 (bez ponawiania)", got)
	}
	if f.ProxyHost() == "" {
		t.Error("ProxyHost pusty mimo ustawionego PLANWAT_PROXY")
	}
}

func TestProxyRejectionPausesAllFetching(t *testing.T) {
	var hits atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(proxy.Close)

	t.Setenv("PLANWAT_PROXY", proxy.URL)
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.FetchHTML("https://wml.wat.edu.pl/plan.htm")
	_, err = f.FetchHTML("https://wel.wat.edu.pl/inny.htm")
	if !errors.Is(err, ErrProxy) || !strings.Contains(err.Error(), "402") {
		t.Fatalf("drugie pobranie: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("zapytań do proxy %d, chcę 1 (pauza po 402)", hits.Load())
	}
	if !f.Blocked("https://wcy.wat.edu.pl/x") {
		t.Fatal("Blocked powinno zwracać true w czasie pauzy proxy")
	}
	if status, _ := f.ProxyError(); !strings.Contains(status, "402") {
		t.Fatalf("ProxyError = %q", status)
	}
}
