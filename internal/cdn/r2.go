package cdn

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	defaultRegion = "auto"
	planCache     = "public, max-age=60"
)

type Bucket struct {
	endpoint *url.URL
	bucket   string
	access   string
	secret   string
	region   string
	client   *http.Client
	now      func() time.Time
}

func New(endpoint, bucket, access, secret, region string) (*Bucket, error) {
	if endpoint == "" || bucket == "" || access == "" || secret == "" {
		return nil, errors.New("R2: potrzebne endpoint, bucket i oba klucze")
	}
	u, err := url.Parse(strings.TrimRight(endpoint, "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("R2: zły endpoint %q", endpoint)
	}
	if region == "" {
		region = defaultRegion
	}
	return &Bucket{endpoint: u, bucket: bucket, access: access, secret: secret, region: region, client: &http.Client{Timeout: 30 * time.Second}, now: time.Now}, nil
}

func PlanKey(id string) string {
	return "plans/" + id + ".json"
}

func (b *Bucket) objectURL(key string) *url.URL {
	u := *b.endpoint
	u.Path = u.Path + "/" + b.bucket + "/" + key
	u.RawPath = ""
	return &u
}

func (b *Bucket) Put(key string, value []byte) error {
	req, err := http.NewRequest(http.MethodPut, b.objectURL(key).String(), bytes.NewReader(value))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Cache-Control", planCache)
	b.sign(req, value)
	return b.do(req, http.StatusOK)
}

func (b *Bucket) All(string) (map[string][]byte, error) {
	return nil, errors.New("R2: odczyt listy nie jest obsługiwany")
}

func (b *Bucket) ETag(key string) (string, error) {
	req, err := http.NewRequest(http.MethodHead, b.objectURL(key).String(), nil)
	if err != nil {
		return "", err
	}
	b.sign(req, nil)
	resp, err := b.client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("R2 HEAD %s: %s", key, resp.Status)
	}
	return strings.Trim(resp.Header.Get("ETag"), `"`), nil
}

func ContentETag(value []byte) string {
	sum := md5.Sum(value)
	return hex.EncodeToString(sum[:])
}

func (b *Bucket) do(req *http.Request, want int) error {
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("R2 %s %s: %s %s", req.Method, req.URL.Path, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func (b *Bucket) sign(req *http.Request, payload []byte) {
	signRequest(req, payload, b.access, b.secret, b.region, b.now().UTC())
}

func signRequest(req *http.Request, payload []byte, access, secret, region string, t time.Time) {
	amzDate := t.Format("20060102T150405Z")
	day := t.Format("20060102")
	payloadHash := sha256Hex(payload)
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	headers := map[string]string{"host": req.URL.Host}
	for name, values := range req.Header {
		lower := strings.ToLower(name)
		if lower == "host" || lower == "x-amz-date" || lower == "x-amz-content-sha256" || lower == "range" || lower == "content-type" {
			headers[lower] = strings.TrimSpace(strings.Join(values, ","))
		}
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name + ":" + headers[name] + "\n")
	}
	signed := strings.Join(names, ";")

	canonical := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.Query().Encode(),
		canonicalHeaders.String(),
		signed,
		payloadHash,
	}, "\n")
	scope := day + "/" + region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonical))
	key := hmacSHA256([]byte("AWS4"+secret), day)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, toSign))
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", access, scope, signed, signature))
}
