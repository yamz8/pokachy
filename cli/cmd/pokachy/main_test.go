package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidServer(t *testing.T) {
	for _, raw := range []string{
		"https://pokachy.example",
		"https://POKACHY.example:8443",
		"http://127.0.0.1:8787",
		"http://[::1]:8787",
	} {
		if err := validServer(raw); err != nil {
			t.Errorf("validServer(%q): %v", raw, err)
		}
	}
	for _, raw := range []string{
		"http://pokachy.example",
		"https://pokachy.example/api",
		"https://pokachy.example/?x=1",
		"https://user@pokachy.example",
		"http://10.0.0.2:8787",
	} {
		if err := validServer(raw); err == nil {
			t.Errorf("validServer(%q) unexpectedly accepted", raw)
		}
	}
}

func TestSaveCreatesPrivateFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config", "credentials.json")
	if err := save(path, Config{Server: "https://pokachy.example", Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	file, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if file.Mode().Perm() != 0600 {
		t.Errorf("credentials mode = %o, want 600", file.Mode().Perm())
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if parent.Mode().Perm() != 0700 {
		t.Errorf("config directory mode = %o, want 700", parent.Mode().Perm())
	}
}

func TestRequestEscapesPathAndCleansAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.EscapedPath(); got != "/api/pokes/a%2Fb" {
			t.Errorf("escaped path = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Errorf("content type = %q", got)
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"no\u001b[31mpe"}`))
	}))
	defer server.Close()
	c := Client{Config: Config{Server: server.URL, Token: "token"}, HTTP: &http.Client{Timeout: time.Second}, Dir: t.TempDir()}
	err := c.poke(context.Background(), "@a/b")
	apiErr := new(APIError)
	if !strings.Contains(err.Error(), "no[31mpe") || strings.Contains(err.Error(), "\x1b") || !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestRequestDoesNotFollowRedirectWithToken(t *testing.T) {
	receivedToken := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("Authorization") != ""
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/state", http.StatusFound)
	}))
	defer redirect.Close()
	c := Client{Config: Config{Server: redirect.URL, Token: "token"}, HTTP: &http.Client{Timeout: time.Second}, Dir: t.TempDir()}
	err := c.request(context.Background(), "GET", "/api/state", nil, nil, "")
	if err == nil || !strings.Contains(err.Error(), "unexpected redirect") {
		t.Fatalf("request error = %v, want redirect rejection", err)
	}
	if receivedToken {
		t.Fatal("redirect target received bearer token")
	}
}

func TestLogoutClearsRevokedLocalSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/sign-out" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"session expired"}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	t.Setenv("POKACHY_CONFIG_DIR", dir)
	if err := save(filepath.Join(dir, "credentials.json"), Config{Server: server.URL, Token: "expired"}); err != nil {
		t.Fatal(err)
	}
	if err := save(filepath.Join(dir, "state.json"), State{}); err != nil {
		t.Fatal(err)
	}
	if err := save(filepath.Join(dir, "notified.json"), map[string]bool{"poke": true}); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"logout"}); err != nil {
		t.Fatalf("logout with revoked token: %v", err)
	}
	for _, name := range []string{"credentials.json", "state.json", "notified.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still exists or could not be checked: %v", name, err)
		}
	}
}

func TestPokeAcknowledgementIsNotHiddenByRefreshFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/pokes/friend":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/state":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"temporarily unavailable"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	t.Setenv("POKACHY_CONFIG_DIR", dir)
	if err := save(filepath.Join(dir, "credentials.json"), Config{Server: server.URL, Token: "valid"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"poke", "@friend"}); err != nil {
		t.Fatalf("successful poke should not fail when cache refresh fails: %v", err)
	}
}

type failingTransport struct{ err error }

func (t failingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, t.err }

func TestTransportFailureDiagnosticsDoNotExposeRequestDetails(t *testing.T) {
	for _, tc := range []struct {
		err      error
		category string
	}{
		{io.EOF, "connection closed"},
		{context.Canceled, "request canceled"},
		{context.DeadlineExceeded, "request timed out"},
		{errors.New("private-token-and-host"), "network error"},
	} {
		c := Client{Config: Config{Server: "https://private.example", Token: "secret-token"}, HTTP: &http.Client{Transport: failingTransport{tc.err}}}
		err := c.request(context.Background(), "POST", "/api/auth/device/token", map[string]string{"device_code": "private-code"}, nil, "")
		if err == nil || !strings.Contains(err.Error(), "("+tc.category+")") {
			t.Fatalf("missing category: %v", err)
		}
		for _, secret := range []string{"private", "secret-token", "/api/auth"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("request details leaked: %v", err)
			}
		}
	}
}
