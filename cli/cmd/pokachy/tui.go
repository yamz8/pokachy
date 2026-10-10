package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unsafe"

	"github.com/coder/websocket"
)

// The terminal UI is a thin interactive layer over the same API calls the
// one-shot commands use. It keeps no state of its own beyond the screen: every
// action is followed by a sync, and the websocket only triggers refreshes.

type tuiBackend interface {
	sync(ctx context.Context) (State, error)
	poke(ctx context.Context, handle string) error
	request(ctx context.Context, method, path string, body any, output any, key string) error
	history(ctx context.Context, handle, before string) (HistoryPage, error)
}

type tuiItemKind int

const (
	tuiInbox tuiItemKind = iota
	tuiFriend
	tuiRequest
)

type tuiItem struct {
	kind      tuiItemKind
	handle    string
	name      string
	id        string
	outgoing  int
	waiting   int
	createdAt int64
}

type tuiMode int

const (
	tuiNormal tuiMode = iota
	tuiAdding
	tuiConfirm
	tuiHistory
	tuiHelp
)

// tuiOp is a network action started by a key press. It runs off the UI loop and
// its result is applied with apply.
type tuiOp func(ctx context.Context, b tuiBackend) tuiResult

type tuiResult struct {
	message    string
	err        error
	history    *HistoryPage
	historyFor string
	state      *State
	syncErr    error
	background bool
}

type tuiModel struct {
	state      State
	loaded     bool
	items      []tuiItem
	cursor     int
	mode       tuiMode
	input      []rune
	confirm    string
	confirmOp  tuiOp
	message    string
	failed     bool
	busy       bool
	history    HistoryPage
	historyFor string
	historyTop int
	width      int
	height     int
	live       bool
	quit       bool
}

func (m *tuiModel) setState(s State) {
	selected := m.selected()
	m.state, m.loaded = s, true
	m.items = m.items[:0]
	for _, p := range s.Inbox {
		m.items = append(m.items, tuiItem{kind: tuiInbox, handle: p.Handle, name: p.Name, id: p.ID, createdAt: p.CreatedAt})
	}
	for _, p := range s.Friends {
		m.items = append(m.items, tuiItem{kind: tuiFriend, handle: p.Handle, name: p.Name, waiting: p.Waiting})
	}
	for _, p := range s.Requests {
		m.items = append(m.items, tuiItem{kind: tuiRequest, handle: p.Handle, name: p.Name, outgoing: p.Outgoing})
	}
	// Keep the cursor on the same row across refreshes when it still exists.
	if selected != nil {
		for i, it := range m.items {
			if it.kind == selected.kind && it.handle == selected.handle && it.id == selected.id {
				m.cursor = i
				return
			}
		}
	}
	m.cursor = max(0, min(m.cursor, len(m.items)-1))
}

func (m *tuiModel) selected() *tuiItem {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return nil
	}
	it := m.items[m.cursor]
	return &it
}

func (m *tuiModel) say(msg string, failed bool) { m.message, m.failed = msg, failed }

func (m *tuiModel) apply(r tuiResult) {
	if !r.background {
		m.busy = false
	}
	if r.state != nil {
		m.setState(*r.state)
	}
	switch {
	case r.err != nil:
		m.say(safe(r.err.Error()), true)
		return
	case r.history != nil:
		m.history, m.historyFor, m.historyTop, m.mode = *r.history, r.historyFor, 0, tuiHistory
		m.say("", false)
	case r.message != "":
		m.say(r.message, false)
	case !r.background:
		// A manual refresh has no message of its own; clear "Refreshing…".
		m.say("", false)
	}
	if r.syncErr != nil {
		m.say(strings.TrimSpace(r.message+" Could not refresh: "+safe(r.syncErr.Error())), true)
	}
}

func tuiMutation(method, path string, body any, done string) tuiOp {
	return func(ctx context.Context, b tuiBackend) tuiResult {
		if err := b.request(ctx, method, path, body, nil, ""); err != nil {
			return tuiResult{err: err}
		}
		return tuiResult{message: done}
	}
}

func handlePath(prefix, handle, suffix string) string {
	return prefix + url.PathEscape(strings.TrimPrefix(handle, "@")) + suffix
}

