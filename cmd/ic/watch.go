package main

// `ic watch`: a live intercom pane. Sessions on top (pick one, press enter to
// message it), every message below as it flows, your unread mail highlighted.

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	cReset  = "\x1b[0m"
	cBold   = "\x1b[1m"
	cDim    = "\x1b[2m"
	cRev    = "\x1b[7m"
	cOrange = "\x1b[38;5;208m"
	cBlue   = "\x1b[34m"
	cRed    = "\x1b[31m"
	cGreen  = "\x1b[32m"
)

type icWatch struct {
	sess     []*Session
	status   map[string]string
	sel      int
	w, h     int
	msg      string
	sig      string
	inputOn  bool
	prompt   string
	input    []rune
	submit   func(string)
	overlay  []string
	stty     string
	results  chan string
	unreadMe int
}

func cmdWatch(args []string) {
	sc := exec.Command("stty", "-g")
	sc.Stdin = os.Stdin
	st, err := sc.Output()
	if err != nil {
		die("ic watch needs a terminal")
	}
	// The pane acts as the human, whatever shell it was started from.
	os.Unsetenv("IC_ID")
	os.Unsetenv("CLAUDECODE")
	w := &icWatch{stty: strings.TrimSpace(string(st)), results: make(chan string, 8)}
	ttyRun("-icanon", "-echo", "min", "1")
	fmt.Print("\x1b[?1049h\x1b[?25l")
	defer func() {
		fmt.Print("\x1b[?25h\x1b[?1049l")
		ttyRun(w.stty)
	}()

	keys := make(chan []byte, 16)
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				close(keys)
				return
			}
			keys <- append([]byte{}, buf[:n]...)
		}
	}()
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	tick := time.NewTicker(700 * time.Millisecond)
	defer tick.Stop()
	statusTick := 0

	w.size()
	w.reload(true)
	w.render()
	for {
		select {
		case k, ok := <-keys:
			if !ok || !w.key(k) {
				return
			}
		case s := <-sigs:
			if s != syscall.SIGWINCH {
				return
			}
			w.size()
		case r := <-w.results:
			w.msg = r
			w.reload(true)
		case <-tick.C:
			statusTick++
			sig := w.signature()
			if sig == w.sig && statusTick%4 != 0 { // herdr states every ~3s
				continue
			}
			w.reload(statusTick%4 == 0)
		}
		w.render()
	}
}

func ttyRun(args ...string) {
	c := exec.Command("stty", args...)
	c.Stdin = os.Stdin
	_ = c.Run()
}

func (w *icWatch) size() {
	c := exec.Command("stty", "size")
	c.Stdin = os.Stdin
	w.h, w.w = 24, 80
	if out, err := c.Output(); err == nil {
		fmt.Sscan(string(out), &w.h, &w.w)
	}
}

func (w *icWatch) signature() string {
	var b strings.Builder
	for _, d := range []string{"messages.log", "sessions", filepath.Join("inbox", "human")} {
		if info, err := os.Stat(filepath.Join(home(), d)); err == nil {
			fmt.Fprintf(&b, "%d-%d;", info.ModTime().UnixNano(), info.Size())
		}
	}
	return b.String()
}

func (w *icWatch) reload(withStatus bool) {
	selID := ""
	if w.sel < len(w.sess) {
		selID = w.sess[w.sel].ID
	}
	w.sig = w.signature()
	w.sess = append([]*Session{human}, sessions(false)...)
	if withStatus || w.status == nil {
		w.status = herdrStatuses()
	}
	w.unreadMe = len(unread("human"))
	w.sel = 0
	for k, s := range w.sess {
		if s.ID == selID {
			w.sel = k
		}
	}
}

// ---------------------------------------------------------------- keys

func (w *icWatch) key(b []byte) bool {
	s := string(b)
	if w.inputOn {
		switch {
		case s == "\x1b":
			w.inputOn, w.msg = false, "cancelled"
		case s == "\r" || s == "\n":
			w.inputOn = false
			if t := strings.TrimSpace(string(w.input)); t != "" {
				w.submit(t)
			}
		case s == "\x7f" || s == "\b":
			if len(w.input) > 0 {
				w.input = w.input[:len(w.input)-1]
			}
		case strings.HasPrefix(s, "\x1b"):
		default:
			for _, r := range strings.ReplaceAll(s, "\n", " ") {
				if r >= 32 {
					w.input = append(w.input, r)
				}
			}
		}
		return true
	}
	if w.overlay != nil {
		w.overlay = nil
		return true
	}
	cur := w.sess[w.sel]
	switch s {
	case "q", "\x03":
		return false
	case "\x1b[A", "k":
		if w.sel > 0 {
			w.sel--
		}
	case "\x1b[B", "j":
		if w.sel < len(w.sess)-1 {
			w.sel++
		}
	case "\r", "\n", "s":
		if cur.Kind == "human" {
			w.msg = "pick an agent to message"
			break
		}
		w.inputOn, w.prompt, w.input = true, "to "+cur.Name, nil
		to := cur.ID
		w.submit = func(t string) { w.run("send", to, t) }
	case "a":
		if cur.Kind == "human" {
			break
		}
		w.inputOn, w.prompt, w.input = true, "ask "+cur.Name+" (waits for the reply)", nil
		to := cur.ID
		w.submit = func(t string) { w.run("ask", to, t) }
	case "i":
		text := drain(human)
		if text == "" {
			text = "no unread messages"
		}
		w.overlay = strings.Split(strings.TrimRight(text, "\n"), "\n")
		w.reload(false)
	case "f":
		if bin := herdrBin(); bin != "" && cur.Pane != "" {
			_ = exec.Command(bin, "agent", "focus", cur.Pane).Run()
			w.msg = "focused " + cur.Name
		}
	case "r":
		w.reload(true)
	case "?":
		w.overlay = []string{
			"intercom · keys", "",
			"  ↑ ↓ / j k   pick a session",
			"  enter / s   message it (delivered at its next safe moment)",
			"  a           ask it and wait for the reply",
			"  i           read your messages",
			"  f           jump to its herdr pane",
			"  r  q        refresh, quit", "",
			"any key closes this",
		}
	}
	return true
}

