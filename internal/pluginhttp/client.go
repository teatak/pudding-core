// Package pluginhttp applies a connection endpoint's TLS policy without changing
// shared clients or allowing that policy to follow redirects to another server.
package pluginhttp

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"sync"
)

var insecureTransports sync.Map // original *http.Transport -> cloned *http.Transport

func Client(base *http.Client, endpointURL string, insecureTLS bool) (*http.Client, error) {
	client := *base
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if !insecureTLS {
		return &client, nil
	}
	endpoint, err := url.Parse(endpointURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" {
		return nil, fmt.Errorf("ignoring certificate errors requires an HTTPS endpoint")
	}
	transport := base.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	original, ok := transport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("transport does not support endpoint TLS settings")
	}
	cached, ok := insecureTransports.Load(original)
	if !ok {
		cloned := original.Clone()
		if cloned.TLSClientConfig == nil {
			cloned.TLSClientConfig = &tls.Config{}
		} else {
			cloned.TLSClientConfig = cloned.TLSClientConfig.Clone()
		}
		// Explicitly selected by the user for this connection's HTTPS endpoint.
		cloned.TLSClientConfig.InsecureSkipVerify = true
		cached, _ = insecureTransports.LoadOrStore(original, cloned)
	}
	client.Transport = cached.(*http.Transport)
	return &client, nil
}
