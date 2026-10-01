package inference

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"slop-generator/internal/config"
)

func testProvider(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	t.Setenv("SLOP_TEST_KEY", "private-test-key")
	p, e := NewHTTP(config.Provider{BaseURL: s.URL, Model: "test", KeyEnv: "SLOP_TEST_KEY", MaxTokens: 42, Timeout: 1})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.client.CloseIdleConnections)
	return p
}
func TestProviderRequestAndUsage(t *testing.T) {
	p := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer private-test-key" {
			t.Error("invalid request")
		}
		var body map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		if body["max_tokens"] != float64(42) || body["stream"] != false {
			t.Error("invalid limits")
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))
	})
	result, e := p.Generate(context.Background(), Request{[]Message{{"user", "hello"}}})
	if e != nil || result.Content != "hello" || result.Usage == nil || result.Usage.Total != 5 {
		t.Fatalf("%+v %v", result, e)
	}
}
func TestProviderFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       string
	}{{"http", "private-test-key", 401, "HTTP 401"}, {"json", "not json", 200, "invalid inference JSON"}, {"empty choices", `{"choices":[]}`, 200, "no inference choices"}, {"truncated", `{"choices":[{"message":{"content":"half"},"finish_reason":"length"}]}`, 200, "token limit"}} {
		t.Run(tc.name, func(t *testing.T) {
			p := testProvider(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) })
			_, e := p.Generate(context.Background(), Request{})
			if e == nil || !strings.Contains(e.Error(), tc.want) || strings.Contains(e.Error(), "private-test-key") {
				t.Fatalf("unexpected error %v", e)
			}
		})
	}
}
func TestProviderCancellationAndTimeout(t *testing.T) {
	p := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := p.Generate(ctx, Request{}); e != context.Canceled {
		t.Fatalf("%v", e)
	}
	start := time.Now()
	if _, e := p.Generate(context.Background(), Request{}); e == nil {
		t.Fatal("expected timeout")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout ignored")
	}
}
func TestProviderMissingUsage(t *testing.T) {
	p := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":""}}]}`))
	})
	r, e := p.Generate(context.Background(), Request{})
	if e != nil || r.Usage != nil || r.Content != "" {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestSOCKSFileFormats(t *testing.T) {
	for _, raw := range []string{"socks5://name:secret@127.0.0.1:1080", "127.0.0.1:1080\r\nuser: name\r\npass: secret\r\n", "127.0.0.1:1080\nusername: name\npassword: secret"} {
		u, err := parseSOCKS(raw)
		if err != nil {
			t.Fatal(err)
		}
		password, _ := u.User.Password()
		if u.Host != "127.0.0.1:1080" || u.User.Username() != "name" || password != "secret" {
			t.Fatal("incorrect proxy parsing")
		}
	}
	for _, raw := range []string{"http://127.0.0.1:1080", "socks5://host", "host:1080\nuser: name", "host:1080\nuser: name\nunknown: secret"} {
		if _, err := parseSOCKS(raw); err == nil {
			t.Fatal("invalid proxy accepted")
		}
	}
}
