package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeTUIBackend struct {
	state    State
	calls    []string
	failPoke error
}

func (f *fakeTUIBackend) sync(context.Context) (State, error) {
	f.calls = append(f.calls, "sync")
	return f.state, nil
}
func (f *fakeTUIBackend) poke(_ context.Context, handle string) error {
	f.calls = append(f.calls, "poke "+handle)
	return f.failPoke
}
func (f *fakeTUIBackend) request(_ context.Context, method, path string, _ any, _ any, _ string) error {
	f.calls = append(f.calls, method+" "+path)
	return nil
}
func (f *fakeTUIBackend) history(_ context.Context, handle, before string) (HistoryPage, error) {
	f.calls = append(f.calls, "history "+handle+" "+before)
	next := "older"
	return HistoryPage{History: []Poke{{Outgoing: 1, CreatedAt: 1}}, NextCursor: &next}, nil
}

func tuiFixtureState() State {
	return State{
		Me:       Me{Handle: "me"},
		Inbox:    []Poke{{ID: "p/1", Handle: "ana", CreatedAt: time.Now().UnixMilli()}},
		Friends:  []Person{{Handle: "ana", Name: "Ana"}, {Handle: "bo", Waiting: 1}},
		Requests: []Person{{Handle: "cy"}, {Handle: "di", Outgoing: 1}},
	}
}

func press(t *testing.T, m *tuiModel, b *fakeTUIBackend, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if op := m.key(k); op != nil {
			m.apply(runTUIOp(context.Background(), b, op))
		}
	}
}

func TestTUIActionsCallTheSameEndpointsAsCommands(t *testing.T) {
	b := &fakeTUIBackend{state: tuiFixtureState()}
	m := &tuiModel{width: 80, height: 24}
	m.setState(b.state)

	press(t, m, b, "enter")       // poke back from inbox
	press(t, m, b, "d")           // dismiss inbox item
	press(t, m, b, "j", "p")      // poke friend ana
	press(t, m, b, "j", "x", "n") // cancelled removal
	press(t, m, b, "x", "y")      // confirmed removal of bo
	press(t, m, b, "j", "enter")  // accept cy
	press(t, m, b, "j", "enter")  // outgoing request: nothing to send
	press(t, m, b, "b", "y")      // block di
	press(t, m, b, "z")           // quiet on
	press(t, m, b, "a", "@", "n", "e", "w", "backspace", "w", "enter")

	want := []string{
		"poke ana", "sync",
		"POST /api/inbox/p%2F1/dismiss", "sync",
		"poke ana", "sync",
		"DELETE /api/friends/bo", "sync",
		"POST /api/friends/cy/accept", "sync",
		"POST /api/blocks/di", "sync",
		"PUT /api/profile", "sync",
		"POST /api/friends/new", "sync",
	}
	if !reflect.DeepEqual(b.calls, want) {
		t.Fatalf("calls:\n%v\nwant:\n%v", b.calls, want)
	}
	if m.busy || m.failed {
		t.Fatalf("model left busy=%v failed=%v (%q)", m.busy, m.failed, m.message)
	}
}

func TestTUIHistoryPagesAndReportsErrors(t *testing.T) {
	b := &fakeTUIBackend{state: tuiFixtureState()}
	m := &tuiModel{width: 60, height: 12}
	m.setState(b.state)
	press(t, m, b, "j", "h")
	if m.mode != tuiHistory || m.historyFor != "ana" {
		t.Fatalf("history mode = %v for %q", m.mode, m.historyFor)
	}
	if frame := m.render(time.Now()); !strings.Contains(frame, "History with @ana") || !strings.Contains(frame, "n for older") {
		t.Fatalf("history frame missing content:\n%s", frame)
	}
	press(t, m, b, "n", "esc")
	if b.calls[len(b.calls)-2] != "history ana older" || m.mode != tuiNormal {
		t.Fatalf("paging calls %v mode %v", b.calls, m.mode)
	}

	b.failPoke = errors.New("slow down\x1b[31m")
	press(t, m, b, "p")
	if !m.failed || m.message != "slow down[31m" {
		t.Fatalf("error message = %q failed=%v", m.message, m.failed)
	}
}

func TestTUIKeepsSelectionAcrossRefresh(t *testing.T) {
	m := &tuiModel{}
	s := tuiFixtureState()
	m.setState(s)
	m.key("j")
	m.key("j") // bo
	s.Inbox = nil
	m.setState(s)
	if it := m.selected(); it == nil || it.handle != "bo" || it.kind != tuiFriend {
		t.Fatalf("selected = %+v", it)
	}
	m.setState(State{})
	if m.selected() != nil || m.cursor != 0 {
		t.Fatalf("empty state cursor = %d", m.cursor)
	}
	m.key("j")
	m.key("enter")
	if m.cursor != 0 || m.busy {
		t.Fatal("keys on an empty list changed state")
	}
}

