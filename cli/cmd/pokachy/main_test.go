package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestHistoryEncodesCursorAndKeepsDirectionAndImage(t *testing.T) {
	cursor := "opaque+/=&cursor"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/history/friend" || r.URL.Query().Get("before") != cursor || r.Method != "GET" {
			t.Errorf("unexpected history request: %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("missing authentication")
		}
		io.WriteString(w, `{"history":[{"id":"fixture","handle":"friend","image":"https://avatars.githubusercontent.com/u/9919?v=4","created_at":123,"outgoing":1}],"next_cursor":"older"}`)
	}))
	defer server.Close()
	c := &Client{Config: Config{Server: server.URL, Token: "fixture-token"}, HTTP: server.Client()}
	page, err := c.history(context.Background(), "@friend", cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.History) != 1 || page.History[0].Outgoing != 1 || page.History[0].Image != "https://avatars.githubusercontent.com/u/9919?v=4" || page.NextCursor == nil || *page.NextCursor != "older" {
		t.Fatalf("unexpected history: %+v", page)
	}
}

func TestNotificationIconFallbackAndAsset(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	if got := notificationIcon(); got != "mail-unread" {
		t.Fatalf("fallback = %q", got)
	}
	icon := filepath.Join(dataHome, "icons", "hicolor", "scalable", "apps", "pokachy.svg")
	if err := os.MkdirAll(filepath.Dir(icon), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(icon, []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := notificationIcon(); got != icon {
		t.Fatalf("icon = %q, want %q", got, icon)
	}
}

func TestAddFragmentHintsUsesBrowserDecodableQueryEncoding(t *testing.T) {
	verification, err := url.Parse("https://pokachy.example/activate?user_code=ABCDEFGH")
	if err != nil {
		t.Fatal(err)
	}
	hints := url.Values{
		"email":  {"yam+desktop@example.com"},
		"handle": {"pokachy user"},
		"name":   {"Yam & Friends"},
	}

	got := addFragmentHints(verification, hints)
	_, encoded, found := strings.Cut(got, "#")
	if !found {
		t.Fatalf("URL has no fragment: %q", got)
	}
	decoded, err := url.ParseQuery(encoded)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range hints {
		if value := decoded.Get(key); value != want[0] {
			t.Errorf("%s = %q, want %q (URL %q)", key, value, want[0], got)
		}
	}
	if strings.Contains(got, "%2540") || strings.Contains(got, "%252B") {
		t.Fatalf("fragment hints were double escaped: %q", got)
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

func TestOnboardingResumesAuthenticatedSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/device/code" {
			t.Fatal("init should not begin a new device flow for an existing session")
		}
		if r.URL.Path != "/api/state" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"me":{"handle":"already-here"}}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	t.Setenv("POKACHY_CONFIG_DIR", dir)
	if err := save(filepath.Join(dir, "credentials.json"), Config{Server: server.URL, Token: "valid"}); err != nil {
		t.Fatal(err)
	}

	original := defaultOnboardingEnvironment
	t.Cleanup(func() { defaultOnboardingEnvironment = original })
	defaultOnboardingEnvironment = onboardingEnvironment{}

	if err := onboarding([]string{"--no-browser", "--no-desktop"}); err != nil {
		t.Fatalf("resume init: %v", err)
	}
}

func TestConfigureDesktopActivatesServiceAndUsesOmarchyConsent(t *testing.T) {
	var output strings.Builder
	var commands [][]string
	env := onboardingEnvironment{
		lookPath: func(name string) (string, error) {
			switch name {
			case "systemctl", "notify-send", "omarchy":
				return "/mock/" + name, nil
			default:
				return "", errors.New("not installed")
			}
		},
		run: func(_ context.Context, name string, args ...string) error {
			commands = append(commands, append([]string{name}, args...))
			return nil
		},
		output: &output,
	}

	configureDesktop(context.Background(), env)
	want := [][]string{
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", "--now", "pokachy.service"},
		{"omarchy", "plugin", "add", omarchyPluginURL, "--enable"},
	}
	if len(commands) != len(want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
	for i := range want {
		if strings.Join(commands[i], "\x00") != strings.Join(want[i], "\x00") {
			t.Errorf("command %d = %#v, want %#v", i, commands[i], want[i])
		}
	}
	if got := output.String(); !strings.Contains(got, "Notifications are running.") || !strings.Contains(got, "Pokachy is in your Omarchy bar.") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestConfigureDesktopHandlesMissingDesktopToolsAndDeclinedPlugin(t *testing.T) {
	t.Run("missing tools", func(t *testing.T) {
		var output strings.Builder
		env := onboardingEnvironment{
			lookPath: func(string) (string, error) { return "", errors.New("not installed") },
			run: func(context.Context, string, ...string) error {
				t.Fatal("no command should run when desktop tools are absent")
				return nil
			},
			output: &output,
		}
		configureDesktop(context.Background(), env)
		got := output.String()
		if !strings.Contains(got, "Systemd user services are unavailable") || !strings.Contains(got, "Desktop notifications need `notify-send`") {
			t.Fatalf("unexpected output: %q", got)
		}
	})

	t.Run("native Omarchy installer declined or failed", func(t *testing.T) {
		var output strings.Builder
		var commands [][]string
		env := onboardingEnvironment{
			lookPath: func(name string) (string, error) {
				if name == "omarchy" {
					return "/mock/omarchy", nil
				}
				return "", errors.New("not installed")
			},
			run: func(_ context.Context, name string, args ...string) error {
				commands = append(commands, append([]string{name}, args...))
				return errors.New("declined")
			},
			output: &output,
		}
		configureDesktop(context.Background(), env)
		if len(commands) != 1 || strings.Join(commands[0], " ") != "omarchy plugin add "+omarchyPluginURL+" --enable" {
			t.Fatalf("native plugin installer command = %#v", commands)
		}
		if !strings.Contains(output.String(), "Pokachy's Omarchy panel was not added") {
			t.Fatalf("unexpected output: %q", output.String())
		}
	})
}

func TestConfigureDesktopKeepsInstalledOmarchyPlugin(t *testing.T) {
	var output strings.Builder
	env := onboardingEnvironment{
		lookPath: func(string) (string, error) { return "/mock/tool", nil },
		run: func(_ context.Context, name string, args ...string) error {
			if name == "systemctl" {
				return nil
			}
			t.Fatalf("installed plugin should not run %s %v", name, args)
			return nil
		},
		runOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "omarchy" || strings.Join(args, " ") != "plugin list --json" {
				t.Fatalf("unexpected output command: %s %v", name, args)
			}
			return []byte(`[{"id":"com.pokachy.poke","enabled":true}]`), nil
		},
		output: &output,
	}
	configureDesktop(context.Background(), env)
	if !strings.Contains(output.String(), "already in your Omarchy bar") {
		t.Fatalf("unexpected output: %q", output.String())
	}
}

func testMaintenanceEnvironment(output io.Writer) maintenanceEnvironment {
	return maintenanceEnvironment{
		lookPath:      func(string) (string, error) { return "", errors.New("not installed") },
		run:           func(context.Context, string, ...string) error { return nil },
		runOutput:     func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("not installed") },
		input:         strings.NewReader(""),
		output:        output,
		createTemp:    os.CreateTemp,
		download:      func(context.Context, string, string) error { return nil },
		chmod:         os.Chmod,
		remove:        os.Remove,
		configDir:     configDir,
		userConfigDir: userConfigDir,
		installPrefix: installPrefix,
	}
}

func TestDoctorReportsHealthyLocalCompanion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/state" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"me":{"handle":"doctor"}}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	t.Setenv("POKACHY_CONFIG_DIR", dir)
	if err := save(filepath.Join(dir, "credentials.json"), Config{Server: server.URL, Token: "token"}); err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	env := testMaintenanceEnvironment(&output)
	env.lookPath = func(name string) (string, error) {
		switch name {
		case "systemctl", "notify-send", "omarchy":
			return "/mock/" + name, nil
		default:
			return "", errors.New("not installed")
		}
	}
	env.runOutput = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "omarchy" || strings.Join(args, " ") != "plugin list --json" {
			t.Fatalf("unexpected output command: %s %v", name, args)
		}
		return []byte(`[{"id":"com.pokachy.poke","enabled":true}]`), nil
	}
	if err := doctorWithEnvironment(context.Background(), env); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	got := output.String()
	for _, want := range []string{"Version: " + version, "Architecture: ", "Server: reachable as @doctor", "Systemd user service: enabled and running", "Notifications: notify-send available", "Omarchy plugin: installed and enabled"} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor output missing %q: %s", want, got)
		}
	}
}

