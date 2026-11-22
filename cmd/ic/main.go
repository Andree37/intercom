// ic — intercom: agent sessions (and you) message each other.
//
// Every Claude Code session registers itself through a SessionStart hook.
// Messages are plain-text files in per-session inboxes. They reach an agent
// only at safe moments, through Claude Code hooks: between tool calls
// (PostToolUse), at the end of a turn (Stop), and when a prompt is sent
// (UserPromptSubmit). An idle agent in herdr gets one short nudge so the
// prompt hook fires. Nothing runs in the background.
package main

import (
	"fmt"
	"os"
	"strings"
)

type command struct {
	name, args, help string
	run              func(args []string)
}

var commands = []command{
	{"me", "", "print this session's id and name", cmdMe},
	{"who", "[--all]", "list sessions", cmdWho},
	{"name", "NAME", "rename this session", cmdName},
	{"send", "TO TEXT...", "send a message (TO: name, id, id prefix, pane:<id>, tab, human)", cmdSend},
	{"ask", "TO TEXT...", "send a question and wait for the reply (--timeout SECONDS, default 100); TO all asks every session", cmdAsk},
	{"done", "[QID]", "close a question asked to all: unseen copies vanish, no more replies; no QID lists open ones", cmdDone},
	{"reply", "MSGID TEXT...", "answer a message", cmdReply},
	{"inbox", "[--peek]", "print unread messages and mark them read", cmdInbox},
	{"log", "[N]", "last N messages to or from this session", cmdLog},
	{"watch", "", "live pane: sessions, all messages, your inbox; message anyone", cmdWatch},
	{"hook", "EVENT", "Claude Code hook entry (session-start, prompt, tool, stop, session-end)", cmdHook},
	{"install", "claude", "add ic hooks to ~/.claude/settings.json (needs jq)", cmdInstall},
}

func usage() {
	fmt.Print("ic — intercom: agent sessions message each other\n\nusage: ic <command> [args]\n\n")
	for _, c := range commands {
		fmt.Printf("  %-8s %-16s %s\n", c.name, c.args, c.help)
	}
	fmt.Print("\nState lives in $IC_HOME (default ~/.local/state/intercom), as plain text.\n")
}

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ic: "+format+"\n", a...)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help" {
		usage()
		return
	}
	for _, c := range commands {
		if c.name == os.Args[1] {
			c.run(os.Args[2:])
			return
		}
	}
	die("unknown command %q (see `ic --help`)", os.Args[1])
}

// flag pulls "--name value" or "--name" (bool) out of args. It stops at "--"
// and keeps it, so a later flag call stops there too; positional drops it once
// every flag is read.
func flag(args []string, name string, takesValue bool) (string, bool, []string) {
	var rest []string
	val, found := "", false
	for k := 0; k < len(args); k++ {
		if args[k] == "--" {
			rest = append(rest, args[k:]...)
			break
		}
		if args[k] == "--"+name {
			found = true
			if takesValue && k+1 < len(args) {
				val = args[k+1]
				k++
			}
			continue
		}
		rest = append(rest, args[k])
	}
	return val, found, rest
}

// positional is what is left after the flags, with the first "--" removed:
// everything after it is text, even "--no-wake".
func positional(args []string) []string {
	for k, a := range args {
		if a == "--" {
			return append(append([]string{}, args[:k]...), args[k+1:]...)
		}
	}
	return args
}

func joinText(args []string) string { return strings.TrimSpace(strings.Join(args, " ")) }
