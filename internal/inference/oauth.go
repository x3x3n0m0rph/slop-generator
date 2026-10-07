package inference

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"slop-generator/internal/config"
)

type oauthMetadata struct {
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	PKCEMethods           []string `json:"code_challenge_methods_supported"`
}

type oauthTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
}

type oauthHTTPError struct {
	Status int
	Code   string
}

func (e *oauthHTTPError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("OAuth2 endpoint returned HTTP %d (%s)", e.Status, e.Code)
	}
	return fmt.Sprintf("OAuth2 endpoint returned HTTP %d", e.Status)
}

type OAuthManager struct {
	cfg        config.Provider
	tokenURL   string
	cachePath  string
	authClient *http.Client
	mu         sync.Mutex
	tokens     oauthTokens
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	closeOnce  sync.Once
}

// NewOAuthManager discovers endpoints, restores or obtains tokens, then starts refresh.
func NewOAuthManager(ctx context.Context, cfg config.Provider, cachePath string) (*OAuthManager, error) {
	if cfg.AuthMode != "oauth2" {
		return nil, fmt.Errorf("provider is not configured for OAuth2")
	}
	authHTTP, err := newHTTPClient(cfg, cfg.OAuth2.TLSInsecureSkipVerify)
	if err != nil {
		return nil, err
	}
	m := &OAuthManager{cfg: cfg, cachePath: cachePath, authClient: authHTTP}
	meta, err := m.discover(ctx)
	if err != nil {
		m.Close()
		return nil, err
	}
	m.tokenURL = meta.TokenEndpoint

	cached, found, err := readOAuthTokens(cachePath)
	if err != nil {
		m.Close()
		return nil, err
	}
	if found {
		fresh, refreshErr := m.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "client_id": {cfg.OAuth2.ClientID}, "refresh_token": {cached.RefreshToken}}, cached.RefreshToken)
		if refreshErr == nil {
			m.tokens = fresh
		} else {
			var endpointErr *oauthHTTPError
			if !errors.As(refreshErr, &endpointErr) || (endpointErr.Code != "invalid_grant" && endpointErr.Code != "invalid_token") {
				m.Close()
				return nil, fmt.Errorf("restore OAuth2 session: %w", refreshErr)
			}
			if err = os.Remove(cachePath); err != nil && !os.IsNotExist(err) {
				m.Close()
				return nil, fmt.Errorf("cannot replace expired OAuth2 session")
			}
			m.tokens, err = m.authorize(ctx, meta.AuthorizationEndpoint)
		}
	} else {
		m.tokens, err = m.authorize(ctx, meta.AuthorizationEndpoint)
	}
	if err != nil {
		m.Close()
		return nil, err
	}
	if err = m.saveTokens(m.tokens); err != nil {
		m.Close()
		return nil, err
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.wg.Add(1)
	go m.refreshLoop()
	return m, nil
}

// Session returns a provider handle whose request settings and Close are local to one task.
func (m *OAuthManager) Session(cfg config.Provider) (*OAuthSession, error) {
	if cfg.AuthMode != "oauth2" || cfg.OAuth2 != m.cfg.OAuth2 {
		return nil, fmt.Errorf("OAuth2 session configuration does not match")
	}
	client, err := newHTTPClient(cfg, false)
	if err != nil {
		return nil, err
	}
	provider := &Client{cfg: cfg, oauth: m, client: client}
	return &OAuthSession{client: provider}, nil
}

type OAuthSession struct{ client *Client }

func (s *OAuthSession) Generate(ctx context.Context, request Request) (Result, error) {
	return s.client.Generate(ctx, request)
}
func (s *OAuthSession) Close() { s.client.Close() }

func (m *OAuthManager) Close() {
	m.closeOnce.Do(func() {
		if m.cancel != nil {
			m.cancel()
			m.wg.Wait()
		}
		if m.authClient != nil {
			m.authClient.CloseIdleConnections()
		}
	})
}