func TestDoctorReturnsUsefulFailureForMissingRequirements(t *testing.T) {
	t.Setenv("POKACHY_CONFIG_DIR", t.TempDir())
	var output strings.Builder
	env := testMaintenanceEnvironment(&output)
	if err := doctorWithEnvironment(context.Background(), env); err == nil {
		t.Fatal("doctor should fail when connection and desktop requirements are missing")
	}
	got := output.String()
	for _, want := range []string{"Account: not connected", "Systemd user service: unavailable", "Notifications: `notify-send` is missing", "Omarchy: not detected"} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor output missing %q: %s", want, got)
		}
	}
}

func TestUpdateUsesTemporaryBootstrapInstallerAndRestartsIntegrations(t *testing.T) {
	tmp := t.TempDir()
	var output strings.Builder
	var commands [][]string
	var downloaded []string
	installerContents := []byte("#!/bin/sh\n")
	env := testMaintenanceEnvironment(&output)
	env.lookPath = func(name string) (string, error) {
		if name == "systemctl" || name == "omarchy" {
			return "/mock/" + name, nil
		}
		return "", errors.New("not installed")
	}
	env.run = func(_ context.Context, name string, args ...string) error {
		commands = append(commands, append([]string{name}, args...))
		return nil
	}
	env.runOutput = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "omarchy" || strings.Join(args, " ") != "plugin list --json" {
			t.Fatalf("unexpected output command: %s %v", name, args)
		}
		return []byte(`[{"id":"com.pokachy.poke","enabled":true}]`), nil
	}
	env.createTemp = func(_ string, pattern string) (*os.File, error) { return os.CreateTemp(tmp, pattern) }
	env.download = func(_ context.Context, source, destination string) error {
		downloaded = append(downloaded, source)
		contents := installerContents
		if source == bootstrapChecksumsURL {
			digest := sha256.Sum256(installerContents)
			contents = []byte(hex.EncodeToString(digest[:]) + "  install.sh\n")
		}
		return os.WriteFile(destination, contents, 0600)
	}
	if err := updateWithEnvironment(context.Background(), true, env); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got, want := strings.Join(downloaded, "\n"), bootstrapInstallerURL+"\n"+bootstrapChecksumsURL; got != want {
		t.Fatalf("downloaded %q, want %q", got, want)
	}
	want := [][]string{
		{"bash"},
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "restart", "pokachy.service"},
		{"omarchy", "plugin", "update", omarchyPluginID, "--yes"},
	}
	if len(commands) != len(want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
	for i, command := range commands {
		if command[0] != want[i][0] || (i > 0 && strings.Join(command[1:], "\x00") != strings.Join(want[i][1:], "\x00")) {
			t.Errorf("command %d = %#v, want %#v", i, command, want[i])
		}
	}
	if !strings.Contains(output.String(), "preserving your local session") {
		t.Fatalf("unexpected output: %q", output.String())
	}
}