// key applies one key press and returns the network action to start, if any.
func (m *tuiModel) key(k string) tuiOp {
	switch m.mode {
	case tuiAdding:
		switch k {
		case "esc", "ctrl+c":
			m.mode, m.input = tuiNormal, nil
		case "enter":
			handle := strings.TrimPrefix(strings.TrimSpace(string(m.input)), "@")
			m.mode, m.input = tuiNormal, nil
			if handle == "" {
				return nil
			}
			// Adding someone who already asked is the same as accepting them.
			for _, it := range m.items {
				if it.kind == tuiRequest && it.outgoing == 0 && it.handle == handle {
					return m.start("Accepting…", tuiMutation("POST", handlePath("/api/friends/", handle, "/accept"), map[string]any{}, "You and @"+safe(handle)+" are friends now."))
				}
			}
			return m.start("Sending request to @"+handle+"…", tuiMutation("POST", handlePath("/api/friends/", handle, ""), map[string]any{}, "Friend request sent to @"+safe(handle)+"."))
		case "backspace":
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}
		default:
			// Handles are 3–24 lowercase letters, digits, or underscores; take
			// only those so a typo shows up before it reaches the server.
			if r := []rune(strings.ToLower(k)); len(r) == 1 && len(m.input) < 25 &&
				(r[0] >= 'a' && r[0] <= 'z' || r[0] >= '0' && r[0] <= '9' || r[0] == '_' || r[0] == '@' && len(m.input) == 0) {
				m.input = append(m.input, r[0])
			}
		}
		return nil
	case tuiConfirm:
		op := m.confirmOp
		m.mode, m.confirm, m.confirmOp = tuiNormal, "", nil
		if k == "y" || k == "Y" {
			return m.start("Working…", op)
		}
		m.say("Cancelled.", false)
		return nil
	case tuiHistory, tuiHelp:
		switch k {
		case "ctrl+c":
			m.quit = true
		case "esc", "q", "enter", "h", "?":
			m.mode = tuiNormal
		case "up", "k":
			m.historyTop = max(0, m.historyTop-1)
		case "down", "j":
			// render clamps this to the last screenful.
			m.historyTop = min(m.historyTop+1, max(0, len(m.history.History)-1))
		case "n":
			if m.mode == tuiHistory && m.history.NextCursor != nil {
				return m.historyOp(m.historyFor, *m.history.NextCursor)
			}
		}
		return nil
	}

	it := m.selected()
	switch k {
	case "q", "ctrl+c":
		m.quit = true
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = max(0, min(len(m.items)-1, m.cursor+1))
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = max(0, len(m.items)-1)
	case "?":
		m.mode = tuiHelp
	case "r":
		return m.start("Refreshing…", func(context.Context, tuiBackend) tuiResult { return tuiResult{} })
	case "a":
		m.mode, m.input = tuiAdding, nil
	case "z":
		if !m.loaded {
			return nil
		}
		on := !m.state.Me.Quiet
		done := "Quiet mode off. Notifications are back."
		if on {
			done = "Quiet mode on. Pokes still reach your inbox."
		}
		return m.start("Updating quiet mode…", tuiMutation("PUT", "/api/profile", map[string]any{"quiet": on}, done))
	case "enter", "p", " ":
		if it == nil {
			return nil
		}
		if it.kind == tuiRequest {
			if it.outgoing == 1 {
				m.say("Waiting for @"+safe(it.handle)+" to accept.", false)
				return nil
			}
			return m.start("Accepting…", tuiMutation("POST", handlePath("/api/friends/", it.handle, "/accept"), map[string]any{}, "You and @"+safe(it.handle)+" are friends now."))
		}
		handle := it.handle
		return m.start("Poking @"+safe(handle)+"…", func(ctx context.Context, b tuiBackend) tuiResult {
			if err := b.poke(ctx, handle); err != nil {
				return tuiResult{err: err}
			}
			return tuiResult{message: "A little nudge sent to @" + safe(handle) + "."}
		})
	case "d":
		if it == nil || it.kind != tuiInbox {
			return nil
		}
		return m.start("Dismissing…", tuiMutation("POST", "/api/inbox/"+url.PathEscape(it.id)+"/dismiss", map[string]any{}, "Dismissed."))
	case "h":
		if it == nil || it.kind == tuiRequest {
			return nil
		}
		return m.historyOp(it.handle, "")
	case "x":
		if it == nil || it.kind == tuiInbox {
			return nil
		}
		prompt := "Remove @" + safe(it.handle) + " from friends?"
		done := "Removed @" + safe(it.handle) + "."
		if it.kind == tuiRequest && it.outgoing == 1 {
			prompt, done = "Cancel your request to @"+safe(it.handle)+"?", "Request cancelled."
		} else if it.kind == tuiRequest {
			prompt, done = "Decline @"+safe(it.handle)+"'s request?", "Request declined."
		}
		m.ask(prompt, tuiMutation("DELETE", handlePath("/api/friends/", it.handle, ""), map[string]any{}, done))
	case "b":
		if it == nil {
			return nil
		}
		m.ask("Block @"+safe(it.handle)+"? They will not be able to poke you.", tuiMutation("POST", handlePath("/api/blocks/", it.handle, ""), map[string]any{}, "Blocked @"+safe(it.handle)+"."))
	}
	return nil
}

