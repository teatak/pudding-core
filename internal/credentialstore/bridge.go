package credentialstore

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type Bridge struct {
	home   string
	client *http.Client
}

func NewBridge(home string) *Bridge {
	return &Bridge{home, &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (b *Bridge) Seal(data []byte) ([]byte, error) { return b.call("seal", data) }
func (b *Bridge) Open(data []byte) ([]byte, error) { return b.call("open", data) }
func (b *Bridge) call(operation string, data []byte) ([]byte, error) {
	// Read on every call so an attached Desktop can replace a stale descriptor.
	raw, err := os.ReadFile(filepath.Join(b.home, "run", "plugin-credentials.json"))
	if err != nil {
		return nil, ErrUnavailable
	}
	var d struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if json.Unmarshal(raw, &d) != nil || len(d.Token) != 64 {
		return nil, ErrUnavailable
	}
	u, err := url.Parse(d.URL)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, ErrUnavailable
	}
	body, _ := json.Marshal(map[string]string{"data": base64.StdEncoding.EncodeToString(data)})
	req, err := http.NewRequest("POST", d.URL+"/"+operation, bytes.NewReader(body))
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+d.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, ErrUnavailable
	}
	var result struct {
		Data string `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result) != nil {
		return nil, ErrUnavailable
	}
	out, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil || len(out) == 0 {
		return nil, ErrUnavailable
	}
	return out, nil
}