func (m *OAuthManager) discover(ctx context.Context) (oauthMetadata, error) {
	var metadata oauthMetadata
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.cfg.OAuth2.WellKnownURL, nil)
	if err != nil {
		return metadata, fmt.Errorf("invalid OAuth2 well-known URL")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := m.authClient.Do(req)
	if err != nil {
		return metadata, fmt.Errorf("cannot load OAuth2 discovery document")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return metadata, fmt.Errorf("OAuth2 discovery returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil || len(body) > 1<<20 || json.Unmarshal(body, &metadata) != nil {
		return metadata, fmt.Errorf("invalid OAuth2 discovery document")
	}
	if err := validOAuthEndpoint(metadata.AuthorizationEndpoint); err != nil {
		return metadata, fmt.Errorf("discovery authorization_endpoint is invalid")
	}
	if err := validOAuthEndpoint(metadata.TokenEndpoint); err != nil {
		return metadata, fmt.Errorf("discovery token_endpoint is invalid")
	}
	if metadata.PKCEMethods != nil {
		supported := false
		for _, method := range metadata.PKCEMethods {
			if method == "S256" {
				supported = true
				break
			}
		}
		if !supported {
			return metadata, fmt.Errorf("OAuth2 server does not support PKCE S256")
		}
	}
	return metadata, nil
}

func validOAuthEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("invalid endpoint")
	}
	return nil
}

func (m *OAuthManager) authorize(ctx context.Context, endpoint string) (oauthTokens, error) {
	var tokens oauthTokens
	redirect, err := url.Parse(m.cfg.OAuth2.RedirectURI)
	if err != nil {
		return tokens, fmt.Errorf("invalid OAuth2 redirect URI")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(redirect.Hostname(), redirect.Port()))
	if err != nil {
		return tokens, fmt.Errorf("cannot listen on OAuth2 redirect address")
	}
	defer listener.Close()

	verifier, err := randomURLToken(64)
	if err != nil {
		return tokens, fmt.Errorf("cannot create PKCE verifier")
	}
	state, err := randomURLToken(32)
	if err != nil {
		return tokens, fmt.Errorf("cannot create OAuth2 state")
	}
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	authURL, err := url.Parse(endpoint)
	if err != nil {
		return tokens, fmt.Errorf("invalid authorization endpoint")
	}
	query := authURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", m.cfg.OAuth2.ClientID)
	query.Set("redirect_uri", m.cfg.OAuth2.RedirectURI)
	query.Set("scope", m.cfg.OAuth2.Scope)
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	authURL.RawQuery = query.Encode()

	type callback struct{ code, state, codeError string }
	callbackResult := make(chan callback, 1)
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc(redirect.Path, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != redirect.Path {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		values := r.URL.Query()
		response := callback{code: values.Get("code"), state: values.Get("state"), codeError: values.Get("error")}
		once.Do(func() { callbackResult <- response })
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if response.codeError != "" || response.code == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "<!doctype html><title>Authorization failed</title><p>Authorization failed. Return to the application.</p>")
			return
		}
		_, _ = io.WriteString(w, "<!doctype html><title>Authorization complete</title><p>Authorization complete. You can close this window.</p>")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	defer func() { _ = server.Shutdown(context.Background()) }()

	fmt.Fprintf(os.Stderr, "Opening OAuth2 authorization in a browser:\n%s\n", authURL.String())
	if err := openBrowser(authURL.String()); err != nil {
		fmt.Fprintln(os.Stderr, "Open the authorization URL above in your browser.")
	}
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var response callback
	select {
	case response = <-callbackResult:
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return tokens, fmt.Errorf("OAuth2 callback listener stopped")
		}
		return tokens, fmt.Errorf("OAuth2 callback listener stopped")
	case <-waitCtx.Done():
		return tokens, fmt.Errorf("timed out waiting for OAuth2 callback")
	}
	if response.codeError != "" {
		return tokens, fmt.Errorf("OAuth2 authorization failed (%s)", response.codeError)
	}
	if response.code == "" {
		return tokens, fmt.Errorf("OAuth2 authorization code was not returned")
	}
	if subtle.ConstantTimeCompare([]byte(state), []byte(response.state)) != 1 {
		return tokens, fmt.Errorf("OAuth2 state mismatch")
	}
	form := url.Values{
		"grant_type": {"authorization_code"}, "client_id": {m.cfg.OAuth2.ClientID},
		"code": {response.code}, "redirect_uri": {m.cfg.OAuth2.RedirectURI}, "code_verifier": {verifier},
	}
	return m.exchange(waitCtx, form, "")
}

func randomURLToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func openBrowser(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		command = exec.Command("open", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func (m *OAuthManager) exchange(ctx context.Context, form url.Values, previousRefresh string) (oauthTokens, error) {
	var tokens oauthTokens
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokens, fmt.Errorf("invalid OAuth2 token endpoint")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := m.authClient.Do(req)
	if err != nil {
		return tokens, fmt.Errorf("OAuth2 token request failed")
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if readErr != nil || len(body) > 1<<20 {
		return tokens, fmt.Errorf("invalid OAuth2 token response")
	}
	var result tokenResponse
	if json.Unmarshal(body, &result) != nil {
		return tokens, fmt.Errorf("invalid OAuth2 token response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tokens, &oauthHTTPError{Status: resp.StatusCode, Code: result.Error}
	}
	if result.AccessToken == "" || result.ExpiresIn < 1 {
		return tokens, fmt.Errorf("OAuth2 token response is missing access_token or expires_in")
	}
	if result.TokenType != "" && !strings.EqualFold(result.TokenType, "bearer") {
		return tokens, fmt.Errorf("OAuth2 token response has unsupported token_type")
	}
	refresh := result.RefreshToken
	if refresh == "" {
		refresh = previousRefresh
	}
	if refresh == "" {
		return tokens, fmt.Errorf("OAuth2 token response is missing refresh_token")
	}
	return oauthTokens{AccessToken: result.AccessToken, RefreshToken: refresh, ExpiresAt: time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)}, nil
}

func readOAuthTokens(path string) (oauthTokens, bool, error) {
	var tokens oauthTokens
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return tokens, false, nil
	}
	if err != nil || json.Unmarshal(data, &tokens) != nil || tokens.RefreshToken == "" {
		return tokens, false, fmt.Errorf("OAuth2 token cache is unreadable; remove it to sign in again")
	}
	return tokens, true, nil
}

func (m *OAuthManager) saveTokens(tokens oauthTokens) error {
	if err := os.MkdirAll(filepath.Dir(m.cachePath), 0700); err != nil {
		return fmt.Errorf("cannot create OAuth2 token cache directory")
	}
	data, err := json.Marshal(tokens)
	if err != nil {
		return fmt.Errorf("cannot encode OAuth2 token cache")
	}
	file, err := os.CreateTemp(filepath.Dir(m.cachePath), ".oauth2-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create OAuth2 token cache")
	}
	temp := file.Name()
	defer os.Remove(temp)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temp, m.cachePath)
	}
	if err != nil {
		return fmt.Errorf("cannot persist OAuth2 token cache")
	}
	return nil
}

func (m *OAuthManager) refreshLocked(ctx context.Context) error {
	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {m.cfg.OAuth2.ClientID}, "refresh_token": {m.tokens.RefreshToken}}
	tokens, err := m.exchange(ctx, form, m.tokens.RefreshToken)
	if err != nil {
		return err
	}
	if err := m.saveTokens(tokens); err != nil {
		return err
	}
	m.tokens = tokens
	return nil
}

func (m *OAuthManager) accessToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tokens.AccessToken != "" && time.Until(m.tokens.ExpiresAt) > 30*time.Second {
		return m.tokens.AccessToken, nil
	}
	if err := m.refreshLocked(ctx); err != nil {
		return "", fmt.Errorf("cannot refresh OAuth2 access token")
	}
	return m.tokens.AccessToken, nil
}

func (m *OAuthManager) refreshLoop() {
	defer m.wg.Done()
	for {
		m.mu.Lock()
		wait := time.Until(m.tokens.ExpiresAt.Add(-time.Minute))
		m.mu.Unlock()
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-m.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		m.mu.Lock()
		err := m.refreshLocked(m.ctx)
		m.mu.Unlock()
		if err != nil {
			timer.Reset(15 * time.Second)
			select {
			case <-m.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}