func (m *tuiModel) ask(prompt string, op tuiOp) {
	m.mode, m.confirm, m.confirmOp = tuiConfirm, prompt, op
}

func (m *tuiModel) start(status string, op tuiOp) tuiOp {
	if m.busy {
		m.say("Still working on the last action…", false)
		return nil
	}
	m.busy = true
	m.say(status, false)
	return op
}

func (m *tuiModel) historyOp(handle, before string) tuiOp {
	return m.start("Loading history…", func(ctx context.Context, b tuiBackend) tuiResult {
		page, err := b.history(ctx, handle, before)
		if err != nil {
			return tuiResult{err: err}
		}
		return tuiResult{history: &page, historyFor: handle}
	})
}

// runOp performs an action and then refreshes, mirroring the one-shot commands.
func runTUIOp(ctx context.Context, b tuiBackend, op tuiOp) tuiResult {
	r := op(ctx, b)
	if s, err := b.sync(ctx); err != nil {
		r.syncErr = err
	} else {
		r.state = &s
	}
	if r.err != nil {
		r.syncErr = nil
	}
	return r
}

// cellWidth reports how many terminal columns r occupies: 0 for combining
// marks, 2 for East Asian wide and fullwidth characters and most emoji, and 1
// otherwise. It follows the wide ranges of Unicode's EastAsianWidth data closely
// enough for names; terminals disagree on the rest anyway.
func cellWidth(r rune) int {
	switch {
	case r == 0 || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r):
		return 0
	case r >= 0x1160 && r <= 0x11ff: // Hangul medial vowels and final consonants join the syllable
		return 0
	case r < 0x1100:
		return 1
	}
	for _, w := range wideRanges {
		if r < w[0] {
			return 1
		}
		if r <= w[1] {
			return 2
		}
	}
	return 1
}