func TestTUIRenderFitsTerminalAndStripsRemoteControls(t *testing.T) {
	s := tuiFixtureState()
	s.Friends[0].Name = "Ana\x1b]0;evil\x07 " + strings.Repeat("long", 40)
	s.Friends[1].Name = strings.Repeat("王小明", 10)
	s.Requests[0].Name = "Zoë 🎉 José"
	m := &tuiModel{width: 40, height: 10}
	m.setState(s)
	frame := m.render(time.Now())
	lines := strings.Split(frame, "\r\n")
	if len(lines) != 10 {
		t.Fatalf("frame has %d lines", len(lines))
	}
	if strings.Contains(frame, "\x1b]") || strings.Contains(frame, "\x07") {
		t.Fatal("remote control sequence reached the terminal")
	}
	for _, l := range lines {
		plain := l
		for _, code := range []string{ansiReset, ansiBold, ansiDim, ansiReverse, ansiRed, ansiGreen, ansiYellow} {
			plain = strings.ReplaceAll(plain, code, "")
		}
		if n := textWidth(plain); n != 40 {
			t.Fatalf("line width %d: %q", n, plain)
		}
	}
	if !strings.Contains(frame, "@me") || !strings.Contains(frame, "Inbox (1)") {
		t.Fatalf("frame missing header or inbox:\n%s", frame)
	}
}

func TestParseKeys(t *testing.T) {
	got := parseKeys([]byte("j\x1b[A\x1b[B\r\x7f\x03\x1b[3~é\x1bq"))
	want := []string{"j", "up", "down", "enter", "backspace", "ctrl+c", "é", "esc", "q"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseKeys = %q want %q", got, want)
	}
}

func TestFitCountsTerminalColumns(t *testing.T) {
	for _, tc := range []struct {
		in    string
		width int
		want  string
	}{
		{"abc", 5, "abc  "},
		{"王小明", 6, "王小明"},
		{"王小明", 7, "王小明 "},
		{"王小明", 5, "王小…"},
		{"王小明", 4, "王… "},                // 小 would straddle the edge, so it becomes padding
		{"Jose\u0301", 5, "Jose\u0301 "}, // combining accent takes no column
		{"🎉 hi", 3, "🎉…"},
		{"김민준", 1, "…"},
		{"anything", 0, ""},
	} {
		got := fit(tc.in, tc.width)
		if got != tc.want || textWidth(got) != tc.width {
			t.Errorf("fit(%q, %d) = %q (width %d), want %q", tc.in, tc.width, got, textWidth(got), tc.want)
		}
	}
	for r, want := range map[rune]int{'a': 1, 'é': 1, '\u0301': 0, '王': 2, '김': 2, 'Ａ': 2, '🎉': 2, '·': 1, '…': 1, '▸': 1, '\u3248': 1} {
		if got := cellWidth(r); got != want {
			t.Errorf("cellWidth(%U) = %d, want %d", r, got, want)
		}
	}
}

func TestTUIHistoryScrollsAndKeepsHint(t *testing.T) {
	m := &tuiModel{width: 40, height: 8, mode: tuiHistory, historyFor: "ana"}
	next := "older"
	for i := range 20 {
		m.history.History = append(m.history.History, Poke{CreatedAt: int64(i+1) * 60_000})
	}
	m.history.NextCursor = &next
	frame := m.render(time.Now())
	if !strings.Contains(frame, "n for older") {
		t.Fatalf("paging hint scrolled off:\n%s", frame)
	}
	for range 30 {
		m.key("j")
	}
	m.render(time.Now())
	if m.historyTop != 20-2 { // body is 4 rows: title, 2 entries, hint
		t.Fatalf("historyTop = %d after scrolling to the end", m.historyTop)
	}
	m.key("k")
	if m.historyTop != 17 {
		t.Fatalf("historyTop = %d after scrolling up", m.historyTop)
	}
}

func TestTUIAddFriendTakesOnlyHandleCharacters(t *testing.T) {
	b := &fakeTUIBackend{state: tuiFixtureState()}
	m := &tuiModel{width: 80, height: 24}
	m.setState(b.state)
	press(t, m, b, "a", "@", "王", "B", "o", "-", "_", "1", "@", "enter")
	if got := b.calls[0]; got != "POST /api/friends/bo_1" {
		t.Fatalf("add friend called %q", got)
	}
}

func TestTUIAddFriendAcceptsAnIncomingRequest(t *testing.T) {
	b := &fakeTUIBackend{state: tuiFixtureState()}
	m := &tuiModel{width: 80, height: 24}
	m.setState(b.state)
	press(t, m, b, "a", "c", "y", "enter")
	if got := b.calls[0]; got != "POST /api/friends/cy/accept" {
		t.Fatalf("adding a pending requester called %q", got)
	}
}

func TestTUIHelpFitsNarrowTerminals(t *testing.T) {
	for _, w := range []int{40, 80} {
		m := &tuiModel{width: w, height: 30, mode: tuiHelp}
		frame := m.render(time.Now())
		if strings.Contains(frame, "…") || !strings.Contains(frame, "remove · decline · cancel") {
			t.Fatalf("help cut off at width %d:\n%s", w, frame)
		}
	}
}
