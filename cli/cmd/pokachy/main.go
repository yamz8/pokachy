package main

import (
	"bufio"
	"context"
	"crypto/rand"
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
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/coder/websocket"
)

const version = "0.1.1"

type Config struct {
	Server string `json:"server"`
	Token  string `json:"token"`
}
type Person struct {
	Handle   string `json:"handle"`
	Name     string `json:"name"`
	Outgoing int    `json:"outgoing"`
	Waiting  int    `json:"waiting"`
}
type Poke struct {
	ID        string `json:"id"`
	Handle    string `json:"handle"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
}
type Me struct {
	ID     string `json:"id"`
	Handle string `json:"handle"`
	Name   string `json:"name"`
	Email  string `json:"email"`
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
	if err != nil || relative.IsAbs() || relative.Host != "" || relative.RawQuery != "" || relative.Fragment != "" {
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
	// ResolveReference intentionally receives no query or fragment from callers.
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

  pokachy init [--server https://pokachy.com] [--no-browser]
  pokachy poke @friend
  pokachy friends [add|accept|remove] @friend
  pokachy inbox
  pokachy dismiss POKE_ID
  pokachy quiet on|off
  pokachy block @person | unblock @person
  pokachy report @person "reason"
  pokachy profile HANDLE
  pokachy account                 Open account and device settings
  pokachy status [--json]         Read the local companion status
  pokachy watch --json            Stream local status changes
  pokachy daemon                 Receive desktop notifications
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
	if err := flags.Parse(args); err != nil {
		return err
	}
	*server = strings.TrimRight(*server, "/")
	if err := validServer(*server); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(configDir(), "credentials.json")); err == nil {
		return errors.New("already connected; use 'pokachy logout' before connecting a different account")
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
		fmt.Printf("\nConnected as @%s. Try: pokachy friends add @friend\n", safe(s.Me.Handle))
		fmt.Println("Start notifications with: systemctl --user enable --now pokachy.service")
		return nil
	}
	return errors.New("device request expired; run pokachy init again")
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
		cmd := exec.CommandContext(notifyCtx, "notify-send", "--app-name=Pokachy", "--icon=mail-unread", "--expire-time=12000", "--wait", "--action=poke=Poke back", "--", html.EscapeString(title)+" poked you", "A little nudge from @"+html.EscapeString(safe(p.Handle)))
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