func (w *icWatch) run(args ...string) {
	exe, _ := os.Executable()
	w.msg = args[0] + "…"
	go func() {
		full := append([]string{args[0], "--", args[1]}, args[2:]...)
		out, _ := exec.Command(exe, full...).CombinedOutput()
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		w.results <- strings.TrimPrefix(lines[len(lines)-1], "ic: ")
	}()
}

// ---------------------------------------------------------------- drawing

func cutTo(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func statusColor(st string) string {
	switch st {
	case "working":
		return cBlue
	case "blocked":
		return cRed
	case "done":
		return cGreen
	}
	return ""
}

func (w *icWatch) render() {
	var out []string
	add := func(color, plain string) { out = append(out, color+cutTo(plain, w.w)+cReset) }

	head := fmt.Sprintf(" intercom · %d session(s)", len(w.sess)-1)
	if w.unreadMe > 0 {
		head += fmt.Sprintf("   ✉ %d for you (i)", w.unreadMe)
	}
	add(cBold, head)
	add(cDim, strings.Repeat("─", w.w))
	if w.overlay != nil {
		for _, l := range w.overlay {
			if len(out) < w.h-1 {
				add("", " "+l)
			}
		}
		w.flush(out)
		return
	}

	h, _ := os.UserHomeDir()
	for k, s := range w.sess {
		st := w.status[s.Pane]
		if s.Kind == "human" {
			st = "you"
		} else if st == "" {
			st = "-"
		}
		cwd := strings.Replace(s.Cwd, h, "~", 1)
		line := fmt.Sprintf(" %-18s %-8s %-8s %s", cutTo(s.Name, 18), st, dash(s.Pane), cwd)
		color := statusColor(st)
		if k == w.sel {
			line = cutTo(line, w.w)
			line += strings.Repeat(" ", max(0, w.w-utf8.RuneCountInString(line)))
			color = cRev + color
		}
		add(color, line)
	}
	add(cDim, strings.Repeat("─", w.w))

	room := w.h - len(out) - 1
	for _, l := range w.messages(room) {
		out = append(out, l)
	}
	for len(out) < w.h-1 {
		out = append(out, "")
	}
	if w.inputOn {
		add(cOrange, " "+w.prompt+" › "+string(w.input)+"▏")
	} else {
		add(cDim, " ↑↓ pick · enter message · a ask · i inbox · f jump · ? help · q quit   "+w.msg)
	}
	w.flush(out)
}

// messages renders the last messages that fit, wrapped, newest at the bottom.
func (w *icWatch) messages(room int) []string {
	data, _ := os.ReadFile(filepath.Join(home(), "messages.log"))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	unreadIDs := map[string]bool{}
	for _, m := range unread("human") {
		unreadIDs[m.ID] = true
	}
	names := map[string]string{"human": "you"}
	for _, s := range w.sess {
		if s.Kind != "human" {
			names[s.ID] = s.Name
		}
	}
	name := func(id string) string {
		if n, ok := names[id]; ok {
			return n
		}
		if s := loadSession(id); s != nil {
			return s.Name
		}
		return short(id)
	}
	var out []string
	for k := len(lines) - 1; k >= 0 && len(out) < room; k-- {
		f := strings.SplitN(lines[k], "\t", 6)
		if len(f) != 6 {
			continue
		}
		hhmm := f[0]
		if len(hhmm) >= 16 {
			hhmm = hhmm[11:16]
		}
		head := fmt.Sprintf(" %s %s → %s", hhmm, name(f[1]), name(f[2]))
		if f[4] != "send" {
			head += " [" + f[4] + "]"
		}
		body := wrap(f[5], w.w-4)
		color := ""
		if unreadIDs[f[3]] {
			color = cOrange
		}
		block := []string{cDim + cutTo(head, w.w) + cReset}
		for _, b := range body {
			block = append(block, color+"   "+b+cReset)
		}
		if len(out)+len(block) > room {
			break
		}
		out = append(block, out...)
	}
	if len(out) == 0 {
		out = []string{cDim + " no messages yet" + cReset}
	}
	return out
}

func wrap(s string, width int) []string {
	if width < 10 {
		width = 10
	}
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		if utf8.RuneCountInString(line)+utf8.RuneCountInString(word)+1 > width && line != "" {
			out = append(out, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	if line != "" {
		out = append(out, line)
	}
	if len(out) > 4 {
		out = append(out[:4], "…")
	}
	return out
}

func (w *icWatch) flush(lines []string) {
	if len(lines) > w.h {
		lines = lines[:w.h]
	}
	fmt.Print("\x1b[H\x1b[2J" + strings.Join(lines, "\r\n"))
}
