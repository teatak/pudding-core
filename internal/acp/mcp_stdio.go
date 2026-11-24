package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// ForwardHostMCP is the installed binary's stdio transport. It never reads the
// desktop descriptor or daemon token, and cancellation closes its HTTP request.
func ForwardHostMCP(endpoint, secret string, stdin io.Reader, stdout io.Writer) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || !net.ParseIP(u.Hostname()).IsLoopback() || u.Port() == "" || u.User != nil || u.Path != "/local-agent-mcp" || u.RawQuery != "" || secret == "" {
		return errors.New("invalid ACP host endpoint or missing connection credential")
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	var mu, outMu sync.Mutex
	var wg sync.WaitGroup
	pending := map[string]context.CancelFunc{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	send := func(value any) { outMu.Lock(); defer outMu.Unlock(); _ = json.NewEncoder(stdout).Encode(value) }
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 64*1024), 16<<20)
	for scanner.Scan() {
		raw := append([]byte(nil), scanner.Bytes()...)
		var msg message
		if err := json.Unmarshal(raw, &msg); err != nil {
			send(message{JSONRPC: "2.0", ID: json.RawMessage(`null`), Error: &RPCError{Code: -32700, Message: "invalid JSON"}})
			continue
		}
		if msg.Method == "notifications/cancelled" {
			var p struct {
				RequestID json.RawMessage `json:"requestId"`
			}
			if json.Unmarshal(msg.Params, &p) == nil {
				mu.Lock()
				if stop := pending[string(p.RequestID)]; stop != nil {
					stop()
				}
				mu.Unlock()
			}
			continue
		}
		if len(msg.ID) == 0 {
			continue
		}
		id := string(msg.ID)
		requestCtx, stop := context.WithCancel(ctx)
		mu.Lock()
		if pending[id] != nil {
			mu.Unlock()
			stop()
			send(message{JSONRPC: "2.0", ID: msg.ID, Error: &RPCError{Code: -32600, Message: "duplicate active request id"}})
			continue
		}
		pending[id] = stop
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer stop()
			defer func() { mu.Lock(); delete(pending, id); mu.Unlock() }()
			req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+secret)
				req.Header.Set("Content-Type", "application/json")
				var res *http.Response
				res, err = client.Do(req)
				if err == nil {
					defer res.Body.Close()
					if res.StatusCode == http.StatusOK {
						var reply json.RawMessage
						err = json.NewDecoder(io.LimitReader(res.Body, 64<<20)).Decode(&reply)
						if err == nil {
							send(reply)
							return
						}
					} else {
						err = fmt.Errorf("Pudding host returned %s", res.Status)
					}
				}
			}
			if requestCtx.Err() != nil {
				err = requestCtx.Err()
			}
			text := err.Error()
			// HTTP errors can contain URLs, but this URL never contains a credential.
			text = strings.TrimSpace(text)
			send(message{JSONRPC: "2.0", ID: msg.ID, Error: &RPCError{Code: -32603, Message: text}})
		}()
	}
	cancel()
	wg.Wait()
	return scanner.Err()
}
