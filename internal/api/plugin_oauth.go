package api

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/oauthbroker"
	"github.com/teatak/pudding-core/internal/plugin"
)

const pluginOAuthStateTTL = 10 * time.Minute

type pluginOAuthProvider struct {
	PluginID              string
	ClientID              string
	AuthorizeURL          string
	ExchangeURL           string
	RedirectURI           string
	Scopes                []string
	AuthorizeParams       map[string]string
	DefaultConnectionName string
}

type oauthStartState struct {
	Provider       string
	PluginID       string
	AuthMethodID   string
	ConnectionID   string
	ConnectionName string
	Fields         map[string]string
	EndpointURLs   map[string]string
	RedirectURI    string
	Verifier       string
	CreatedAt      time.Time
}

type startPluginOAuthReq struct {
	ConnectionID   string            `json:"connectionID"`
	PluginID       string            `json:"pluginID"`
	AuthMethodID   string            `json:"authMethodID"`
	ConnectionName string            `json:"connectionName"`
	Fields         map[string]string `json:"fields"`
	EndpointURLs   map[string]string `json:"endpointURLs"`
}

type startPluginOAuthResp struct {
	AuthorizationURL string `json:"authorizationURL"`
}

type completePluginOAuthReq struct {
	Provider string `json:"provider"`
	Ticket   string `json:"ticket"`
	State    string `json:"state"`
	Error    string `json:"error"`
}