// wideRanges is sorted so cellWidth can stop at the first range past r.
var wideRanges = [][2]rune{
	{0x1100, 0x115f}, {0x231a, 0x231b}, {0x2329, 0x232a}, {0x23e9, 0x23ec}, {0x23f0, 0x23f0},
	{0x23f3, 0x23f3}, {0x25fd, 0x25fe}, {0x2614, 0x2615}, {0x2648, 0x2653}, {0x267f, 0x267f},
	{0x2693, 0x2693}, {0x26a1, 0x26a1}, {0x26aa, 0x26ab}, {0x26bd, 0x26be}, {0x26c4, 0x26c5},
	{0x26ce, 0x26ce}, {0x26d4, 0x26d4}, {0x26ea, 0x26ea}, {0x26f2, 0x26f3}, {0x26f5, 0x26f5},
	{0x26fa, 0x26fa}, {0x26fd, 0x26fd}, {0x2705, 0x2705}, {0x270a, 0x270b}, {0x2728, 0x2728},
	{0x274c, 0x274c}, {0x274e, 0x274e}, {0x2753, 0x2755}, {0x2757, 0x2757}, {0x2795, 0x2797},
	{0x27b0, 0x27b0}, {0x27bf, 0x27bf}, {0x2b1b, 0x2b1c}, {0x2b50, 0x2b50}, {0x2b55, 0x2b55},
	{0x2e80, 0x303e}, {0x3041, 0x3247}, {0x3250, 0x4dbf}, {0x4e00, 0x9fff}, {0xa000, 0xa4cf},
	{0xa960, 0xa97f}, {0xac00, 0xd7a3}, {0xf900, 0xfaff}, {0xfe10, 0xfe19}, {0xfe30, 0xfe6f},
	{0xff00, 0xff60}, {0xffe0, 0xffe6}, {0x16fe0, 0x16fe4}, {0x16ff0, 0x16ff1}, {0x17000, 0x18d08}, {0x1aff0, 0x1b2ff},
	{0x1f004, 0x1f004}, {0x1f0cf, 0x1f0cf}, {0x1f18e, 0x1f18e}, {0x1f191, 0x1f19a}, {0x1f200, 0x1f202},
	{0x1f210, 0x1f23b}, {0x1f240, 0x1f248}, {0x1f250, 0x1f251}, {0x1f260, 0x1f265}, {0x1f300, 0x1f320},
	{0x1f32d, 0x1f335}, {0x1f337, 0x1f37c}, {0x1f37e, 0x1f393}, {0x1f3a0, 0x1f3ca}, {0x1f3cf, 0x1f3d3},
	{0x1f3e0, 0x1f3f0}, {0x1f3f4, 0x1f3f4}, {0x1f3f8, 0x1f43e}, {0x1f440, 0x1f440}, {0x1f442, 0x1f4fc},
	{0x1f4ff, 0x1f53d}, {0x1f54b, 0x1f54e}, {0x1f550, 0x1f567}, {0x1f57a, 0x1f57a}, {0x1f595, 0x1f596},
	{0x1f5a4, 0x1f5a4}, {0x1f5fb, 0x1f64f}, {0x1f680, 0x1f6c5}, {0x1f6cc, 0x1f6cc}, {0x1f6d0, 0x1f6d2},
	{0x1f6d5, 0x1f6d7}, {0x1f6dc, 0x1f6df}, {0x1f6eb, 0x1f6ec}, {0x1f6f4, 0x1f6fc}, {0x1f7e0, 0x1f7eb},
	{0x1f7f0, 0x1f7f0}, {0x1f90c, 0x1f93a}, {0x1f93c, 0x1f945}, {0x1f947, 0x1f9ff}, {0x1fa70, 0x1faff},
	{0x20000, 0x2fffd}, {0x30000, 0x3fffd},
}

// textWidth is the number of terminal columns s occupies.
func textWidth(s string) int {
	n := 0
	for _, r := range s {
		n += cellWidth(r)
	}
	return n
}

// fit truncates or pads s to exactly width terminal columns, so a line never
// wraps. Remote text has already passed through safe, so every rune is
// printable. A wide character that would straddle the edge is replaced by
// padding rather than split.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if n := textWidth(s); n <= width {
		return s + strings.Repeat(" ", width-n)
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := cellWidth(r)
		if used+w > width-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…" + strings.Repeat(" ", width-1-used)
}

func ago(ms int64, now time.Time) string {
	d := now.Sub(time.UnixMilli(ms))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return time.UnixMilli(ms).Local().Format("Jan 2 15:04")
}

func tuiLabel(it tuiItem, now time.Time) string {
	who := "@" + safe(it.handle)
	if name := safe(it.name); name != "" && name != safe(it.handle) {
		who = name + " (@" + safe(it.handle) + ")"
	}
	switch it.kind {
	case tuiInbox:
		return who + " poked you · " + ago(it.createdAt, now)
	case tuiRequest:
		if it.outgoing == 1 {
			return who + " · request sent"
		}
		return who + " · wants to be friends"
	}
	if it.waiting == 1 {
		return who + " · waiting for a poke back"
	}
	return who
}

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiReverse = "\x1b[7m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
)

