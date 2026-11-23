package cdn

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSignatureMatchesAWSExample(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	at := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	signRequest(req, nil, "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1", at)
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("podpis:\n%s\nchcę:\n%s", got, want)
	}
}

func TestNewValidates(t *testing.T) {
	for _, c := range [][4]string{
		{"", "b", "a", "s"},
		{"https://x.r2.cloudflarestorage.com", "", "a", "s"},
		{"https://x.r2.cloudflarestorage.com", "b", "", "s"},
		{"ftp://x", "b", "a", "s"},
		{"nie adres", "b", "a", "s"},
	} {
		if _, err := New(c[0], c[1], c[2], c[3], ""); err == nil {
			t.Errorf("%v: brak błędu", c)
		}
	}
	b, err := New("https://acc.r2.cloudflarestorage.com/", "watplan-plans", "a", "s", "")
	if err != nil {
		t.Fatal(err)
	}
	if b.region != "auto" {
		t.Fatalf("region %q", b.region)
	}
	if got := b.objectURL(PlanKey("WMT26AX3S1")).String(); got != "https://acc.r2.cloudflarestorage.com/watplan-plans/plans/WMT26AX3S1.json" {
		t.Fatalf("adres %q", got)
	}
}

func TestPutSignsBodyHash(t *testing.T) {
	b, _ := New("https://acc.r2.cloudflarestorage.com", "bucket", "AK", "SK", "")
	b.now = func() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) }
	var seen *http.Request
	b.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		seen = r
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}}, nil
	})}
	if err := b.Put(PlanKey("A1"), []byte(`{"events":[]}`)); err != nil {
		t.Fatal(err)
	}
	if seen.Method != http.MethodPut || seen.URL.Path != "/bucket/plans/A1.json" {
		t.Fatalf("%s %s", seen.Method, seen.URL.Path)
	}
	if seen.Header.Get("X-Amz-Content-Sha256") != sha256Hex([]byte(`{"events":[]}`)) {
		t.Fatal("zły skrót treści")
	}
	auth := seen.Header.Get("Authorization")
	if !strings.Contains(auth, "Credential=AK/20261123/auto/s3/aws4_request") || !strings.Contains(auth, "SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date") {
		t.Fatalf("Authorization %q", auth)
	}
	if seen.Header.Get("Cache-Control") != planCache {
		t.Fatalf("Cache-Control %q", seen.Header.Get("Cache-Control"))
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
