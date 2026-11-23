package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/push"
)

func TestMain(m *testing.M) {
	if os.Getenv("WATPLAN_TEST_MAIN") == "1" {
		var args []string
		json.Unmarshal([]byte(os.Getenv("WATPLAN_TEST_ARGS")), &args)
		os.Args = append([]string{os.Args[0]}, args...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

type serverEnv struct {
	dir  string
	addr string
	env  []string
}

func newServerEnv(t *testing.T) *serverEnv {
	t.Helper()
	dir := t.TempDir()
	copyDir(t, "../../e2e/fixtures", dir)
	addr := freeAddr(t)
	env := []string{
		"WATPLAN_TEST_MAIN=1",
		"PLANWAT_ADDR=" + addr,
		"PLANWAT_CONFIG=" + filepath.Join(dir, "plans.json"),
		"PLANWAT_DATA=" + filepath.Join(dir, "data"),
		"PLANWAT_SOURCES=" + filepath.Join(dir, "sources.json"),
	}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PLANWAT_") && !strings.HasPrefix(kv, "WATPLAN_") {
			env = append(env, kv)
		}
	}
	return &serverEnv{dir: dir, addr: addr, env: env}
}

func (e *serverEnv) command(ctx context.Context, args []string, extra ...string) (*exec.Cmd, *syncBuffer) {
	encoded, _ := json.Marshal(args)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Env = append(append(append([]string{}, e.env...), "WATPLAN_TEST_ARGS="+string(encoded)), extra...)
	out := &syncBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd, out
}

func (e *serverEnv) run(t *testing.T, args []string, extra ...string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd, out := e.command(ctx, args, extra...)
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, out.String()
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestServerStartsOfflineServesAndShutsDownCleanly(t *testing.T) {
	e := newServerEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd, out := e.command(ctx, []string{"-offline"})
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	base := "http://" + e.addr
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := http.Get(base + "/api/status")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			cmd.Process.Kill()
			t.Fatalf("serwer nie wstał w 30 s:\n%s", out.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	for path, want := range map[string]string{
		"/":                       "WATplan",
		"/app":                    "WATplan",
		"/plan/WMT26AX3S1":        "WMT26AX3S1",
		"/api/plan/WMT26AX3S1":    `"events"`,
		"/sitemap.xml":            "/plan/WMT26AX3S1",
		"/plany":                  "WMT26AX3S1",
		"/plan/WMT26AX3S1/og.png": "PNG",
	} {
		code, body := get(t, base+path)
		if code != http.StatusOK || !strings.Contains(body, want) {
			t.Errorf("%s: %d, brak %q", path, code, want)
		}
	}
	if code, _ := get(t, base+"/healthz"); code != http.StatusOK {
		t.Errorf("/healthz: %d", code)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("zamknięcie po SIGTERM: %v\n%s", err, out.String())
		}
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatalf("serwer nie zamknął się 10 s po SIGTERM:\n%s", out.String())
	}
	log := out.String()
	for _, want := range []string{"tryb offline", "serwer działa pod adresem http://" + e.addr} {
		if !strings.Contains(log, want) {
			t.Errorf("w logu brak %q:\n%s", want, log)
		}
	}
}

func TestServerRefusesToStartWithBrokenConfig(t *testing.T) {
	e := newServerEnv(t)
	broken := filepath.Join(e.dir, "broken.json")
	os.WriteFile(broken, []byte(`[{"id":"A1","url":"https://x/a.htm"}]`), 0o644)
	code, log := e.run(t, []string{"-offline"}, "PLANWAT_CONFIG="+broken)
	if code == 0 || !strings.Contains(log, "start_year") {
		t.Fatalf("kod %d, log:\n%s", code, log)
	}
}

func TestServerRefusesToStartWithBadEnv(t *testing.T) {
	e := newServerEnv(t)
	for _, kv := range []string{
		"PLANWAT_LIMIT_PUSH_PER_HOUR=dużo",
		"PLANWAT_LIMIT_CHECKS_PER_HOUR=1.5",
		"PLANWAT_HEALTH_FETCH_MAX_AGE=wczoraj",
		"PLANWAT_HEALTH_FETCH_MAX_AGE=-1h",
	} {
		key := strings.SplitN(kv, "=", 2)[0]
		code, log := e.run(t, []string{"-offline"}, kv)
		if code == 0 || !strings.Contains(log, "zły "+key) {
			t.Errorf("%s: kod %d, log:\n%s", kv, code, log)
		}
	}
}

func startServer(t *testing.T, e *serverEnv, args []string, extra ...string) (*exec.Cmd, *syncBuffer, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	cmd, out := e.command(ctx, args, extra...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	base := "http://" + e.addr
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := http.Get(base + "/api/status")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return cmd, out, base
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("serwer nie wstał w 30 s:\n%s", out.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestBrokenPushKeysDisablePushButServerRuns(t *testing.T) {
	for name, keys := range map[string][]string{
		"śmieci":      {"PLANWAT_VAPID_PUBLIC=x", "PLANWAT_VAPID_PRIVATE=y"},
		"zamienione":  {"PLANWAT_VAPID_PUBLIC=" + testPrivate, "PLANWAT_VAPID_PRIVATE=" + testPublic},
		"niepasujące": {"PLANWAT_VAPID_PUBLIC=" + testPublic, "PLANWAT_VAPID_PRIVATE=" + otherPrivate},
	} {
		t.Run(name, func(t *testing.T) {
			e := newServerEnv(t)
			_, out, base := startServer(t, e, []string{"-offline"}, keys...)
			if !strings.Contains(out.String(), "powiadomienia push WYŁĄCZONE") {
				t.Fatalf("brak ostrzeżenia w logu:\n%s", out.String())
			}
			if code, _ := get(t, base+"/api/plan/WMT26AX3S1"); code != http.StatusOK {
				t.Fatalf("plan: %d", code)
			}
			if code, _ := get(t, base+"/api/push/key"); code != http.StatusNotFound {
				t.Fatalf("/api/push/key: %d, chcę 404 (push wyłączony)", code)
			}
		})
	}
	t.Run("poprawne", func(t *testing.T) {
		e := newServerEnv(t)
		_, out, base := startServer(t, e, []string{"-offline"}, "PLANWAT_VAPID_PUBLIC="+testPublic, "PLANWAT_VAPID_PRIVATE="+testPrivate, "PLANWAT_PUSH_FILE="+filepath.Join(e.dir, "push.json"))
		if !strings.Contains(out.String(), "powiadomienia push włączone") {
			t.Fatalf("log:\n%s", out.String())
		}
		if code, body := get(t, base+"/api/push/key"); code != http.StatusOK || !strings.Contains(body, testPublic) {
			t.Fatalf("/api/push/key: %d %s", code, body)
		}
	})
}

var testPublic, testPrivate, otherPrivate = func() (string, string, string) {
	public, private, err := push.GenerateKeys()
	if err != nil {
		panic(err)
	}
	_, other, err := push.GenerateKeys()
	if err != nil {
		panic(err)
	}
	return public, private, other
}()

func TestOnlineStartNeedsSources(t *testing.T) {
	e := newServerEnv(t)
	code, log := e.run(t, nil, "PLANWAT_SOURCES="+filepath.Join(e.dir, "brak.json"))
	if code == 0 || !strings.Contains(log, "brak.json") {
		t.Fatalf("start online bez sources.json: kod %d, log:\n%s", code, log)
	}
}

func TestVapidKeysFlag(t *testing.T) {
	e := newServerEnv(t)
	code, out := e.run(t, []string{"-vapid-keys"})
	if code != 0 || !strings.Contains(out, "PLANWAT_VAPID_PUBLIC=") || !strings.Contains(out, "PLANWAT_VAPID_PRIVATE=") {
		t.Fatalf("kod %d, wyjście:\n%s", code, out)
	}
}

func TestEnvHelpers(t *testing.T) {
	t.Setenv("WATPLAN_T_STR", "  ")
	if envOr("WATPLAN_T_STR", "domyślne") != "  " || envOr("WATPLAN_T_BRAK", "domyślne") != "domyślne" {
		t.Fatal("envOr")
	}
	for value, want := range map[string]bool{"1": true, "true": true, "TAK": true, " on ": true, "0": false, "nie": false, "": false} {
		t.Setenv("WATPLAN_T_BOOL", value)
		if envBool("WATPLAN_T_BOOL") != want {
			t.Errorf("envBool(%q) != %v", value, want)
		}
	}
	t.Setenv("WATPLAN_T_INT", " -1 ")
	if envInt("WATPLAN_T_INT") != -1 || envInt("WATPLAN_T_BRAK") != 0 {
		t.Fatal("envInt")
	}
	t.Setenv("WATPLAN_T_DUR", "90m")
	if envDuration("WATPLAN_T_DUR") != 90*time.Minute || envDuration("WATPLAN_T_BRAK") != 0 {
		t.Fatal("envDuration")
	}
}

func TestImportFileErrors(t *testing.T) {
	reg := config.NewRegistry([]config.PlanConfig{{ID: "A1"}})
	for spec, want := range map[string]string{
		"bez-rownasie":        "zły format",
		"=plik.html":          "zły format",
		"A1=":                 "zły format",
		"NIEMA=plik.html":     "nie występuje",
		"A1=/brak/pliku.html": "otwarcie",
	} {
		err := importFile(nil, reg, spec)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, chcę %q", spec, err, want)
		}
	}
}

func TestDiscoverUnknownFaculty(t *testing.T) {
	e := newServerEnv(t)
	sources := filepath.Join(e.dir, "sources.json")
	os.WriteFile(sources, []byte(`[{"faculty":"WML","index_url":"https://example.invalid/index.xml"}]`), 0o644)
	err := discoverFaculty("XYZ", sources, filepath.Join(e.dir, "plans.json"), nil)
	if err == nil || !strings.Contains(err.Error(), "XYZ") {
		t.Fatalf("%v", err)
	}
	if err := discoverFaculty("WML", filepath.Join(e.dir, "brak.json"), "", nil); err == nil {
		t.Fatal("brak sources.json powinien dać błąd")
	}
}

func TestBrokenCDNConfigDisablesCDNButServerRuns(t *testing.T) {
	for name, env := range map[string][]string{
		"brak kluczy":   {"PLANWAT_R2_ENDPOINT=https://acc.r2.cloudflarestorage.com", "PLANWAT_CDN_URL=https://cdn.watplan.me"},
		"cdn bez https": {"PLANWAT_R2_ENDPOINT=https://acc.r2.cloudflarestorage.com", "PLANWAT_R2_BUCKET=b", "PLANWAT_R2_ACCESS_KEY_ID=a", "PLANWAT_R2_SECRET_ACCESS_KEY=s", "PLANWAT_CDN_URL=http://cdn.watplan.me"},
		"zły endpoint":  {"PLANWAT_R2_ENDPOINT=ftp://x", "PLANWAT_R2_BUCKET=b", "PLANWAT_R2_ACCESS_KEY_ID=a", "PLANWAT_R2_SECRET_ACCESS_KEY=s", "PLANWAT_CDN_URL=https://cdn.watplan.me"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newServerEnv(t)
			_, out, base := startServer(t, e, []string{"-offline"}, env...)
			if !strings.Contains(out.String(), "CDN WYŁĄCZONY") {
				t.Fatalf("brak ostrzeżenia:\n%s", out.String())
			}
			_, page := get(t, base+"/app")
			if strings.Contains(page, `<meta name="watplan-cdn"`) {
				t.Fatal("strona zawiera adres CDN mimo wyłączonego CDN")
			}
		})
	}
	t.Run("sam adres CDN bez R2", func(t *testing.T) {
		e := newServerEnv(t)
		_, out, base := startServer(t, e, []string{"-offline"}, "PLANWAT_CDN_URL=https://cdn.watplan.me")
		if !strings.Contains(out.String(), "CDN wyłączony") {
			t.Fatalf("log:\n%s", out.String())
		}
		if _, page := get(t, base+"/app"); strings.Contains(page, `<meta name="watplan-cdn"`) {
			t.Fatal("adres CDN na stronie bez R2")
		}
	})
}
