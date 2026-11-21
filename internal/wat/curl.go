package wat

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func (f *Fetcher) fetchWithCurl(target string) ([]byte, error) {
	proxy := f.proxies[0]
	cookie, err := os.CreateTemp("", "watplan-cookies-*.txt")
	if err != nil {
		return nil, err
	}
	cookiePath := cookie.Name()
	cookie.Close()
	defer os.Remove(cookiePath)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, f.curlImpersonate,
		"-sS", "-L", "--compressed",
		"--max-time", "40",
		"--connect-timeout", "15",
		"--proxy", proxy.String(),
		"--cookie-jar", cookiePath,
		"--cookie", cookiePath,
		target,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("%w: curl-impersonate: %s", ErrProxy, message)
	}

	body := stdout.Bytes()
	if len(body) == 0 {
		return nil, fmt.Errorf("%w: curl-impersonate: pusta odpowiedź", ErrProxy)
	}
	if isWAFBlockBody(body) {
		return nil, fmt.Errorf("%w (curl-impersonate)", ErrBlocked)
	}
	return body, nil
}