type pluginOAuthTokenResp struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	TokenType             string `json:"token_type"`
	Scope                 string `json:"scope"`
	ExpiresIn             int64  `json:"expires_in"`
	Error                 string `json:"error"`
	ErrorDescription      string `json:"error_description"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
}

var pluginOAuthProviders = map[string]pluginOAuthProvider{
	"gmail": {
		PluginID:     "gmail",
		ClientID:     "226317408426-s2jpl76do0qegl9vesjn1osrkbos1t9o.apps.googleusercontent.com",
		AuthorizeURL: "https://accounts.google.com/o/oauth2/v2/auth",
		ExchangeURL:  "https://oauth.x-t.top/gmail/exchange",
		Scopes: []string{
			"openid",
			"email",
			"profile",
			"https://www.googleapis.com/auth/gmail.readonly",
		},
		AuthorizeParams: map[string]string{
			"access_type":            "offline",
			"include_granted_scopes": "true",
			"prompt":                 "consent",
		},
		DefaultConnectionName: "Gmail",
	},
}

func (s *Server) startPluginOAuth(c *cart.Context) error {
	var req startPluginOAuthReq
	if err := decode(c, &req); err != nil && !errors.Is(err, io.EOF) {
		return badRequest(c, "invalid json body")
	}
	pluginID := strings.TrimSpace(req.PluginID)
	def, err := s.getPluginDefinition(c.Request.Context(), pluginID)
	if err != nil {
		if errors.Is(err, plugin.ErrNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "plugin_not_found"})
			return nil
		}
		return s.fail(c, err)
	}
	method, ok := plugin.FindAuthMethod(def, req.AuthMethodID, plugin.AuthTypeOAuth2)
	if !ok || method.Type != plugin.AuthTypeOAuth2 {
		return badRequest(c, "oauth2 is not supported by plugin")
	}
	providerID := method.Provider
	if providerID == "" {
		providerID = pluginID
	}
	githubAppFlow := providerID == "github" && method.ID == plugin.GitHubAppAuthMethodID
	provider, legacyProvider := pluginOAuthProviders[providerID]
	if providerID == "github" && !githubAppFlow {
		return badRequest(c, "github oauth method is not supported")
	}
	if !githubAppFlow && !legacyProvider {
		return badRequest(c, "oauth provider is not configured for plugin")
	}
	cfg, ok := s.pluginConnectionConfig(c)
	if !ok {
		return nil
	}
	connections, err := cfg.ListPluginConnections(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	name := strings.TrimSpace(req.ConnectionName)
	connectionID := strings.TrimSpace(req.ConnectionID)
	if strings.ContainsAny(connectionID, "/ ") {
		return badRequest(c, "connection id must not contain '/' or spaces")
	}
	var existing *plugin.Connection
	if connectionID != "" {
		for _, conn := range connections {
			if conn == nil || conn.ID != connectionID {
				continue
			}
			if conn.PluginID != pluginID {
				return badRequest(c, "connection does not belong to plugin")
			}
			if name == "" {
				name = conn.Name
			}
			existing = conn
			break
		}
	}
	fieldsInput := req.Fields
	if fieldsInput == nil && existing != nil {
		fieldsInput = existing.Fields
	}
	fields, err := normalizePluginConnectionFields(def.Connection, fieldsInput, existing)
	if err != nil {
		return badRequest(c, err.Error())
	}
	endpointURLsInput := req.EndpointURLs
	if endpointURLsInput == nil && existing != nil {
		endpointURLsInput = existing.EndpointURLs
	}
	endpointURLs, err := plugin.NormalizeConnectionEndpointURLs(def, endpointURLsInput)
	if err != nil {
		return badRequest(c, err.Error())
	}
	state, err := randomOAuthState()
	if err != nil {
		return s.fail(c, err)
	}
	if name == "" && githubAppFlow {
		name = "GitHub"
	} else if name == "" {
		name = provider.DefaultConnectionName
	}
	if connectionID == "" {
		connectionID = nextPluginOAuthConnectionID(pluginID, connections)
	}
	redirectURI := ""
	verifier := ""
	authorizationURL := ""
	if githubAppFlow {
		verifier, err = randomOAuthState()
		if err != nil {
			return s.fail(c, err)
		}
		challengeBytes := sha256.Sum256([]byte(verifier))
		started, err := s.oauthBroker.Start(c.Request.Context(), oauthbroker.StartRequest{
			Provider: "github", Client: pluginOAuthBrokerClient(), ClientState: state, Flow: "install",
			Challenge: base64.RawURLEncoding.EncodeToString(challengeBytes[:]),
		})
		if err != nil {
			return s.fail(c, err)
		}
		authorizationURL = started.AuthorizationURL
	} else {
		redirectURI = provider.RedirectURI
		if redirectURI == "" {
			redirectURI = localOAuthCallbackURL(c.Request, providerID)
		}
		authorizationURL, err = buildPluginOAuthAuthorizeURL(provider, redirectURI, state)
		if err != nil {
			return s.fail(c, err)
		}
	}
	s.rememberOAuthState(state, oauthStartState{
		Provider:       providerID,
		PluginID:       pluginID,
		AuthMethodID:   method.ID,
		ConnectionID:   connectionID,
		ConnectionName: name,
		Fields:         fields,
		EndpointURLs:   endpointURLs,
		RedirectURI:    redirectURI,
		Verifier:       verifier,
		CreatedAt:      time.Now(),
	})
	c.JSON(http.StatusOK, startPluginOAuthResp{AuthorizationURL: authorizationURL})
	return nil
}

func (s *Server) pluginOAuthCallback(c *cart.Context) error {
	providerID, _ := c.Param("provider")
	providerID = strings.TrimSpace(providerID)
	provider, configured := pluginOAuthProviders[providerID]
	if !configured {
		return oauthHTML(c, http.StatusBadRequest, "Authorization failed", "Provider is not configured.")
	}
	errorCode := strings.TrimSpace(c.Request.URL.Query().Get("error"))
	if errorCode != "" {
		return oauthHTML(c, http.StatusBadRequest, "Authorization failed", errorCode)
	}
	code := strings.TrimSpace(c.Request.URL.Query().Get("code"))
	state := strings.TrimSpace(c.Request.URL.Query().Get("state"))
	if code == "" || state == "" {
		return oauthHTML(c, http.StatusBadRequest, "Authorization failed", "Missing code or state.")
	}
	start, ok := s.takeOAuthState(state)
	if !ok || start.Provider != providerID {
		return oauthHTML(c, http.StatusBadRequest, "Authorization expired", "Please return to Pudding and try again.")
	}
	token, err := exchangePluginOAuthCode(c.Request, provider, code, start.RedirectURI)
	if err != nil {
		return oauthHTML(c, http.StatusBadGateway, "Authorization failed", err.Error())
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return oauthHTML(c, http.StatusBadGateway, "Authorization failed", "Provider did not return an access token.")
	}
	cfg, ok := s.pluginConnectionConfig(c)
	if !ok {
		return nil
	}
	auth := plugin.Auth{
		MethodID:     start.AuthMethodID,
		Type:         "oauth2",
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Scopes:       oauthScopes(token.Scope),
	}
	if token.ExpiresIn > 0 {
		auth.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	conn := &plugin.Connection{
		ID:           start.ConnectionID,
		Name:         start.ConnectionName,
		PluginID:     start.PluginID,
		Fields:       start.Fields,
		EndpointURLs: start.EndpointURLs,
		Auth:         auth,
	}
	if err := cfg.PutPluginConnection(c.Request.Context(), conn); err != nil {
		return oauthHTML(c, http.StatusInternalServerError, "Authorization failed", err.Error())
	}
	return oauthSuccessHTML(c, providerID)
}

func (s *Server) completePluginOAuth(c *cart.Context) error {
	var req completePluginOAuthReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	providerID := strings.TrimSpace(req.Provider)
	state := strings.TrimSpace(req.State)
	start, ok := s.takeOAuthState(state)
	if !ok || start.Provider != providerID || providerID != "github" || start.AuthMethodID != plugin.GitHubAppAuthMethodID || start.Verifier == "" {
		return badRequest(c, "oauth authorization expired")
	}
	if errorCode := strings.TrimSpace(req.Error); errorCode != "" {
		return badRequest(c, "github authorization was cancelled: "+errorCode)
	}
	if strings.TrimSpace(req.Ticket) == "" {
		return badRequest(c, "oauth ticket is required")
	}
	token, err := s.oauthBroker.Redeem(c.Request.Context(), req.Ticket, start.Verifier)
	if err != nil {
		return s.fail(c, err)
	}
	account, err := s.github.Account(c.Request.Context(), token.AccessToken)
	if err != nil {
		return s.fail(c, err)
	}
	cfg, ok := s.pluginConnectionConfig(c)
	if !ok {
		return nil
	}
	connections, err := cfg.ListPluginConnections(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	targetID := start.ConnectionID
	name := strings.TrimSpace(start.ConnectionName)
	for _, connection := range connections {
		if connection == nil || connection.PluginID != "github" || connection.Auth.Type != plugin.AuthTypeOAuth2 || connection.Account == nil || connection.Account.ID != account.ID || connection.ID == targetID {
			continue
		}
		targetID = connection.ID
		if strings.TrimSpace(connection.Name) != "" {
			name = connection.Name
		}
		break
	}
	if name == "" || name == "GitHub" {
		name = "GitHub · " + account.Login
	}
	auth := plugin.Auth{
		MethodID:     start.AuthMethodID,
		Type:         plugin.AuthTypeOAuth2,
		Variant:      plugin.GitHubAppAuthVariant,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Scopes:       oauthScopes(token.Scope),
	}
	if token.ExpiresIn > 0 {
		auth.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	if token.RefreshTokenExpiresIn > 0 {
		auth.RefreshExpiresAt = time.Now().Add(time.Duration(token.RefreshTokenExpiresIn) * time.Second)
	}
	connection := &plugin.Connection{
		ID: targetID, Name: name, PluginID: start.PluginID, Fields: start.Fields, EndpointURLs: start.EndpointURLs,
		Account: &plugin.ConnectionAccount{ID: account.ID, Login: account.Login, Name: account.Name, AvatarURL: account.AvatarURL, Type: account.Type},
		Auth:    auth,
	}
	if err := cfg.PutPluginConnection(c.Request.Context(), connection); err != nil {
		return s.fail(c, err)
	}
	updated, err := cfg.GetPluginConnection(c.Request.Context(), targetID)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, plugin.ViewConnection(updated))
	return nil
}

func buildPluginOAuthAuthorizeURL(provider pluginOAuthProvider, redirectURI, state string) (string, error) {
	u, err := url.Parse(provider.AuthorizeURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("client_id", provider.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("state", state)
	if len(provider.Scopes) > 0 {
		q.Set("scope", strings.Join(provider.Scopes, " "))
	}
	for key, value := range provider.AuthorizeParams {
		if strings.TrimSpace(key) == "" {
			continue
		}
		q.Set(key, value)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func exchangePluginOAuthCode(r *http.Request, provider pluginOAuthProvider, code, redirectURI string) (*pluginOAuthTokenResp, error) {
	body, err := json.Marshal(map[string]string{
		"code":         code,
		"redirect_uri": redirectURI,
	})
	if err != nil {
		return nil, err
	}
	ctx := r.Context()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.ExchangeURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}
	var token pluginOAuthTokenResp
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if token.ErrorDescription != "" {
			return nil, errors.New(token.ErrorDescription)
		}
		if token.Error != "" {
			return nil, errors.New(token.Error)
		}
		return nil, fmt.Errorf("token exchange failed: %s", resp.Status)
	}
	if token.Error != "" {
		if token.ErrorDescription != "" {
			return nil, errors.New(token.ErrorDescription)
		}
		return nil, errors.New(token.Error)
	}
	return &token, nil
}

func localOAuthCallbackURL(r *http.Request, pluginID string) string {
	base := requestBaseURL(r)
	u, err := url.Parse(base)
	if err != nil {
		return strings.TrimRight(base, "/") + "/oauth/callback/" + pluginID
	}
	host := u.Host
	if name, port, err := net.SplitHostPort(u.Host); err == nil {
		if isLoopbackHost(name) {
			host = net.JoinHostPort("localhost", port)
		}
	} else if isLoopbackHost(u.Hostname()) {
		host = "localhost"
	}
	u.Host = host
	u.Path = "/oauth/callback/" + pluginID
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (s *Server) rememberOAuthState(state string, start oauthStartState) {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	now := time.Now()
	for key, item := range s.oauth {
		if now.Sub(item.CreatedAt) > pluginOAuthStateTTL {
			delete(s.oauth, key)
		}
	}
	s.oauth[state] = start
}

func (s *Server) takeOAuthState(state string) (oauthStartState, bool) {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	start, ok := s.oauth[state]
	if ok {
		delete(s.oauth, state)
	}
	if !ok || time.Since(start.CreatedAt) > pluginOAuthStateTTL {
		return oauthStartState{}, false
	}
	return start, true
}

func randomOAuthState() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func nextPluginOAuthConnectionID(pluginID string, connections []*plugin.Connection) string {
	used := make(map[string]struct{}, len(connections))
	for _, conn := range connections {
		if conn != nil {
			used[conn.ID] = struct{}{}
		}
	}
	base := pluginID + "-main"
	if _, ok := used[base]; !ok {
		return base
	}
	for i := 2; ; i++ {
		id := pluginID + "-" + strconv.Itoa(i)
		if _, ok := used[id]; !ok {
			return id
		}
	}
}

func oauthScopes(scope string) []string {
	parts := strings.FieldsFunc(scope, func(r rune) bool {
		return r == ',' || r == ' '
	})
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func oauthHTML(c *cart.Context, status int, title, detail string) error {
	return oauthHTMLPage(c, status, title, detail, "")
}

func oauthSuccessHTML(c *cart.Context, providerID string) error {
	return oauthHTMLPage(c, http.StatusOK, "Connected", "You can return to Pudding.", pluginOAuthReturnScheme()+"://oauth/connected/"+url.PathEscape(providerID))
}

func pluginOAuthReturnScheme() string {
	scheme := strings.TrimSpace(os.Getenv("PUDDING_OAUTH_RETURN_SCHEME"))
	if validURLScheme(scheme) {
		return scheme
	}
	return "pudding"
}

func pluginOAuthBrokerClient() string {
	if pluginOAuthReturnScheme() == "pudding-dev" {
		return "desktop_dev"
	}
	return "desktop"
}

func validURLScheme(scheme string) bool {
	if scheme == "" || !asciiLetter(scheme[0]) {
		return false
	}
	for i := 1; i < len(scheme); i++ {
		c := scheme[i]
		if asciiLetter(c) || ('0' <= c && c <= '9') || c == '+' || c == '-' || c == '.' {
			continue
		}
		return false
	}
	return true
}

func asciiLetter(c byte) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func oauthHTMLPage(c *cart.Context, status int, title, detail, openURL string) error {
	openURLJSON, _ := json.Marshal(openURL)
	openAction := ""
	if openURL != "" {
		openAction = `<a class="button" href="` + html.EscapeString(openURL) + `">Open Pudding</a><script>const puddingOpenURL=` + string(openURLJSON) + `;setTimeout(()=>{window.location.href=puddingOpenURL},450);</script>`
	}
	body := `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>` + html.EscapeString(title) + `</title>
  <style>
    :root { color-scheme: light dark; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: Widget; color: CanvasText; }
    main { width: min(440px, calc(100vw - 48px)); text-align: center; }
    .mark { width: 56px; height: 56px; margin: 0 auto 22px; border-radius: 16px; display: grid; place-items: center; background: color-mix(in oklch, CanvasText 8%, transparent); color: #4f46e5; font-size: 30px; font-weight: 700; }
    h1 { margin: 0; font-size: 28px; line-height: 1.2; letter-spacing: 0; }
    p { margin: 12px 0 24px; color: color-mix(in oklch, CanvasText 62%, transparent); font-size: 15px; line-height: 1.6; }
    .button { display: inline-flex; align-items: center; justify-content: center; height: 40px; padding: 0 18px; border-radius: 12px; background: #4f46e5; color: white; text-decoration: none; font-weight: 650; }
  </style>
</head>
<body>
  <main>
    <div class="mark">✓</div>
    <h1>` + html.EscapeString(title) + `</h1>
    <p>` + html.EscapeString(detail) + `</p>
    ` + openAction + `
  </main>
</body>
</html>`
	c.Data(status, "text/html; charset=utf-8", []byte(body))
	return nil
}
