package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
)

type planBlob struct {
	etag string
	raw  []byte
	gz   []byte
	br   []byte
}

type planBlobs struct {
	mu sync.Mutex
	m  map[string]*planBlob
}

func (c *planBlobs) get(id string, data []byte) *planBlob {
	sum := sha1.Sum(data)
	etag := `"` + hex.EncodeToString(sum[:10]) + `"`
	c.mu.Lock()
	b := c.m[id]
	c.mu.Unlock()
	if b != nil && b.etag == etag {
		return b
	}
	b = &planBlob{etag: etag, raw: data, gz: gzipBytes(data), br: brotliBytes(data)}
	c.mu.Lock()
	if c.m == nil {
		c.m = make(map[string]*planBlob)
	}
	c.m[id] = b
	c.mu.Unlock()
	return b
}

func (b *planBlob) encoded(acceptEncoding string) ([]byte, string) {
	if acceptsEncoding(acceptEncoding, "br") {
		return b.br, "br"
	}
	if acceptsEncoding(acceptEncoding, "gzip") {
		return b.gz, "gzip"
	}
	return b.raw, ""
}

func acceptsEncoding(header, name string) bool {
	for _, part := range strings.Split(header, ",") {
		token, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(token), name) {
			continue
		}
		q := strings.ReplaceAll(strings.TrimSpace(params), " ", "")
		return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
	}
	return false
}

func gzipBytes(data []byte) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Write(data)
	zw.Close()
	return buf.Bytes()
}

func brotliBytes(data []byte) []byte {
	var buf bytes.Buffer
	bw := brotli.NewWriterLevel(&buf, brotli.BestCompression)
	bw.Write(data)
	bw.Close()
	return buf.Bytes()
}
