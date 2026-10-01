package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"slop-generator/internal/config"
)

// Client implements OpenAI-compatible inference using a private HTTP transport.
type Client struct {
	cfg    config.Provider
	key    string
	client *http.Client
}

func secretFile(path string) (string, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return "", e
	}
	return strings.TrimSpace(string(b)), nil
}

// NewHTTP resolves credentials and configures the provider's optional SOCKS5 proxy.
func NewHTTP(c config.Provider) (*Client, error) {
	key := strings.TrimSpace(c.APIKey)
	if c.KeyEnv != "" {
		key = os.Getenv(c.KeyEnv)
	}
	var e error
	if c.KeyFile != "" {
		key, e = secretFile(c.KeyFile)
		if e != nil {
			return nil, fmt.Errorf("cannot read API key file")
		}
	}
	if key == "" {
		return nil, fmt.Errorf("API key is empty")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	px := c.SOCKS5
	if c.SOCKS5File != "" {
		px, e = secretFile(c.SOCKS5File)
		if e != nil {
			return nil, fmt.Errorf("cannot read proxy file")
		}
	}
	if px != "" {
		u, e := parseSOCKS(px)
		if e != nil {
			return nil, e
		}
		var auth *proxy.Auth
		if u.User != nil {
			pass, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: pass}
		}
		d, e := proxy.SOCKS5("tcp", u.Host, auth, &net.Dialer{Timeout: 30 * time.Second})
		if e != nil {
			return nil, fmt.Errorf("cannot configure SOCKS5")
		}
		cd, ok := d.(proxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("proxy has no context support")
		}
		tr.Proxy = nil
		tr.DialContext = cd.DialContext
	}
	return &Client{c, key, &http.Client{Transport: tr, Timeout: time.Duration(c.Timeout) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (p *Client) Generate(ctx context.Context, r Request) (Result, error) {
	var out Result
	b, e := json.Marshal(struct {
		Model     string    `json:"model"`
		Messages  []Message `json:"messages"`
		MaxTokens int       `json:"max_tokens"`
		Stream    bool      `json:"stream"`
	}{p.cfg.Model, r.Messages, p.cfg.MaxTokens, false})
	if e != nil {
		return out, e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(p.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(b))
	if e != nil {
		return out, e
	}
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("Content-Type", "application/json")
	resp, e := p.client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, fmt.Errorf("inference request failed (network or timeout)")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, fmt.Errorf("inference HTTP %d", resp.StatusCode)
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024+1))
	if e != nil {
		return out, fmt.Errorf("cannot read inference response")
	}
	if len(raw) > 8*1024*1024 {
		return out, fmt.Errorf("response exceeds 8 MiB")
	}
	var body struct {
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
		Usage *Usage `json:"usage"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return out, fmt.Errorf("invalid inference JSON")
	}
	out.Usage = body.Usage
	if len(body.Choices) == 0 {
		return out, fmt.Errorf("no inference choices")
	}
	if body.Choices[0].FinishReason == "length" {
		return out, fmt.Errorf("output token limit reached")
	}
	out.Content = body.Choices[0].Message.Content
	return out, nil
}

// Close releases idle connections held by this client.
func (p *Client) Close() { p.client.CloseIdleConnections() }
