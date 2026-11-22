package main

// Plain-text state:
//
//	sessions/<id>                 "key value" lines, one session each
//	inbox/<id>/<msg>.msg          unread messages ("key value" header, blank line, body)
//	inbox/<id>/read/<msg>.msg     delivered messages
//	questions/<id>                one question asked to everyone ("key value" header, blank line, body)
//	messages.log                  one tab-separated line per message, for `ic log`

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func home() string {
	if h := os.Getenv("IC_HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "state", "intercom")
}

func now() string { return time.Now().Format("2006-01-02 15:04:05") }

// ---------------------------------------------------------------- key/value files

func readKV(path string) (map[string]string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	head, body, _ := strings.Cut(string(data), "\n\n")
	kv := map[string]string{}
	for _, line := range strings.Split(head, "\n") {
		if k, v, ok := strings.Cut(line, " "); ok {
			kv[k] = v
		}
	}
	return kv, body, nil
}

func writeKV(path string, keys []string, kv map[string]string, body string) error {
	var b strings.Builder
	for _, k := range keys {
		if v := kv[k]; v != "" {
			fmt.Fprintf(&b, "%s %s\n", k, oneLine(v))
		}
	}
	if body != "" {
		b.WriteString("\n" + body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func oneLine(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", " ") }

// ---------------------------------------------------------------- sessions

type Session struct {
	ID, Name, Kind, Cwd, Pane, Tab, Workspace, Started, Seen, Ended string
}

var sessionKeys = []string{"id", "name", "kind", "cwd", "pane", "tab", "workspace", "started", "seen", "ended"}

var human = &Session{ID: "human", Name: "human", Kind: "human"}

func sessionPath(id string) string { return filepath.Join(home(), "sessions", id) }

func loadSession(id string) *Session {
	if id == "human" {
		return human
	}
	kv, _, err := readKV(sessionPath(filepath.Base(id)))
	if err != nil {
		return nil
	}
	return &Session{kv["id"], kv["name"], kv["kind"], kv["cwd"], kv["pane"], kv["tab"], kv["workspace"], kv["started"], kv["seen"], kv["ended"]}
}

func (s *Session) save() error {
	if s.ID == "human" {
		return nil
	}
	kv := map[string]string{"id": s.ID, "name": s.Name, "kind": s.Kind, "cwd": s.Cwd, "pane": s.Pane, "tab": s.Tab,
		"workspace": s.Workspace, "started": s.Started, "seen": s.Seen, "ended": s.Ended}
	return writeKV(sessionPath(s.ID), sessionKeys, kv, "")
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (s *Session) label() string {
	if s.Kind == "human" {
		return "human"
	}
	return fmt.Sprintf("%s (%s %s)", s.Name, s.Kind, short(s.ID))
}

// sessions lists known sessions, newest activity first.
func sessions(all bool) []*Session {
	entries, _ := os.ReadDir(filepath.Join(home(), "sessions"))
	var out []*Session
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		if s := loadSession(e.Name()); s != nil && (all || s.Ended == "") {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Seen > out[b].Seen })
	return out
}

// self is the session this process belongs to: $IC_ID (set by the
// SessionStart hook), else the live agent session in this herdr pane.
// Outside any agent session it is the human.
func self() *Session {
	if id := os.Getenv("IC_ID"); id != "" {
		if s := loadSession(id); s != nil {
			return s
		}
	}
	if pane := os.Getenv("HERDR_PANE_ID"); pane != "" && os.Getenv("CLAUDECODE") == "1" {
		for _, s := range sessions(false) {
			if s.Pane == pane {
				return s
			}
		}
	}
	return human
}

// resolve finds a recipient: human, pane:<id>, tab, an exact id, a name, or an id prefix.
func resolve(to string, me *Session) (*Session, error) {
	live := sessions(false)
	pick := func(hits []*Session, what string) (*Session, error) {
		switch len(hits) {
		case 1:
			return hits[0], nil
		case 0:
			return nil, fmt.Errorf("no live session matches %s (see `ic who`)", what)
		}
		var names []string
		for _, h := range hits {
			names = append(names, h.label())
		}
		return nil, fmt.Errorf("%s is ambiguous: %s", what, strings.Join(names, ", "))
	}
	switch {
	case strings.EqualFold(to, "human"), to == "me" && me.Kind == "human":
		return human, nil
	case strings.HasPrefix(to, "pane:"):
		var hits []*Session
		for _, s := range live {
			if s.Pane == strings.TrimPrefix(to, "pane:") {
				hits = append(hits, s)
			}
		}
		return pick(hits, to)
	case to == "tab":
		tab := os.Getenv("HERDR_TAB_ID")
		if tab == "" {
			return nil, fmt.Errorf("`tab` only works inside herdr")
		}
		var hits []*Session
		for _, s := range live {
			if s.Tab == tab && s.ID != me.ID && s.Kind != "human" {
				hits = append(hits, s)
			}
		}
		return pick(hits, "an agent in this tab")
	}
	if s := loadSession(to); s != nil {
		return s, nil
	}
	var byName, byPrefix []*Session
	for _, s := range live {
		if strings.EqualFold(s.Name, to) {
			byName = append(byName, s)
		}
		if len(to) >= 4 && strings.HasPrefix(s.ID, to) {
			byPrefix = append(byPrefix, s)
		}
	}
	if len(byName) > 0 {
		return pick(byName, "name "+to)
	}
	return pick(byPrefix, to)
}

// ---------------------------------------------------------------- messages

type Message struct {
	ID, From, To, Time, Kind, Re, Topic, Question string
	Body                                          string
}

var messageKeys = []string{"id", "from", "to", "time", "kind", "re", "topic", "question"}

func newID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

func inboxDir(id string) string { return filepath.Join(home(), "inbox", id) }

func (m *Message) deliver() error {
	if err := m.store(); err != nil {
		return err
	}
	m.log(m.To)
	return nil
}

// store writes the message into its recipient's inbox without logging it.
func (m *Message) store() error {
	kv := map[string]string{"id": m.ID, "from": m.From, "to": m.To, "time": m.Time, "kind": m.Kind, "re": m.Re, "topic": m.Topic, "question": m.Question}
	return writeKV(filepath.Join(inboxDir(m.To), m.ID+".msg"), messageKeys, kv, m.Body)
}

// log appends the message to messages.log, addressed to `to` (an id, or "all").
func (m *Message) log(to string) {
	f, err := os.OpenFile(filepath.Join(home(), "messages.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		fmt.Fprintf(f, "%s\t%s\t%s\t%s\t%s\t%s\n", m.Time, m.From, to, m.ID, m.Kind, oneLine(m.Body))
		f.Close()
	}
}

func readMessage(path string) *Message {
	kv, body, err := readKV(path)
	if err != nil {
		return nil
	}
	return &Message{kv["id"], kv["from"], kv["to"], kv["time"], kv["kind"], kv["re"], kv["topic"], kv["question"], strings.TrimRight(body, "\n")}
}

// unread lists a session's unread messages, oldest first.
func unread(id string) []*Message {
	entries, _ := os.ReadDir(inboxDir(id))
	var out []*Message
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".msg") {
			if m := readMessage(filepath.Join(inboxDir(id), e.Name())); m != nil {
				out = append(out, m)
			}
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

func markRead(m *Message) {
	dir := inboxDir(m.To)
	_ = os.MkdirAll(filepath.Join(dir, "read"), 0o755)
	_ = os.Rename(filepath.Join(dir, m.ID+".msg"), filepath.Join(dir, "read", m.ID+".msg"))
}

// drain marks a session's unread messages read and returns them as text for an agent.
func drain(s *Session) string {
	msgs := unread(s.ID)
	if len(msgs) == 0 {
		return ""
	}
	var live []*Message
	for _, m := range msgs {
		if m.Question != "" && m.Kind == "ask" && questionClosed(m.Question) {
			_ = os.Remove(filepath.Join(inboxDir(m.To), m.ID+".msg")) // answered elsewhere: never shown
			continue
		}
		live = append(live, m)
	}
	if len(live) == 0 {
		return ""
	}
	msgs = live
	var b strings.Builder
	fmt.Fprintf(&b, "[intercom] %d new message(s) for you (%s):\n", len(msgs), s.Name)
	for _, m := range msgs {
		from := m.From
		if fs := loadSession(m.From); fs != nil {
			from = fs.label()
		}
		hhmm := m.Time
		if len(hhmm) >= 16 {
			hhmm = hhmm[11:16]
		}
		fmt.Fprintf(&b, "\n— from %s at %s · msg %s · %s", from, hhmm, m.ID, m.Kind)
		if m.Topic != "" {
			fmt.Fprintf(&b, " · topic %s", m.Topic)
		}
		if m.Question != "" && m.Kind == "reply" {
			fmt.Fprintf(&b, " · answers question %s (close it: ic done %s)", m.Question, m.Question)
		}
		fmt.Fprintf(&b, "\n%s\n", m.Body)
		switch {
		case m.Kind == "ask" && m.Question != "":
			fmt.Fprintf(&b, "→ Asked to every session (question %s). Answer with: ic reply %s \"...\" "+
				"If it gets closed before you answer, you'll be told; then drop it.\n", m.Question, m.ID)
		case m.Kind == "ask":
			fmt.Fprintf(&b, "→ They are waiting for an answer: ic reply %s \"...\"\n", m.ID)
		case m.Kind == "send":
			fmt.Fprintf(&b, "→ Reply if useful: ic reply %s \"...\"\n", m.ID)
		}
		markRead(m)
	}
	return b.String()
}