// render draws a full frame. Each line is padded to the terminal width so a
// shorter frame overwrites a longer previous one without a flickering clear.
func (m *tuiModel) render(now time.Time) string {
	w, h := max(m.width, 20), max(m.height, 6)
	lines := make([]string, 0, h)
	add := func(style, text string) {
		if style == "" {
			lines = append(lines, fit(text, w))
		} else {
			lines = append(lines, style+fit(text, w)+ansiReset)
		}
	}

	title := "Pokachy"
	if m.loaded && m.state.Me.Handle != "" {
		title += " · @" + safe(m.state.Me.Handle)
	}
	flags := []string{}
	if m.loaded && m.state.Me.Quiet {
		flags = append(flags, "quiet")
	}
	if m.live {
		flags = append(flags, "live")
	} else {
		flags = append(flags, "offline")
	}
	add(ansiBold+ansiReverse, " "+title+"  ["+strings.Join(flags, " · ")+"]")
	add("", "")

	body := h - 4 // header, blank, blank, footer
	switch m.mode {
	case tuiHelp:
		help := [][2]string{
			{"↑/k ↓/j", "move"}, {"enter/p", "poke · poke back · accept"},
			{"a", "add a friend"}, {"d", "dismiss a poke"},
			{"h", "history"}, {"x", "remove · decline · cancel"},
			{"z", "toggle quiet"}, {"b", "block"},
			{"r", "refresh"}, {"q", "quit"},
		}
		// Two columns when they fit, otherwise one, so nothing is cut off.
		for i := 0; i < len(help); i += 2 {
			left := fmt.Sprintf("  %-10s %s", help[i][0], help[i][1])
			right := fmt.Sprintf("%-10s %s", help[i+1][0], help[i+1][1])
			if w >= 72 {
				add("", fmt.Sprintf("%-32s%s", left, right))
			} else {
				add("", left)
				add("", "  "+right)
			}
		}
		add("", "")
		add("", "  Only mutual friends can poke.")
		add("", "  One outstanding poke per direction.")
		add("", "  Press esc or ? to close this help.")
	case tuiHistory:
		add(ansiBold, "  History with @"+safe(m.historyFor))
		if len(m.history.History) == 0 {
			add(ansiDim, "  No pokes yet.")
		}
		// Keep the title and the hint visible; scroll the entries between them.
		rows := max(1, body-2)
		m.historyTop = max(0, min(m.historyTop, len(m.history.History)-rows))
		for _, p := range m.history.History[m.historyTop:min(len(m.history.History), m.historyTop+rows)] {
			verb := "  ← poked you"
			if p.Outgoing == 1 {
				verb = "  → you poked"
			}
			add("", verb+" · "+time.UnixMilli(p.CreatedAt).Local().Format("Jan 2 15:04"))
		}
		more := "  esc to go back"
		if len(m.history.History) > rows {
			more = "  ↑/↓ scroll · esc to go back"
		}
		if m.history.NextCursor != nil {
			more = "  ↑/↓ scroll · n for older · esc to go back"
		}
		add(ansiDim, more)
	default:
		if !m.loaded {
			add(ansiDim, "  Loading…")
			break
		}
		rows := m.listRows(now)
		// Scroll so the cursor row stays visible.
		cursorRow := 0
		for i, r := range rows {
			if r.item == m.cursor && r.selectable {
				cursorRow = i
			}
		}
		start := 0
		if cursorRow >= body {
			start = cursorRow - body + 1
		}
		for i := start; i < len(rows) && i < start+body; i++ {
			r := rows[i]
			switch {
			case r.selectable && r.item == m.cursor:
				add(ansiReverse, " ▸ "+r.text)
			case r.selectable:
				add(r.style, "   "+r.text)
			default:
				add(r.style, r.text)
			}
		}
	}
	for len(lines) < h-2 {
		add("", "")
	}
	lines = lines[:h-2]

	switch {
	case m.mode == tuiAdding:
		add(ansiBold, " Add friend: @"+string(m.input)+"▏  (enter to send, esc to cancel)")
	case m.mode == tuiConfirm:
		add(ansiBold+ansiYellow, " "+m.confirm+" [y/N]")
	case m.failed:
		add(ansiRed, " "+m.message)
	case m.message != "":
		add(ansiGreen, " "+m.message)
	default:
		add("", "")
	}
	hint := " enter poke · a add · d dismiss · h history · z quiet · ? help · q quit"
	if textWidth(hint) > w {
		hint = " enter poke · ? help · q quit"
	}
	add(ansiDim, hint)
	return strings.Join(lines, "\r\n")
}

type tuiRow struct {
	text       string
	style      string
	item       int
	selectable bool
}

