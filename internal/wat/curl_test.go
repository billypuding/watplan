package wat

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func curlScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "curl_chrome")
	script := "#!/bin/sh\nprintf '%s'\n"
	if err := os.WriteFile(path, []byte(strings.Replace(script, "%s", body, 1)), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCurlImpersonateManualFetch(t *testing.T) {
	script := curlScript(t, "<html>plan</html>")

	t.Setenv("PLANWAT_CURL_IMPERSONATE", script)
	t.Setenv("PLANWAT_PROXY", "http://127.0.0.1:1")
	t.Setenv("PLANWAT_PROXY_BACKUP", "")
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")

	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}

	body, _, err := f.FetchPageViaProxy("https://wml.wat.edu.pl/plan.htm", Validators{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "plan") {
		t.Fatalf("body = %q", body)
	}
}

func TestCurlImpersonateDetectsBlock(t *testing.T) {
	script := curlScript(t, "<html>Access denied Error 17. Incapsula incident ID: 1</html>")

	t.Setenv("PLANWAT_CURL_IMPERSONATE", script)
	t.Setenv("PLANWAT_PROXY", "http://127.0.0.1:1")
	t.Setenv("PLANWAT_PROXY_BACKUP", "")
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")

	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = f.FetchPageViaProxy("https://wml.wat.edu.pl/plan.htm", Validators{})
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("err = %v, chcę ErrBlocked", err)
	}
}
