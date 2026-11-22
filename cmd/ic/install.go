package main

// `ic install claude` merges ic's hooks into ~/.claude/settings.json with jq,
// which keeps the file's existing order and content. A backup is written first.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const installFilter = `
def ic_entry($e): {hooks: [{type: "command", command: $e.cmd, timeout: 10}]}
  + (if $e.event == "PostToolUse" then {matcher: "*"} else {} end);
.hooks //= {}
| reduce $evs[] as $e (.;
    .hooks[$e.event] = (
      (.hooks[$e.event] // [])
      | map(.hooks |= map(select((.command // "") | test("(^|/)ic hook ") | not)))
      | map(select((.hooks | length) > 0))
    ) + [ic_entry($e)]
  )`

func cmdInstall(args []string) {
	if len(args) == 0 || args[0] != "claude" {
		die("usage: ic install claude")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		die("needs jq (brew install jq)")
	}
	exe, err := os.Executable()
	if err != nil {
		die("%v", err)
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		die("%v", err)
	}
	if strings.Contains(exe, "go-build") {
		die("run an installed ic (go install ./cmd/ic), not `go run`")
	}
	h, _ := os.UserHomeDir()
	path := filepath.Join(h, ".claude", "settings.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data = []byte("{}")
	} else if err != nil {
		die("%v", err)
	}
	backup := fmt.Sprintf("%s.bak-ic-%s", path, time.Now().Format("20060102-150405"))
	if err := os.WriteFile(backup, data, 0o644); err != nil {
		die("%v", err)
	}
	var evs []string
	for _, e := range [][2]string{{"SessionStart", "session-start"}, {"UserPromptSubmit", "prompt"},
		{"PostToolUse", "tool"}, {"Stop", "stop"}, {"SessionEnd", "session-end"}} {
		evs = append(evs, fmt.Sprintf(`{"event":%q,"cmd":%q}`, e[0], exe+" hook "+e[1]))
	}
	cmd := exec.Command("jq", "--argjson", "evs", "["+strings.Join(evs, ",")+"]", installFilter)
	cmd.Stdin = strings.NewReader(string(data))
	out, err := cmd.Output()
	if err != nil {
		die("jq failed: %v", err)
	}
	if err := os.WriteFile(path+".tmp", out, 0o644); err != nil {
		die("%v", err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		die("%v", err)
	}
	fmt.Printf("added ic hooks to %s (backup: %s)\nNew Claude sessions pick them up; restart running ones to join.\n", path, backup)
}
