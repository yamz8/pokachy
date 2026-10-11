package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestAvatarAllowedOnlyGitHubAndServerAvatars(t *testing.T) {
	server, _ := url.Parse("https://pokachy.com")
	for raw, want := range map[string]bool{
		"https://avatars.githubusercontent.com/u/1?v=4":    true,
		"https://pokachy.com/avatars/user/0f0f-version":    true,
		"http://avatars.githubusercontent.com/u/1":         false,
		"https://pokachy.com/api/state":                    false,
		"https://pokachy.com/avatars/../api/state":         false,
		"https://user@avatars.githubusercontent.com/u/1":   false,
		"https://avatars.githubusercontent.com.evil.test/": false,
		"https://evil.test/avatars/x":                      false,
		"file:///etc/passwd":                               false,
		"":                                                 false,
	} {
		if got := avatarAllowed(raw, server); got != want {
			t.Errorf("avatarAllowed(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestSyncCachesAvatarsLocallyWithoutCredentials(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	var avatarRequests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/state":
			_ = json.NewEncoder(w).Encode(State{
				Me:       Me{Handle: "me", Image: server.URL + "/avatars/me/v1"},
				Friends:  []Person{{Handle: "page", Image: server.URL + "/avatars/page/v1"}, {Handle: "moved", Image: server.URL + "/avatars/moved/v1"}},
				Requests: []Person{{Handle: "same", Image: server.URL + "/avatars/me/v1"}},
				Blocked:  []Person{{Handle: "elsewhere", Image: "https://evil.test/avatars/x.png"}},
			})
			return
		}
		avatarRequests.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Errorf("avatar request %s carried credentials", r.URL.Path)
		}
		switch r.URL.Path {
		case "/avatars/me/v1":
			_, _ = w.Write(png)
		case "/avatars/page/v1":
			_, _ = w.Write([]byte("<html>not an image</html>"))
		case "/avatars/moved/v1":
			http.Redirect(w, r, "/avatars/me/v1", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	if err := os.MkdirAll(avatarDir(dir), 0700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(avatarDir(dir), avatarKey("https://old.example")+".png")
	fresh := filepath.Join(avatarDir(dir), avatarKey("https://new.example")+".png")
	foreign := filepath.Join(avatarDir(dir), "notes.txt")
	for _, path := range []string{stale, fresh, foreign} {
		if err := os.WriteFile(path, png, 0600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(foreign, old, old); err != nil {
		t.Fatal(err)
	}

	c := &Client{Config: Config{Server: server.URL, Token: "secret-token"}, HTTP: server.Client(), Dir: dir}
	s, err := c.sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(avatarDir(dir), avatarKey(server.URL+"/avatars/me/v1")+".png")
	if s.Me.ImageFile != want || s.Requests[0].ImageFile != want {
		t.Fatalf("me/request image files = %q, %q; want %q", s.Me.ImageFile, s.Requests[0].ImageFile, want)
	}
	if s.Friends[0].ImageFile != "" || s.Friends[1].ImageFile != "" || s.Blocked[0].ImageFile != "" {
		t.Fatalf("rejected avatars were cached: %+v %+v", s.Friends, s.Blocked)
	}
	if info, err := os.Stat(want); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("cached avatar mode = %v, %v", info, err)
	}
	if info, err := os.Stat(avatarDir(dir)); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("avatar directory mode = %v, %v", info, err)
	}
	var saved State
	data, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if json.Unmarshal(data, &saved) != nil || saved.Me.ImageFile != want {
		t.Fatalf("state.json does not carry the local avatar: %s", data)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale unreferenced avatar survived: %v", err)
	}
	for _, path := range []string{fresh, foreign} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was removed: %v", filepath.Base(path), err)
		}
	}
	// me/v1 once; page and moved are retried on the next sync because they failed.
	if got := avatarRequests.Load(); got != 3 {
		t.Fatalf("avatar requests = %d, want 3", got)
	}
	if _, err := c.sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := avatarRequests.Load(); got != 5 {
		t.Fatalf("avatar requests after a cached sync = %d, want 5 (no repeat for the cached image)", got)
	}

	if err := clearLocalSession(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Errorf("logout kept a cached avatar: %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("logout removed a file Pokachy did not write: %v", err)
	}
}
