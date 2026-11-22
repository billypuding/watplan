package wat

import (
	"net/url"
	"testing"
)

func TestProxyLogin(t *testing.T) {
	cases := map[string]string{
		"resi_abc":                "customer-resi_abc-cc-PL",
		"customer-resi_abc-cc-PL": "customer-resi_abc-cc-PL",
		"resi_abc-cc-DE":          "customer-resi_abc-cc-DE",
		"customer-resi_abc-cc-PL-sessid-old-sesstime-5": "customer-resi_abc-cc-PL",
	}
	for input, want := range cases {
		if got := proxyLogin(input); got != want {
			t.Errorf("proxyLogin(%q) = %q, chcę %q", input, got, want)
		}
	}
}

func TestNewTLSProxyClient(t *testing.T) {
	t.Setenv("PLANWAT_PROXY_HOST", "pr.oxylabs.io:7777")
	t.Setenv("PLANWAT_PROXY_USER", "resi_abc")
	t.Setenv("PLANWAT_PROXY_PASS", "secret")
	c, err := newTLSProxyClient()
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.client == nil || c.host != "pr.oxylabs.io:7777" {
		t.Fatalf("client = %+v", c)
	}
}

func TestNewTLSProxyClientRequiresPassword(t *testing.T) {
	t.Setenv("PLANWAT_PROXY_HOST", "pr.oxylabs.io:7777")
	t.Setenv("PLANWAT_PROXY_USER", "resi_abc")
	t.Setenv("PLANWAT_PROXY_PASS", "")
	if _, err := newTLSProxyClient(); err == nil {
		t.Fatal("chcę błąd bez hasła")
	}
}

func TestTLSProxyURL(t *testing.T) {
	c := &tlsProxyClient{
		host:    "pr.oxylabs.io:7777",
		user:    "resi_abc",
		pass:    "p@ss",
		session: "abc123",
	}
	u, err := url.Parse(c.proxyURL())
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "http" || u.Host != "pr.oxylabs.io:7777" {
		t.Fatalf("proxy = %q", u)
	}
	if u.User.Username() != "customer-resi_abc-cc-PL-sessid-abc123-sesstime-10" {
		t.Fatalf("username = %q", u.User.Username())
	}
	if pass, _ := u.User.Password(); pass != "p@ss" {
		t.Fatalf("password = %q", pass)
	}
}