func (m *tuiModel) listRows(now time.Time) []tuiRow {
	var rows []tuiRow
	section := func(kind tuiItemKind, title, empty string) {
		rows = append(rows, tuiRow{text: " " + title, style: ansiBold})
		found := false
		for i, it := range m.items {
			if it.kind == kind {
				style := ""
				if kind == tuiInbox {
					style = ansiYellow
				}
				rows = append(rows, tuiRow{text: tuiLabel(it, now), style: style, item: i, selectable: true})
				found = true
			}
		}
		if !found && empty != "" {
			rows = append(rows, tuiRow{text: "   " + empty, style: ansiDim})
		}
		rows = append(rows, tuiRow{})
	}
	section(tuiInbox, fmt.Sprintf("Inbox (%d)", len(m.state.Inbox)), "All caught up.")
	section(tuiFriend, fmt.Sprintf("Friends (%d)", len(m.state.Friends)), "Your corner is quiet. Press a to add someone.")
	if len(m.state.Requests) > 0 {
		section(tuiRequest, fmt.Sprintf("Requests (%d)", len(m.state.Requests)), "")
	}
	return rows
}

// parseKeys splits a chunk of terminal input into named keys.
func parseKeys(b []byte) []string {
	var keys []string
	for len(b) > 0 {
		switch {
		case b[0] == 0x1b && len(b) >= 3 && (b[1] == '[' || b[1] == 'O'):
			seq := map[byte]string{'A': "up", 'B': "down", 'C': "right", 'D': "left", 'H': "home", 'F': "end"}
			if name, ok := seq[b[2]]; ok {
				keys = append(keys, name)
				b = b[3:]
				continue
			}
			// Skip other CSI sequences (e.g. "\x1b[3~") up to their final byte.
			i := 2
			for i < len(b) && (b[i] < 0x40 || b[i] > 0x7e) {
				i++
			}
			if i < len(b) && b[i] == '~' && string(b[2:i]) == "1" {
				keys = append(keys, "home")
			} else if i < len(b) && b[i] == '~' && string(b[2:i]) == "4" {
				keys = append(keys, "end")
			}
			b = b[min(i+1, len(b)):]
		case b[0] == 0x1b:
			keys = append(keys, "esc")
			b = b[1:]
		case b[0] == '\r' || b[0] == '\n':
			keys = append(keys, "enter")
			b = b[1:]
		case b[0] == 0x7f || b[0] == 0x08:
			keys = append(keys, "backspace")
			b = b[1:]
		case b[0] == 0x03:
			keys = append(keys, "ctrl+c")
			b = b[1:]
		case b[0] < 0x20:
			b = b[1:]
		default:
			r := []rune(string(b))
			if len(r) == 0 {
				return keys
			}
			n := len(string(r[0]))
			keys = append(keys, string(b[:n]))
			b = b[n:]
		}
	}
	return keys
}

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

func terminalSize(fd uintptr) (int, int) {
	var ws struct{ Row, Col, X, Y uint16 }
	if ioctl(fd, syscall.TIOCGWINSZ, unsafe.Pointer(&ws)) != nil || ws.Col == 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}

// rawMode switches the terminal to unbuffered, no-echo input and returns a
// function that restores the original settings.
func rawMode(fd uintptr) (func(), error) {
	var old syscall.Termios
	if err := ioctl(fd, syscall.TCGETS, unsafe.Pointer(&old)); err != nil {
		return nil, err
	}
	raw := old
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN], raw.Cc[syscall.VTIME] = 1, 0
	if err := ioctl(fd, syscall.TCSETS, unsafe.Pointer(&raw)); err != nil {
		return nil, err
	}
	return func() { _ = ioctl(fd, syscall.TCSETS, unsafe.Pointer(&old)) }, nil
}

