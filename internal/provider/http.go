package provider

import (
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// HTTPClient preserves an injected client or creates the shared streaming
// transport policy. Only response headers time out; the stream has no time limit.
func HTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
	}
}

// HTTPErrorSummary bounds upstream error bodies and redacts credentials from
// both the response and any read error before the protocol adds its status text.
func HTTPErrorSummary(body io.Reader, secret string) string {
	b, err := io.ReadAll(io.LimitReader(body, 2048))
	summary := strings.TrimSpace(string(b))
	if err != nil {
		summary = err.Error()
	}
	if secret != "" {
		summary = strings.ReplaceAll(summary, secret, "[redacted]")
	}
	return summary
}
