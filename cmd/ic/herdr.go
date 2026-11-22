package main

// herdr: wake idle agents and notify the human. Everything here is optional;
// without herdr, messages simply wait for the recipient's next hook.

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
)

func herdrBin() string {
	if os.Getenv("HERDR_ENV") == "" && os.Getenv("HERDR_SOCKET_PATH") == "" {
		return ""
	}
	if b := os.Getenv("HERDR_BIN_PATH"); b != "" {
		return b
	}
	if b, err := exec.LookPath("herdr"); err == nil {
		return b
	}
	return ""
}

// herdrStatuses maps pane id → agent status (idle, working, blocked, done).
func herdrStatuses() map[string]string {
	out := map[string]string{}
	bin := herdrBin()
	if bin == "" {
		return out
	}
	data, err := exec.Command(bin, "agent", "list").Output()
	if err != nil {
		return out
	}
	var res struct {
		Result struct {
			Agents []struct {
				PaneID string `json:"pane_id"`
				Status string `json:"agent_status"`
			} `json:"agents"`
		} `json:"result"`
	}
	if json.Unmarshal(data, &res) == nil {
		for _, a := range res.Result.Agents {
			out[a.PaneID] = a.Status
		}
	}
	return out
}

// nudge wakes the recipient when that cannot interrupt anything, and says how
// the message will arrive.
func nudge(dst, from *Session) string {
	bin := herdrBin()
	if dst.Kind == "human" {
		if bin != "" {
			body := strings.TrimSpace(oneLine(lastBody(dst)))
			if len(body) > 200 {
				body = body[:200] + "…"
			}
			_ = exec.Command(bin, "notification", "show", "intercom · "+from.Name, "--body", body, "--sound", "request").Run()
			return "notified you in herdr (read with `ic inbox`)"
		}
		return "read with `ic inbox`"
	}
	if bin == "" || dst.Pane == "" {
		return "queued: delivered at their next tool call, turn end or prompt"
	}
	switch herdrStatuses()[dst.Pane] {
	case "idle", "done":
		text := "[intercom] New message from " + from.Name + ". It is shown with this prompt; read it and act on it."
		if exec.Command(bin, "agent", "prompt", dst.Pane, text).Run() == nil {
			return "they were idle, so herdr woke them"
		}
		return "queued (waking them failed)"
	case "working":
		return "they are working: delivered after their current tool call"
	case "blocked":
		return "they are waiting on a dialog: delivered once it is answered"
	}
	return "queued: delivered at their next tool call, turn end or prompt"
}

func lastBody(dst *Session) string {
	msgs := unread(dst.ID)
	if len(msgs) == 0 {
		return ""
	}
	return msgs[len(msgs)-1].Body
}