// watchEvents signals refresh whenever the server announces a change, and
// reports whether the live connection is up. It reconnects with backoff.
func (c *Client) watchEvents(ctx context.Context, refresh chan<- struct{}, live chan<- bool) {
	endpoint := strings.Replace(c.Config.Server, "https://", "wss://", 1)
	endpoint = strings.Replace(endpoint, "http://", "ws://", 1)
	send := func(v bool) {
		select {
		case live <- v:
		case <-ctx.Done():
		}
	}
	backoff := time.Second
	for ctx.Err() == nil {
		headers := http.Header{"Authorization": []string{"Bearer " + c.Config.Token}, "User-Agent": []string{"Pokachy/" + version}}
		wsHTTP := *c.HTTP
		wsHTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		conn, _, err := websocket.Dial(ctx, endpoint+"/api/events", &websocket.DialOptions{HTTPHeader: headers, HTTPClient: &wsHTTP})
		if err == nil {
			backoff = time.Second
			send(true)
			connCtx, cancel := context.WithCancel(ctx)
			go func() {
				ticker := time.NewTicker(45 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-connCtx.Done():
						return
					case <-ticker.C:
						pingCtx, pingCancel := context.WithTimeout(connCtx, 5*time.Second)
						e := conn.Write(pingCtx, websocket.MessageText, []byte("ping"))
						pingCancel()
						if e != nil {
							cancel()
							return
						}
					}
				}
			}()
			for {
				if _, _, e := conn.Read(connCtx); e != nil {
					break
				}
				select {
				case refresh <- struct{}{}:
				default:
				}
			}
			cancel()
			_ = conn.CloseNow()
			send(false)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func isTerminal(fd uintptr) bool {
	var t syscall.Termios
	return ioctl(fd, syscall.TCGETS, unsafe.Pointer(&t)) == nil
}

func tui(c *Client) error {
	in, out := os.Stdin, os.Stdout
	if !isTerminal(in.Fd()) || !isTerminal(out.Fd()) {
		return errors.New("pokachy tui needs an interactive terminal")
	}
	restore, err := rawMode(in.Fd())
	if err != nil {
		return err
	}
	defer restore()
	// Alternate screen, hidden cursor, and no autowrap, so a line whose width a
	// terminal counts differently is clipped instead of scrolling the frame.
	// All three are restored on every exit path.
	_, _ = io.WriteString(out, "\x1b[?1049h\x1b[?25l\x1b[?7l\x1b[2J")
	defer io.WriteString(out, "\x1b[?7h\x1b[?25h\x1b[?1049l")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGWINCH, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)

	keys := make(chan []string)
	go func() {
		buf := make([]byte, 256)
		for {
			n, e := in.Read(buf)
			if e != nil {
				close(keys)
				return
			}
			select {
			case keys <- parseKeys(buf[:n]):
			case <-ctx.Done():
				return
			}
		}
	}()
	refresh := make(chan struct{}, 1)
	live := make(chan bool)
	go c.watchEvents(ctx, refresh, live)
	results := make(chan tuiResult)
	syncing, syncPending := false, false
	runOp := func(op tuiOp, background bool) {
		if background {
			if syncing {
				syncPending = true
				return
			}
			syncing = true
		}
		go func() {
			opCtx, opCancel := context.WithTimeout(ctx, 30*time.Second)
			defer opCancel()
			r := runTUIOp(opCtx, c, op)
			r.background = background
			select {
			case results <- r:
			case <-ctx.Done():
			}
		}()
	}
	refreshOnly := func(context.Context, tuiBackend) tuiResult { return tuiResult{} }

	m := &tuiModel{}
	m.width, m.height = terminalSize(out.Fd())
	if s, e := cached(); e == nil && !s.NeedsLogin {
		m.setState(s)
	}
	runOp(refreshOnly, true)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	draw := func() { _, _ = io.WriteString(out, "\x1b[H"+m.render(time.Now())) }
	for !m.quit {
		draw()
		select {
		case ks, ok := <-keys:
			if !ok {
				return nil
			}
			for _, k := range ks {
				if op := m.key(k); op != nil {
					runOp(op, false)
				}
				if m.quit {
					break
				}
			}
		case r := <-results:
			// Like the daemon, stop when the device session is no longer valid
			// instead of showing a screen that can never load.
			var apiErr *APIError
			if errors.As(r.syncErr, &apiErr) && (apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden) {
				_ = save(filepath.Join(c.Dir, "state.json"), State{NeedsLogin: true})
				return errors.New("this computer is signed out of Pokachy; run 'pokachy init' to connect it again")
			}
			m.apply(r)
			if r.background {
				syncing = false
				if syncPending {
					// Server events arrive in bursts; coalesce them into one more sync.
					syncPending = false
					runOp(refreshOnly, true)
				}
			}
		case up := <-live:
			m.live = up
		case <-refresh:
			runOp(refreshOnly, true)
		case <-ticker.C:
			runOp(refreshOnly, true)
		case sig := <-signals:
			if sig != syscall.SIGWINCH {
				return nil
			}
			m.width, m.height = terminalSize(out.Fd())
			_, _ = io.WriteString(out, "\x1b[2J")
		}
	}
	return nil
}
