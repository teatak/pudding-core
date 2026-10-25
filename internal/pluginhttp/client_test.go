package pluginhttp

import (
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEndpointTLSIsExplicitIsolatedAndReusesTransport(t *testing.T) {
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	target.Config.ErrorLog = log.New(io.Discard, "", 0)
	target.StartTLS()
	defer target.Close()
	original := http.DefaultTransport.(*http.Transport).Clone()
	original.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	defer original.CloseIdleConnections()
	base := &http.Client{Transport: original}
	for _, enabled := range []bool{false, true, false} {
		client, err := Client(base, target.URL, enabled)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Get(target.URL)
		if enabled {
			if err != nil {
				t.Fatal("opt-in did not allow self-signed certificate", err)
			}
			response.Body.Close()
		} else if err == nil {
			response.Body.Close()
			t.Fatal("strict connection accepted self-signed certificate")
		}
	}
	if original.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("modified original TLS configuration")
	}
	first, _ := Client(base, target.URL, true)
	second, _ := Client(base, target.URL, true)
	if first.Transport != second.Transport {
		t.Fatal("did not reuse transport")
	}
	modified := first.Transport.(*http.Transport)
	defer modified.CloseIdleConnections()
	if modified == original || modified.TLSClientConfig == original.TLSClientConfig || modified.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("TLS configuration was not cloned and preserved")
	}
	if base.CheckRedirect != nil {
		t.Fatal("mutated base client")
	}
	if _, err := Client(base, "http://localhost", true); err == nil {
		t.Fatal("enabled TLS exception for HTTP")
	}
}

func TestEndpointTLSDoesNotFollowRedirects(t *testing.T) {
	reached := false
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer other.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 307) }))
	defer source.Close()
	client, err := Client(http.DefaultClient, source.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(source.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 307 || reached {
		t.Fatal("TLS exception followed a redirect")
	}
}