func TestUpdateRefusesInstallerWithWrongReleaseChecksum(t *testing.T) {
	tmp := t.TempDir()
	var output strings.Builder
	env := testMaintenanceEnvironment(&output)
	env.createTemp = func(_ string, pattern string) (*os.File, error) { return os.CreateTemp(tmp, pattern) }
	env.download = func(_ context.Context, source, destination string) error {
		contents := []byte("#!/bin/sh\necho unsafe\n")
		if source == bootstrapChecksumsURL {
			contents = []byte(strings.Repeat("0", 64) + "  install.sh\n")
		}
		return os.WriteFile(destination, contents, 0600)
	}
	if err := updateWithEnvironment(context.Background(), true, env); err == nil || !strings.Contains(err.Error(), "checksum verification failed") {
		t.Fatalf("update error = %v, want checksum rejection", err)
	}
}

func TestUninstallRemovesOnlyKnownLocalFilesAndKeepsAccount(t *testing.T) {
	tmp := t.TempDir()
	var output strings.Builder
	var commands [][]string
	var removed []string
	env := testMaintenanceEnvironment(&output)
	env.lookPath = func(name string) (string, error) {
		if name == "systemctl" || name == "omarchy" {
			return "/mock/" + name, nil
		}
		return "", errors.New("not installed")
	}
	env.run = func(_ context.Context, name string, args ...string) error {
		commands = append(commands, append([]string{name}, args...))
		return nil
	}
	env.runOutput = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte(`[{"id":"com.pokachy.poke","enabled":true}]`), nil
	}
	env.remove = func(path string) error {
		removed = append(removed, path)
		return nil
	}
	env.configDir = func() string { return filepath.Join(tmp, "pokachy") }
	env.userConfigDir = func() string { return filepath.Join(tmp, "config") }
	env.installPrefix = func() string { return filepath.Join(tmp, "custom-prefix") }
	if err := uninstallWithEnvironment(context.Background(), true, env); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	wantCommands := [][]string{
		{"omarchy", "plugin", "remove", omarchyPluginID, "--yes"},
		{"systemctl", "--user", "disable", "--now", "pokachy.service"},
		{"systemctl", "--user", "daemon-reload"},
	}
	if len(commands) != len(wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}
	for i := range wantCommands {
		if strings.Join(commands[i], "\x00") != strings.Join(wantCommands[i], "\x00") {
			t.Errorf("command %d = %#v, want %#v", i, commands[i], wantCommands[i])
		}
	}
	if got, want := strings.Join(removed, "\n"), strings.Join(knownPokachyPaths(env), "\n"); got != want {
		t.Errorf("removed paths = %q, want only %q", got, want)
	}
	if !strings.Contains(output.String(), "does not delete your account") || !strings.Contains(output.String(), "account still exists") {
		t.Fatalf("unexpected output: %q", output.String())
	}
}

func TestUninstallNeverTreatsSharedConfigDirectoryAsPokachyData(t *testing.T) {
	tmp := t.TempDir()
	var output strings.Builder
	env := testMaintenanceEnvironment(&output)
	env.configDir = func() string { return tmp }
	env.userConfigDir = func() string { return filepath.Join(tmp, "config") }
	env.installPrefix = func() string { return filepath.Join(tmp, "prefix") }
	for _, path := range knownPokachyPaths(env) {
		if path == tmp || strings.HasPrefix(path, tmp+string(filepath.Separator)+"credentials.json") {
			t.Fatalf("unsafe config cleanup target: %s", path)
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
