package main

// Claude Code hooks. Input arrives as JSON on stdin (Claude Code's format, not ours).
// Hooks never fail loudly: any problem means "no messages".

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type hookInput struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Source    string `json:"source"`
}

func cmdHook(args []string) {
	if len(args) == 0 || os.Getenv("IC_SKIP") == "1" { // helper sessions (e.g. code-tree's classifier) stay out
		return
	}
	var in hookInput
	data, _ := io.ReadAll(os.Stdin)
	_ = json.Unmarshal(data, &in)
	if in.SessionID == "" {
		return
	}
	s := loadSession(in.SessionID)

	switch args[0] {
	case "session-start":
		s = register(in, s)
		fmt.Printf("[intercom] You are %s (id %s). Other sessions and the human can message you; "+
			"messages appear in your context automatically. `ic who` lists sessions, "+
			"`ic send <name> \"...\"` and `ic ask <name> \"...\"` reach them, `ic reply <msg> \"...\"` answers.\n",
			s.Name, short(s.ID))
		fmt.Print(drain(s))
	case "prompt":
		if s = touch(in, s); s != nil {
			fmt.Print(drain(s))
		}
	case "tool":
		if s = touch(in, s); s != nil {
			if text := drain(s); text != "" {
				emit(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": text}})
			}
		}
	case "stop":
		if s = touch(in, s); s != nil {
			if text := drain(s); text != "" {
				emit(map[string]any{"decision": "block", "reason": text + "\nHandle these messages, then finish your turn."})
			}
		}
	case "session-end":
		if s != nil {
			s.Ended = now()
			_ = s.save()
		}
	}
}

func emit(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

// register creates or refreshes this session and exports IC_ID to its shell.
func register(in hookInput, s *Session) *Session {
	if s == nil {
		s = &Session{ID: in.SessionID, Kind: "claude", Started: now()}
		s.Name = uniqueName(filepath.Base(in.Cwd))
	}
	s.Cwd, s.Seen, s.Ended = in.Cwd, now(), ""
	s.Pane, s.Tab, s.Workspace = os.Getenv("HERDR_PANE_ID"), os.Getenv("HERDR_TAB_ID"), os.Getenv("HERDR_WORKSPACE_ID")
	_ = s.save()
	if f := os.Getenv("CLAUDE_ENV_FILE"); f != "" {
		if fh, err := os.OpenFile(f, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(fh, "export IC_ID=%s\n", s.ID)
			fh.Close()
		}
	}
	return s
}

// touch keeps the session's last-seen time and herdr location current.
func touch(in hookInput, s *Session) *Session {
	if s == nil {
		return register(in, nil)
	}
	s.Seen, s.Ended = now(), ""
	if p := os.Getenv("HERDR_PANE_ID"); p != "" {
		s.Pane, s.Tab, s.Workspace = p, os.Getenv("HERDR_TAB_ID"), os.Getenv("HERDR_WORKSPACE_ID")
	}
	_ = s.save()
	return s
}

func uniqueName(base string) string {
	if base == "" || base == "." || base == "/" {
		base = "session"
	}
	taken := map[string]bool{"human": true}
	for _, s := range sessions(false) {
		taken[s.Name] = true
	}
	name := base
	for k := 2; taken[name]; k++ {
		name = fmt.Sprintf("%s-%d", base, k)
	}
	return name
}
