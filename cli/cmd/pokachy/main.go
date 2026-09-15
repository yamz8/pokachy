package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/coder/websocket"
)

const version = "0.1.8"

const omarchyPluginURL = "https://github.com/yamz8/pokachy-omarchy.git"
const bootstrapInstallerURL = "https://github.com/yamz8/pokachy/releases/latest/download/install.sh"
const bootstrapChecksumsURL = "https://github.com/yamz8/pokachy/releases/latest/download/SHA256SUMS"
const omarchyPluginID = "com.pokachy.poke"

type Config struct {
	Server string `json:"server"`
	Token  string `json:"token"`
}
type Person struct {
	Handle   string `json:"handle"`
	Name     string `json:"name"`
	Image    string `json:"image"`
	Outgoing int    `json:"outgoing"`
	Waiting  int    `json:"waiting"`
}
type Poke struct {
	ID        string `json:"id"`
	Handle    string `json:"handle"`
	Name      string `json:"name"`
	Image     string `json:"image"`
	CreatedAt int64  `json:"created_at"`
	Outgoing  int    `json:"outgoing"`
}
type Me struct {
	ID     string `json:"id"`
	Handle string `json:"handle"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Image  string `json:"image"`
	Quiet  bool   `json:"quiet"`
}
type State struct {
	Me         Me       `json:"me"`
	Friends    []Person `json:"friends"`
	Requests   []Person `json:"requests"`
	Inbox      []Poke   `json:"inbox"`
	History    []Poke   `json:"history"`
	Blocked    []Person `json:"blocked"`
	Received   int      `json:"received"`
	Online     bool     `json:"online"`
	SyncedAt   int64    `json:"synced_at"`
	NeedsLogin bool     `json:"needsLogin"`
}
type HistoryPage struct {
	History    []Poke  `json:"history"`
	NextCursor *string `json:"next_cursor"`
}
type Client struct {
	Config Config
	HTTP   *http.Client
	Dir    string
}
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return e.Message }
func safe(s string) string {
	return strings.Map(func(r rune) rune {
		// Escape, bidi, and other format characters can change how a terminal
		// renders surrounding text. Names are remote input, so keep output plain.
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}
func configDir() string {
	if p := os.Getenv("POKACHY_CONFIG_DIR"); p != "" {
		return p
	}
	p := os.Getenv("XDG_CONFIG_HOME")
	if p == "" {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, ".config")
	}
	return filepath.Join(p, "pokachy")
}
func save(path string, value any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pokachy-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}
func loadClient() (*Client, error) {
	dir := configDir()
	data, err := os.ReadFile(filepath.Join(dir, "credentials.json"))
	if err != nil {
		return nil, errors.New("run 'pokachy init' to connect this computer")
	}
	var cfg Config
	if err = json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if err = validServer(cfg.Server); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("stored device session is invalid; run 'pokachy init' again")
	}
	return &Client{Config: cfg, HTTP: &http.Client{Timeout: 20 * time.Second}, Dir: dir}, nil
}
func validServer(raw string) error {
	_, err := serverURL(raw)
	return err
}

// serverURL accepts an origin only. HTTP is deliberately limited to loopback
// so a device token can never be configured for a network HTTP server.
func serverURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.RawPath != "" || u.Opaque != "" {
		return nil, errors.New("server must be an HTTPS origin without a path")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	local := host == "localhost"
	if ip := net.ParseIP(host); ip != nil {
		local = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(local && u.Scheme == "http") {
		return nil, errors.New("HTTPS is required except on localhost")
	}
	return u, nil
}

func sameOrigin(a, b *url.URL) bool {
	if !strings.EqualFold(a.Scheme, b.Scheme) || !strings.EqualFold(a.Hostname(), b.Hostname()) {
		return false
	}
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if strings.EqualFold(u.Scheme, "https") {
			return "443"
		}
		return "80"
	}
	return port(a) == port(b)
}
func (c *Client) request(ctx context.Context, method, path string, body any, output any, key string) error {
	base, err := serverURL(c.Config.Server)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return errors.New("invalid API path")
	}
	relative, err := url.Parse(path)
	if err != nil || relative.IsAbs() || relative.Host != "" || relative.Fragment != "" {
		return errors.New("invalid API path")
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(data))
	}
	target := base.ResolveReference(relative)
	// Query values are encoded by callers; absolute origins and fragments are rejected.
	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Pokachy-Client", "cli")
	req.Header.Set("User-Agent", "Pokachy/"+version)
	if c.Config.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Config.Token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	hc := *c.HTTP
	// Do not follow a redirect: a redirect target is not part of the configured
	// trusted origin and must never receive a bearer token or device request.
	hc.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := hc.Do(req)
	if err != nil {
		category := "network error"
		switch {
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			category = "connection closed"
		case errors.Is(err, context.Canceled):
			category = "request canceled"
		case errors.Is(err, context.DeadlineExceeded):
			category = "request timed out"
		case errors.Is(err, syscall.ECONNREFUSED):
			category = "connection refused"
		case errors.Is(err, syscall.ECONNRESET):
			category = "connection reset"
		}
		return fmt.Errorf("could not reach Pokachy (%s); check your connection", category)
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return errors.New("Pokachy returned an unexpected redirect")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode >= 400 {
		var m map[string]json.RawMessage
		_ = json.Unmarshal(data, &m)
		var msg string
		_ = json.Unmarshal(m["error"], &msg)
		if msg == "" {
			_ = json.Unmarshal(m["message"], &msg)
		}
		if msg == "" {
			msg = http.StatusText(response.StatusCode)
		}
		return &APIError{response.StatusCode, safe(msg)}
	}
	if output != nil {
		return json.Unmarshal(data, output)
	}
	return nil
}
func (c *Client) sync(ctx context.Context) (State, error) {
	var s State
	err := c.request(ctx, "GET", "/api/state", nil, &s, "")
	if err != nil {
		return s, err
	}
	s.Online = true
	s.SyncedAt = time.Now().Unix()
	err = save(filepath.Join(c.Dir, "state.json"), s)
	return s, err
}
func requestKey() string { b := make([]byte, 16); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func (c *Client) poke(ctx context.Context, handle string) error {
	return c.request(ctx, "POST", "/api/pokes/"+url.PathEscape(strings.TrimPrefix(handle, "@")), map[string]any{}, nil, requestKey())
}
func (c *Client) history(ctx context.Context, handle, before string) (HistoryPage, error) {
	path := "/api/history/" + url.PathEscape(strings.TrimPrefix(handle, "@"))
	if before != "" {
		path += "?" + url.Values{"before": {before}}.Encode()
	}
	var page HistoryPage
	if err := c.request(ctx, "GET", path, nil, &page, ""); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound && apiErr.Message == "Not found" {
			return page, errors.New("Older history needs a server update. Recent cached pokes are still shown.")
		}
		return page, err
	}
	return page, nil
}

func notificationIcon() string {
	if executable, err := os.Executable(); err == nil {
		icon := filepath.Join(filepath.Dir(executable), "..", "share", "icons", "hicolor", "scalable", "apps", "pokachy.svg")
		if info, err := os.Stat(icon); err == nil && info.Mode().IsRegular() {
			return icon
		}
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dataHome = filepath.Join(home, ".local", "share")
		}
	}
	icon := filepath.Join(dataHome, "icons", "hicolor", "scalable", "apps", "pokachy.svg")
	if info, err := os.Stat(icon); err == nil && info.Mode().IsRegular() {
		return icon
	}
	return "mail-unread"
}

func clearLocalSession(dir string) error {
	for _, name := range []string{"credentials.json", "state.json", "notified.json"} {
		err := os.Remove(filepath.Join(dir, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
func outputJSON(v any) { b, _ := json.Marshal(v); fmt.Println(string(b)) }
func help() {
	fmt.Print(`Pokachy — a little nudge for your Linux friends

  pokachy init [--server https://pokachy.com] [--no-browser] [--no-desktop]
  pokachy poke @friend
  pokachy friends [add|accept|remove] @friend
  pokachy inbox
  pokachy history @friend [--before CURSOR] [--json]
  pokachy dismiss POKE_ID
  pokachy quiet on|off
  pokachy block @person | unblock @person
  pokachy report @person "reason"
  pokachy profile HANDLE
  pokachy account                 Open account and device settings
  pokachy status [--json]         Read the local companion status
  pokachy watch --json            Stream local status changes
  pokachy daemon                 Receive desktop notifications
  pokachy omarchy install        Install or repair the Omarchy bar panel
  pokachy doctor                 Check local companion health
  pokachy update [--yes]         Update this computer from the public installer
  pokachy uninstall [--yes]      Remove this computer (does not delete your account)
  pokachy logout
  pokachy version

Add --json for machine-readable output. Install/start the companion
service with the repository's scripts/install.sh after building.
`)
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "pokachy:", safe(err.Error()))
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		help()
		return nil
	}
	if args[0] == "version" {
		fmt.Println(version)
		return nil
	}
	asJSON := slices.Contains(args, "--json")
	args = slices.DeleteFunc(args, func(s string) bool { return s == "--json" })
	if len(args) == 0 {
		return errors.New("provide a command")
	}
	if args[0] == "init" {
		return onboarding(args[1:])
	}
	if args[0] == "omarchy" {
		return omarchy(args[1:])
	}
	if args[0] == "doctor" {
		return doctor()
	}
	if args[0] == "update" {
		return update(args[1:])
	}
	if args[0] == "uninstall" {
		return uninstall(args[1:])
	}
	if args[0] == "status" || args[0] == "watch" {
		return status(args[0] == "watch", asJSON)
	}
	c, err := loadClient()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if args[0] == "daemon" {
		return c.daemon(ctx)
	}
	need := func(n int) error {
		if len(args) < n {
			return errors.New("missing argument; run 'pokachy help'")
		}
		return nil
	}
	path := ""
	method := "POST"
	body := any(map[string]any{})
	switch args[0] {
	case "poke":
		if err = need(2); err != nil {
			return err
		}
		if err = c.poke(ctx, args[1]); err != nil {
			return err
		}
	case "friends":
		if len(args) == 1 {
			var s State
			s, err = c.sync(ctx)
			if err != nil {
				return err
			}
			if asJSON {
				outputJSON(s)
				return nil
			}
			for _, p := range s.Friends {
				suffix := ""
				if p.Waiting == 1 {
					suffix = " · waiting for a poke back"
				}
				fmt.Printf("@%s%s\n", safe(p.Handle), suffix)
			}
			for _, p := range s.Requests {
				direction := "incoming"
				if p.Outgoing == 1 {
					direction = "sent"
				}
				fmt.Printf("@%s · %s friend request\n", safe(p.Handle), direction)
			}
			if len(s.Friends)+len(s.Requests) == 0 {
				fmt.Println("Your corner is quiet. Add someone: pokachy friends add @handle")
			}
			return nil
		}
		if err = need(3); err != nil {
			return err
		}
		path = "/api/friends/" + url.PathEscape(strings.TrimPrefix(args[2], "@"))
		switch args[1] {
		case "add":
		case "accept":
			path += "/accept"
		case "remove":
			method = "DELETE"
		default:
			return errors.New("use friends add, accept, or remove")
		}
	case "inbox":
		s, e := c.sync(ctx)
		if e != nil {
			return e
		}
		if asJSON {
			outputJSON(s.Inbox)
		} else {
			for _, p := range s.Inbox {
				fmt.Printf("@%s poked you · %s\n  pokachy poke @%s  |  pokachy dismiss %s\n", safe(p.Handle), time.UnixMilli(p.CreatedAt).Local().Format("Jan 2 15:04"), safe(p.Handle), safe(p.ID))
			}
			if len(s.Inbox) == 0 {
				fmt.Println("All caught up. A little nudge will find you here.")
			}
		}
		return nil
	case "history":
		if err = need(2); err != nil {
			return err
		}
		before := ""
		for i := 2; i < len(args); i++ {
			if args[i] == "--before" && i+1 < len(args) {
				before = args[i+1]
				i++
			} else if strings.HasPrefix(args[i], "--before=") {
				before = strings.TrimPrefix(args[i], "--before=")
			} else {
				return errors.New("use history @friend [--before CURSOR]")
			}
		}
		page, e := c.history(ctx, args[1], before)
		if e != nil {
			return e
		}
		if asJSON {
			outputJSON(page)
		} else {
			for _, p := range page.History {
				verb := "poked you"
				if p.Outgoing == 1 {
					verb = "You poked"
				}
				fmt.Printf("%s · %s\n", verb, time.UnixMilli(p.CreatedAt).Local().Format("Jan 2 15:04"))
			}
			if page.NextCursor != nil {
				fmt.Printf("Older history: pokachy history %s --before %s\n", safe(args[1]), safe(*page.NextCursor))
			}
		}
		return nil
	case "dismiss":
		if err = need(2); err != nil {
			return err
		}
		path = "/api/inbox/" + url.PathEscape(args[1]) + "/dismiss"
	case "quiet":
		if err = need(2); err != nil {
			return err
		}
		if args[1] != "on" && args[1] != "off" {
			return errors.New("use quiet on or quiet off")
		}
		path = "/api/profile"
		method = "PUT"
		body = map[string]any{"quiet": args[1] == "on"}
	case "profile":
		if err = need(2); err != nil {
			return err
		}
		path = "/api/profile"
		method = "PUT"
		body = map[string]any{"handle": strings.TrimPrefix(args[1], "@")}
	case "block", "unblock":
		if err = need(2); err != nil {
			return err
		}
		path = "/api/blocks/" + url.PathEscape(strings.TrimPrefix(args[1], "@"))
		if args[0] == "unblock" {
			method = "DELETE"
		}
	case "report":
		if err = need(3); err != nil {
			return err
		}
		path = "/api/reports/" + url.PathEscape(strings.TrimPrefix(args[1], "@"))
		body = map[string]any{"reason": strings.Join(args[2:], " ")}
	case "account":
		return openBrowser(c.Config.Server + "/account")
	case "logout":
		if err = c.request(ctx, "POST", "/api/auth/sign-out", body, nil, ""); err != nil {
			var apiErr *APIError
			if !errors.As(err, &apiErr) || (apiErr.Status != http.StatusUnauthorized && apiErr.Status != http.StatusForbidden) {
				return err
			}
		}
		if err = clearLocalSession(c.Dir); err != nil {
			return err
		}
		fmt.Println("Signed out. Local device session cleared.")
		return nil
	default:
		return errors.New("unknown command; run 'pokachy help'")
	}
	if path != "" {
		if err = c.request(ctx, method, path, body, nil, ""); err != nil {
			return err
		}
	}
	if _, syncErr := c.sync(ctx); syncErr != nil {
		fmt.Fprintln(os.Stderr, "Pokachy completed the request but could not refresh local status:", safe(syncErr.Error()))
	}
	if asJSON {
		outputJSON(map[string]bool{"ok": true})
	} else {
		if args[0] == "poke" {
			fmt.Printf("A little nudge sent to %s.\n", safe(args[1]))
		} else {
			fmt.Println("Done.")
		}
	}
	return nil
}
func openBrowser(target string) error {
	if err := exec.Command("xdg-open", target).Start(); err != nil {
		return fmt.Errorf("open this address in your browser: %s", target)
	}
	return nil
}

// onboardingEnvironment keeps desktop integration separate from account
// activation. It makes the best-effort local setup easy to exercise without
// starting a service or changing a user's desktop during tests.
type onboardingEnvironment struct {
	lookPath  func(string) (string, error)
	run       func(context.Context, string, ...string) error
	runOutput func(context.Context, string, ...string) ([]byte, error)
	output    io.Writer
}

func runInteractiveCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	// Omarchy deliberately asks the user to approve third-party plugin code.
	// Preserve that interaction rather than using a non-interactive flag.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func installOmarchyPlugin(ctx context.Context, env onboardingEnvironment) error {
	if _, err := env.lookPath("omarchy"); err != nil {
		return errors.New("Omarchy is not installed or is not available on PATH")
	}
	if env.runOutput != nil {
		if data, err := env.runOutput(ctx, "omarchy", "plugin", "list", "--json"); err == nil {
			var plugins []omarchyPlugin
			if json.Unmarshal(data, &plugins) == nil {
				for _, plugin := range plugins {
					if plugin.ID != omarchyPluginID {
						continue
					}
					if plugin.Enabled {
						fmt.Fprintln(env.output, "Pokachy is already in your Omarchy bar.")
						return nil
					}
					if err := env.run(ctx, "omarchy", "plugin", "enable", omarchyPluginID); err != nil {
						return fmt.Errorf("could not enable Pokachy's Omarchy panel; try `omarchy plugin enable %s`: %w", omarchyPluginID, err)
					}
					fmt.Fprintln(env.output, "Pokachy is in your Omarchy bar.")
					return nil
				}
			}
		}
	}
	fmt.Fprintln(env.output, "Omarchy detected. Its plugin installer will ask whether to add Pokachy to your bar.")
	if err := env.run(ctx, "omarchy", "plugin", "add", omarchyPluginURL, "--enable"); err != nil {
		return fmt.Errorf("Pokachy's Omarchy panel was not added; try `omarchy plugin add %s --enable`: %w", omarchyPluginURL, err)
	}
	fmt.Fprintln(env.output, "Pokachy is in your Omarchy bar.")
	return nil
}

func omarchy(args []string) error {
	if len(args) != 1 || args[0] != "install" {
		return errors.New("use 'pokachy omarchy install'")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return installOmarchyPlugin(ctx, defaultOnboardingEnvironment)
}

var defaultOnboardingEnvironment = onboardingEnvironment{
	lookPath:  exec.LookPath,
	run:       runInteractiveCommand,
	runOutput: runCommandOutput,
	output:    os.Stdout,
}

// configureDesktop is deliberately best effort: a connected account remains
// useful even if the host has no systemd, notifications, or Omarchy.
func configureDesktop(ctx context.Context, env onboardingEnvironment) {
	if _, err := env.lookPath("systemctl"); err != nil {
		fmt.Fprintln(env.output, "Systemd user services are unavailable. Run `pokachy daemon` in a terminal to receive notifications.")
	} else if err := env.run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		fmt.Fprintln(env.output, "Could not reload systemd user services. Run `systemctl --user daemon-reload` after fixing your user systemd session.")
	} else if err := env.run(ctx, "systemctl", "--user", "enable", "--now", "pokachy.service"); err != nil {
		fmt.Fprintln(env.output, "Could not start Pokachy notifications. Run `systemctl --user enable --now pokachy.service` after checking the installed service.")
	} else {
		fmt.Fprintln(env.output, "Notifications are running.")
	}

	if _, err := env.lookPath("notify-send"); err != nil {
		fmt.Fprintln(env.output, "Desktop notifications need `notify-send` (libnotify) and a compatible notification service.")
	}

	if _, err := env.lookPath("omarchy"); err == nil {
		if err := installOmarchyPlugin(ctx, env); err != nil {
			fmt.Fprintln(env.output, safe(err.Error()))
		}
	}
}

func completeOnboarding(ctx context.Context, handle string, resumed bool) {
	if resumed {
		fmt.Printf("Already connected as @%s. Checking desktop integration...\n", safe(handle))
	} else {
		fmt.Printf("Connected as @%s. Checking desktop integration...\n", safe(handle))
	}
	configureDesktop(ctx, defaultOnboardingEnvironment)
	fmt.Println("Next: pokachy friends add @friend")
}

func gitValue(key string) string {
	out, err := exec.Command("git", "config", "--global", "--get", key).Output()
	if err != nil {
		return ""
	}
	return safe(strings.TrimSpace(string(out)))
}

func addFragmentHints(verification *url.URL, hints url.Values) string {
	base := *verification
	base.Fragment = ""
	base.RawFragment = ""
	if encoded := hints.Encode(); encoded != "" {
		return base.String() + "#" + encoded
	}
	return base.String()
}

func onboarding(args []string) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	server := flags.String("server", "https://pokachy.com", "Pokachy server origin")
	noBrowser := flags.Bool("no-browser", false, "Print the URL instead of opening it")
	noDesktop := flags.Bool("no-desktop", false, "Skip systemd, notification, and Omarchy setup")
	if err := flags.Parse(args); err != nil {
		return err
	}
	*server = strings.TrimRight(*server, "/")
	if err := validServer(*server); err != nil {
		return err
	}
	credentialsPath := filepath.Join(configDir(), "credentials.json")
	if _, err := os.Stat(credentialsPath); err == nil {
		c, err := loadClient()
		if err != nil {
			return fmt.Errorf("could not resume the stored device session: %w", err)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		s, err := c.sync(ctx)
		if err != nil {
			return fmt.Errorf("could not verify the stored device session; run 'pokachy logout' before connecting another account: %w", err)
		}
		if *noDesktop {
			fmt.Printf("Already connected as @%s. Desktop integration skipped.\n", safe(s.Me.Handle))
			fmt.Println("Next: pokachy friends add @friend")
		} else {
			completeOnboarding(ctx, s.Me.Handle, true)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	c := &Client{Config: Config{Server: *server}, HTTP: &http.Client{Timeout: 20 * time.Second}, Dir: configDir()}
	name, email := gitValue("user.name"), gitValue("user.email")
	handle := safe(os.Getenv("USER"))
	hints := url.Values{}
	if name != "" || email != "" {
		fmt.Printf("Suggested from your Omarchy/Git setup:\n  Name: %s\n  Email: %s\n  Handle: @%s\nUse these as editable signup suggestions? [Y/n] ", name, email, handle)
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
			if answer == "" || answer == "y" || answer == "yes" {
				hints.Set("name", name)
				hints.Set("email", email)
				hints.Set("handle", handle)
			}
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var codes struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
		URL        string `json:"verification_uri_complete"`
		Interval   int    `json:"interval"`
		ExpiresIn  int    `json:"expires_in"`
	}
	if err := c.request(ctx, "POST", "/api/auth/device/code", map[string]string{"client_id": "pokachy-cli"}, &codes, ""); err != nil {
		return err
	}
	trusted, err := serverURL(*server)
	if err != nil {
		return err
	}
	verification, err := url.Parse(codes.URL)
	if err != nil || !verification.IsAbs() || verification.User != nil || !sameOrigin(trusted, verification) || verification.Path != "/activate" {
		return errors.New("server returned an unexpected verification URL")
	}
	verificationURL := addFragmentHints(verification, hints)
	fmt.Printf("\nYour device code: %s\nSign in with email or GitHub, then approve this code:\n%s\n", safe(codes.UserCode), safe(verificationURL))
	if !*noBrowser {
		_ = openBrowser(verificationURL)
	}
	interval := time.Duration(max(codes.Interval, 5)) * time.Second
	deadline := time.Now().Add(time.Duration(min(codes.ExpiresIn, 900)) * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		// A device-token POST is deliberately not retried because redemption is
		// one-time. Avoid racing an intermediary that closes the connection at
		// the same idle interval by starting each attempt on a fresh connection.
		c.HTTP.CloseIdleConnections()
		var auth struct {
			Token string `json:"access_token"`
		}
		err := c.request(ctx, "POST", "/api/auth/device/token", map[string]string{"client_id": "pokachy-cli", "device_code": codes.DeviceCode, "grant_type": "urn:ietf:params:oauth:grant-type:device_code"}, &auth, "")
		if err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) {
				if strings.Contains(apiErr.Message, "authorization_pending") {
					continue
				}
				if strings.Contains(apiErr.Message, "slow_down") {
					interval += 5 * time.Second
					continue
				}
			}
			return err
		}
		if auth.Token == "" {
			return errors.New("server returned no device session")
		}
		c.Config.Token = auth.Token
		if err = save(filepath.Join(c.Dir, "credentials.json"), c.Config); err != nil {
			return err
		}
		s, err := c.sync(ctx)
		if err != nil {
			return err
		}
		fmt.Println()
		if *noDesktop {
			fmt.Printf("Connected as @%s. Desktop integration skipped.\n", safe(s.Me.Handle))
			fmt.Println("Next: pokachy friends add @friend")
		} else {
			completeOnboarding(ctx, s.Me.Handle, false)
		}
		return nil
	}
	return errors.New("device request expired; run pokachy init again")
}

type maintenanceEnvironment struct {
	lookPath      func(string) (string, error)
	run           func(context.Context, string, ...string) error
	runOutput     func(context.Context, string, ...string) ([]byte, error)
	input         io.Reader
	output        io.Writer
	createTemp    func(string, string) (*os.File, error)
	download      func(context.Context, string, string) error
	chmod         func(string, os.FileMode) error
	remove        func(string) error
	configDir     func() string
	userConfigDir func() string
	installPrefix func() string
}

func runCommandOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func userConfigDir() string {
	if p := os.Getenv("XDG_CONFIG_HOME"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config")
}

func installPrefix() string {
	if p := os.Getenv("POKACHY_INSTALL_PREFIX"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local")
}

func downloadBootstrapInstaller(ctx context.Context, source, destination string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 || request.URL.Scheme != "https" {
			return errors.New("unsafe installer redirect")
		}
		allowed := map[string]bool{
			"github.com": true, "objects.githubusercontent.com": true, "release-assets.githubusercontent.com": true,
		}
		if !allowed[strings.ToLower(request.URL.Hostname())] {
			return errors.New("installer redirected to an untrusted host")
		}
		return nil
	}}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("could not download the Pokachy installer: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("could not download the Pokachy installer: unexpected HTTP status %d", response.StatusCode)
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	limited := &io.LimitedReader{R: response.Body, N: (1 << 20) + 1}
	written, err := io.Copy(file, limited)
	if err != nil {
		file.Close()
		return err
	}
	if written > 1<<20 {
		file.Close()
		return errors.New("downloaded Pokachy installer is unexpectedly large")
	}
	return file.Close()
}

var defaultMaintenanceEnvironment = maintenanceEnvironment{
	lookPath:      exec.LookPath,
	run:           runInteractiveCommand,
	runOutput:     runCommandOutput,
	input:         os.Stdin,
	output:        os.Stdout,
	createTemp:    os.CreateTemp,
	download:      downloadBootstrapInstaller,
	chmod:         os.Chmod,
	remove:        os.Remove,
	configDir:     configDir,
	userConfigDir: userConfigDir,
	installPrefix: installPrefix,
}

func verifyReleaseFile(path, checksumsPath, name string) error {
	checksums, err := os.ReadFile(checksumsPath)
	if err != nil {
		return err
	}
	wanted := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			if wanted != "" {
				return fmt.Errorf("release checksums list %s more than once", name)
			}
			wanted = strings.ToLower(fields[0])
		}
	}
	if len(wanted) != sha256.Size*2 {
		return fmt.Errorf("release checksums do not contain a valid %s digest", name)
	}
	if _, err := hex.DecodeString(wanted); err != nil {
		return fmt.Errorf("release checksum for %s is invalid", name)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	actual := sha256.Sum256(contents)
	if hex.EncodeToString(actual[:]) != wanted {
		return fmt.Errorf("release checksum verification failed for %s", name)
	}
	return nil
}

type omarchyPlugin struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

// omarchyPluginStatus only asks Omarchy's plugin manager. It never reads or
// edits Omarchy configuration directly, so it is safe for doctor and gives
// update/uninstall a conservative answer when the shell is not running.
func omarchyPluginStatus(ctx context.Context, env maintenanceEnvironment) (installed, enabled, known bool) {
	if _, err := env.lookPath("omarchy"); err != nil {
		return false, false, true
	}
	data, err := env.runOutput(ctx, "omarchy", "plugin", "list", "--json")
	if err != nil {
		return false, false, false
	}
	var plugins []omarchyPlugin
	if err := json.Unmarshal(data, &plugins); err != nil {
		return false, false, false
	}
	for _, plugin := range plugins {
		if plugin.ID == omarchyPluginID {
			return true, plugin.Enabled, true
		}
	}
	return false, false, true
}

func doctor() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return doctorWithEnvironment(ctx, defaultMaintenanceEnvironment)
}

// doctorWithEnvironment is intentionally read-only. It avoids Client.sync,
// which updates the local cache, and instead makes one authenticated state
// request only when local credentials are valid.
func doctorWithEnvironment(ctx context.Context, env maintenanceEnvironment) error {
	issues := false
	fmt.Fprintln(env.output, "Pokachy doctor")
	fmt.Fprintf(env.output, "Version: %s\nArchitecture: %s/%s\nConfig: %s\n", version, runtime.GOOS, runtime.GOARCH, env.configDir())

	if c, err := loadClient(); err != nil {
		issues = true
		fmt.Fprintln(env.output, "Account: not connected — run `pokachy init`")
	} else {
		var state State
		if err := c.request(ctx, http.MethodGet, "/api/state", nil, &state, ""); err != nil {
			issues = true
			fmt.Fprintln(env.output, "Server: unreachable — "+safe(err.Error()))
		} else if state.Me.Handle != "" {
			fmt.Fprintln(env.output, "Account: connected\nServer: reachable as @"+safe(state.Me.Handle))
		} else {
			fmt.Fprintln(env.output, "Account: connected\nServer: reachable")
		}
	}

	if _, err := env.lookPath("systemctl"); err != nil {
		issues = true
		fmt.Fprintln(env.output, "Systemd user service: unavailable — run `pokachy daemon` in a terminal instead")
	} else {
		enabled := env.run(ctx, "systemctl", "--user", "is-enabled", "--quiet", "pokachy.service") == nil
		active := env.run(ctx, "systemctl", "--user", "is-active", "--quiet", "pokachy.service") == nil
		if enabled && active {
			fmt.Fprintln(env.output, "Systemd user service: enabled and running")
		} else {
			issues = true
			fmt.Fprintln(env.output, "Systemd user service: needs attention — run `systemctl --user enable --now pokachy.service`")
		}
	}

	if _, err := env.lookPath("notify-send"); err != nil {
		issues = true
		fmt.Fprintln(env.output, "Notifications: `notify-send` is missing (install libnotify)")
	} else {
		fmt.Fprintln(env.output, "Notifications: notify-send available")
	}

	if _, err := env.lookPath("omarchy"); err != nil {
		fmt.Fprintln(env.output, "Omarchy: not detected")
	} else {
		fmt.Fprintln(env.output, "Omarchy: detected")
		installed, enabled, known := omarchyPluginStatus(ctx, env)
		switch {
		case !known:
			fmt.Fprintln(env.output, "Omarchy plugin: state unavailable — start Omarchy shell, then run `omarchy plugin list --json`")
		case !installed:
			fmt.Fprintln(env.output, "Omarchy plugin: not installed — run `pokachy omarchy install`")
		case !enabled:
			issues = true
			fmt.Fprintln(env.output, "Omarchy plugin: installed but disabled — run `omarchy plugin enable "+omarchyPluginID+"`")
		default:
			fmt.Fprintln(env.output, "Omarchy plugin: installed and enabled")
		}
	}

	if issues {
		return errors.New("doctor found checks that need attention")
	}
	return nil
}

func confirmDefaultNo(input io.Reader, output io.Writer, prompt string) bool {
	fmt.Fprint(output, prompt)
	scanner := bufio.NewScanner(input)
	if !scanner.Scan() {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes"
}

func update(args []string) error {
	flags := flag.NewFlagSet("update", flag.ContinueOnError)
	yes := flags.Bool("yes", false, "Run the reviewed installer without another prompt")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("use update [--yes]")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return updateWithEnvironment(ctx, *yes, defaultMaintenanceEnvironment)
}

func updateWithEnvironment(ctx context.Context, yes bool, env maintenanceEnvironment) error {
	if !yes && !confirmDefaultNo(env.input, env.output, "Update Pokachy on this computer using "+bootstrapInstallerURL+"? [y/N] ") {
		fmt.Fprintln(env.output, "Update cancelled.")
		return nil
	}
	installer, err := env.createTemp("", "pokachy-install-*")
	if err != nil {
		return err
	}
	installerPath := installer.Name()
	if err := installer.Close(); err != nil {
		env.remove(installerPath)
		return err
	}
	defer env.remove(installerPath)
	checksums, err := env.createTemp("", "pokachy-checksums-*")
	if err != nil {
		return err
	}
	checksumsPath := checksums.Name()
	if err := checksums.Close(); err != nil {
		env.remove(checksumsPath)
		return err
	}
	defer env.remove(checksumsPath)
	fmt.Fprintln(env.output, "Downloading and verifying the public installer before execution.")
	if err := env.download(ctx, bootstrapInstallerURL, installerPath); err != nil {
		return err
	}
	if err := env.download(ctx, bootstrapChecksumsURL, checksumsPath); err != nil {
		return err
	}
	if err := verifyReleaseFile(installerPath, checksumsPath, "install.sh"); err != nil {
		return err
	}
	if err := env.chmod(installerPath, 0700); err != nil {
		return err
	}
	if err := env.run(ctx, "bash", installerPath, "--update"); err != nil {
		return fmt.Errorf("Pokachy installer failed: %w", err)
	}
	fmt.Fprintln(env.output, "Pokachy files updated; preserving your local session.")
	if _, err := env.lookPath("systemctl"); err == nil {
		if err := env.run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			fmt.Fprintln(env.output, "Could not reload systemd user services; restart Pokachy manually after fixing systemd.")
		} else if err := env.run(ctx, "systemctl", "--user", "restart", "pokachy.service"); err != nil {
			fmt.Fprintln(env.output, "Could not restart Pokachy notifications; run `systemctl --user restart pokachy.service` after checking the service.")
		} else {
			fmt.Fprintln(env.output, "Notifications restarted.")
		}
	}
	installed, _, known := omarchyPluginStatus(ctx, env)
	if known && installed {
		args := []string{"plugin", "update", omarchyPluginID}
		if yes {
			args = append(args, "--yes")
		}
		if err := env.run(ctx, "omarchy", args...); err != nil {
			fmt.Fprintln(env.output, "Omarchy could not update the Pokachy bar plugin; try `omarchy plugin update "+omarchyPluginID+"`.")
		} else {
			fmt.Fprintln(env.output, "Omarchy bar plugin updated.")
		}
	}
	return nil
}

func uninstall(args []string) error {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	yes := flags.Bool("yes", false, "Remove local Pokachy files without another prompt")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("use uninstall [--yes]")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return uninstallWithEnvironment(ctx, *yes, defaultMaintenanceEnvironment)
}

func knownPokachyPaths(env maintenanceEnvironment) []string {
	paths := []string{}
	appendPath := func(root string, parts ...string) {
		cleanRoot := filepath.Clean(root)
		// Never turn a missing, relative, or root-level XDG value into a
		// destructive target. The packaged installer also requires absolute
		// configuration roots.
		if !filepath.IsAbs(cleanRoot) || cleanRoot == string(filepath.Separator) {
			return
		}
		paths = append(paths, filepath.Join(append([]string{cleanRoot}, parts...)...))
	}
	appendPath(env.userConfigDir(), "systemd", "user", "pokachy.service")
	config := env.configDir()
	cleanConfig := filepath.Clean(config)
	// Credential cleanup is permitted only for a directory clearly dedicated
	// to Pokachy. This prevents a hostile POKACHY_CONFIG_DIR from targeting a
	// home or shared configuration directory.
	if filepath.IsAbs(cleanConfig) && filepath.Base(cleanConfig) == "pokachy" {
		for _, name := range []string{"credentials.json", "state.json", "notified.json", "daemon.lock"} {
			appendPath(cleanConfig, name)
		}
		// os.Remove is deliberately non-recursive: unexpected files survive.
		appendPath(cleanConfig)
	}
	appendPath(env.installPrefix(), "bin", "pokachy")
	appendPath(env.installPrefix(), "share", "icons", "hicolor", "scalable", "apps", "pokachy.svg")
	return paths
}

func uninstallWithEnvironment(ctx context.Context, yes bool, env maintenanceEnvironment) error {
	if !yes && !confirmDefaultNo(env.input, env.output, "Remove Pokachy from this computer? This removes local files and credentials only; it does not delete your account. [y/N] ") {
		fmt.Fprintln(env.output, "Uninstall cancelled. Your Pokachy account remains unchanged.")
		return nil
	}
	fmt.Fprintln(env.output, "Removing local Pokachy files only. This does not delete your account.")

	var failures []error
	installed, _, known := omarchyPluginStatus(ctx, env)
	if known && installed {
		args := []string{"plugin", "remove", omarchyPluginID}
		if yes {
			args = append(args, "--yes")
		}
		if err := env.run(ctx, "omarchy", args...); err != nil {
			failures = append(failures, fmt.Errorf("remove Omarchy plugin: %w", err))
		}
	} else if !known {
		fmt.Fprintln(env.output, "Could not determine the Omarchy plugin state; no plugin files were removed directly.")
	}

	if _, err := env.lookPath("systemctl"); err == nil {
		if err := env.run(ctx, "systemctl", "--user", "disable", "--now", "pokachy.service"); err != nil {
			fmt.Fprintln(env.output, "Could not stop the Pokachy user service; continuing with local cleanup.")
		}
	}
	for _, path := range knownPokachyPaths(env) {
		if err := env.remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Errorf("remove %s: %w", path, err))
		}
	}
	if _, err := env.lookPath("systemctl"); err == nil {
		if err := env.run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			fmt.Fprintln(env.output, "Could not reload systemd user services after cleanup.")
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	fmt.Fprintln(env.output, "Pokachy was removed from this computer. Your server account still exists; use account settings to revoke or delete it.")
	return nil
}

func cached() (State, error) {
	var s State
	data, err := os.ReadFile(filepath.Join(configDir(), "state.json"))
	if err != nil {
		s.NeedsLogin = true
		return s, nil
	}
	if err = json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	if time.Now().Unix()-s.SyncedAt > 90 {
		s.Online = false
	}
	return s, nil
}
func status(watch, asJSON bool) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	last := ""
	for {
		s, err := cached()
		if err != nil {
			return err
		}
		data, _ := json.Marshal(s)
		if string(data) != last {
			if asJSON {
				fmt.Println(string(data))
			} else {
				if s.NeedsLogin {
					fmt.Println("Not connected. Run: pokachy init")
				} else {
					connection := "offline"
					if s.Online {
						connection = "connected"
					}
					fmt.Printf("@%s · %s · %d pending pokes\n", safe(s.Me.Handle), connection, len(s.Inbox))
				}
			}
			last = string(data)
		}
		if !watch {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}
}
func (c *Client) daemon(ctx context.Context) error {
	lock, err := os.OpenFile(filepath.Join(c.Dir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("the Pokachy companion is already running")
	}
	var notified = map[string]bool{}
	if data, e := os.ReadFile(filepath.Join(c.Dir, "notified.json")); e == nil {
		_ = json.Unmarshal(data, &notified)
	}
	var syncMu sync.Mutex
	syncState := func() error {
		syncMu.Lock()
		defer syncMu.Unlock()
		s, e := c.sync(ctx)
		if e != nil {
			return e
		}
		next := map[string]bool{}
		for _, p := range s.Inbox {
			next[p.ID] = true
			if !notified[p.ID] && !s.Me.Quiet {
				c.notify(ctx, p)
			}
		}
		notified = next
		return save(filepath.Join(c.Dir, "notified.json"), notified)
	}
	backoff := time.Second
	for ctx.Err() == nil {
		if err = syncState(); err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) && (apiErr.Status == 401 || apiErr.Status == 403) {
				_ = save(filepath.Join(c.Dir, "state.json"), State{NeedsLogin: true})
				return err
			}
		}
		endpoint := strings.Replace(c.Config.Server, "https://", "wss://", 1)
		endpoint = strings.Replace(endpoint, "http://", "ws://", 1)
		headers := http.Header{"Authorization": []string{"Bearer " + c.Config.Token}, "User-Agent": []string{"Pokachy/" + version}}
		wsHTTP := *c.HTTP
		wsHTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		conn, _, dialErr := websocket.Dial(ctx, endpoint+"/api/events", &websocket.DialOptions{HTTPHeader: headers, HTTPClient: &wsHTTP})
		if dialErr == nil {
			backoff = time.Second
			events := make(chan error, 1)
			go func() {
				for {
					_, _, e := conn.Read(ctx)
					if e != nil {
						events <- e
						return
					}
					if e = syncState(); e != nil {
						events <- e
						return
					}
				}
			}()
			ticker := time.NewTicker(45 * time.Second)
			running := true
			for running {
				select {
				case <-ctx.Done():
					running = false
				case <-events:
					running = false
				case <-ticker.C:
					if e := syncState(); e != nil {
						running = false
					} else {
						pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
						e = conn.Write(pingCtx, websocket.MessageText, []byte("ping"))
						cancel()
						if e != nil {
							running = false
						}
					}
				}
			}
			ticker.Stop()
			_ = conn.CloseNow()
		}
		s, _ := cached()
		s.Online = false
		_ = save(filepath.Join(c.Dir, "state.json"), s)
		if ctx.Err() != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
	return nil
}
func (c *Client) notify(ctx context.Context, p Poke) {
	go func() {
		notifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		title := safe(p.Name)
		if title == "" {
			title = "@" + safe(p.Handle)
		}
		cmd := exec.CommandContext(notifyCtx, "notify-send", "--app-name=Pokachy", "--icon="+notificationIcon(), "--expire-time=12000", "--wait", "--action=poke=Poke back", "--", html.EscapeString(title)+" poked you", "A little nudge from @"+html.EscapeString(safe(p.Handle)))
		out, err := cmd.Output()
		if err != nil {
			if notifyCtx.Err() == nil {
				fmt.Fprintln(os.Stderr, "Pokachy could not show a notification; check notify-send and your desktop notification service.")
			}
			return
		}
		if strings.TrimSpace(string(out)) == "poke" {
			if err = c.poke(ctx, p.Handle); err != nil {
				fmt.Fprintln(os.Stderr, "Poke back:", safe(err.Error()))
			}
		}
	}()
}
