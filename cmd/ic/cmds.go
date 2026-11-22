package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func cmdMe(args []string) {
	s := self()
	if s.Kind == "human" {
		fmt.Println("human")
		return
	}
	fmt.Printf("%s %s\n", s.ID, s.Name)
}

func cmdWho(args []string) {
	_, all, _ := flag(args, "all", false)
	me := self()
	status := herdrStatuses()
	for _, s := range sessions(all) {
		mark := ""
		if s.ID == me.ID {
			mark = " (you)"
		}
		st := status[s.Pane]
		if st == "" {
			st = "-"
		}
		if s.Ended != "" {
			st = "ended"
		}
		cwd := s.Cwd
		if h, _ := os.UserHomeDir(); h != "" {
			cwd = strings.Replace(cwd, h, "~", 1)
		}
		fmt.Printf("%-22s %-7s %-8s %-9s %-8s %s%s\n", s.Name, s.Kind, short(s.ID), st, dash(s.Pane), cwd, mark)
	}
	if n := len(unread("human")); n > 0 {
		fmt.Printf("\nhuman has %d unread message(s): ic inbox\n", n)
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func cmdName(args []string) {
	name := joinText(args)
	if name == "" || strings.ContainsAny(name, " \t") {
		die("usage: ic name NAME (one word)")
	}
	s := self()
	if s.Kind == "human" {
		die("not inside an agent session")
	}
	for _, o := range sessions(false) {
		if o.ID != s.ID && strings.EqualFold(o.Name, name) {
			die("%s is taken by %s", name, o.label())
		}
	}
	s.Name = name
	if err := s.save(); err != nil {
		die("%v", err)
	}
	fmt.Println(name)
}

// send writes a message and wakes the recipient if that is safe.
func send(to, kind, re, topic, question, body string, wake bool) *Message {
	if strings.TrimSpace(body) == "" {
		die("empty message")
	}
	me := self()
	dst, err := resolve(to, me)
	if err != nil {
		die("%v", err)
	}
	if dst.ID == me.ID {
		die("that is you")
	}
	m := &Message{ID: newID(), From: me.ID, To: dst.ID, Time: now(), Kind: kind, Re: re, Topic: topic, Question: question, Body: body}
	if err := m.deliver(); err != nil {
		die("%v", err)
	}
	how := "queued: it reaches them at their next safe moment"
	if wake {
		how = nudge(dst, me)
	}
	fmt.Printf("sent %s to %s · %s\n", m.ID, dst.label(), how)
	return m
}

func cmdSend(args []string) {
	topic, _, args := flag(args, "topic", true)
	_, noWake, args := flag(args, "no-wake", false)
	if len(args) < 2 {
		die("usage: ic send TO TEXT... [--topic T] [--no-wake]")
	}
	if strings.EqualFold(args[0], "all") {
		die("to reach everyone, ask a question: ic ask all TEXT...")
	}
	send(args[0], "send", "", topic, "", joinText(args[1:]), !noWake)
}

func cmdAsk(args []string) {
	topic, _, args := flag(args, "topic", true)
	tv, _, args := flag(args, "timeout", true)
	_, noWake, args := flag(args, "no-wake", false)
	if len(args) < 2 {
		die("usage: ic ask TO TEXT... [--timeout SECONDS] [--topic T]")
	}
	if strings.EqualFold(args[0], "all") { // answers arrive as messages; close with `ic done`
		askAll(joinText(args[1:]), topic, !noWake)
		return
	}
	timeout := 100 // stays under an agent's default 2-minute tool limit
	if n, err := strconv.Atoi(tv); err == nil && n > 0 {
		timeout = n
	}
	q := send(args[0], "ask", "", topic, "", joinText(args[1:]), !noWake)
	me := self()
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range unread(me.ID) {
			if m.Re == q.ID {
				markRead(m)
				fmt.Printf("\nreply from %s:\n%s\n", nameOf(m.From), m.Body)
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Printf("\nno reply within %ds. It will arrive like any message (re %s).\n", timeout, q.ID)
}

func nameOf(id string) string {
	if s := loadSession(id); s != nil {
		return s.label()
	}
	return id
}

func cmdReply(args []string) {
	if len(args) < 2 {
		die("usage: ic reply MSGID TEXT...")
	}
	me := self()
	id := filepath.Base(args[0])
	var orig *Message
	for _, p := range []string{filepath.Join(inboxDir(me.ID), id+".msg"), filepath.Join(inboxDir(me.ID), "read", id+".msg")} {
		if m := readMessage(p); m != nil {
			orig = m
			break
		}
	}
	if orig == nil {
		die("no message %s in your inbox", id)
	}
	if orig.Question != "" && questionClosed(orig.Question) {
		die("question %s is closed: it was already answered. No reply needed; drop it.", orig.Question)
	}
	send(orig.From, "reply", orig.ID, orig.Topic, orig.Question, joinText(args[1:]), true)
}

func cmdInbox(args []string) {
	_, peek, _ := flag(args, "peek", false)
	me := self()
	if peek {
		for _, m := range unread(me.ID) {
			fmt.Printf("%s  from %s  %s\n", m.ID, nameOf(m.From), oneLine(m.Body))
		}
		return
	}
	if text := drain(me); text != "" {
		fmt.Print(text)
	} else {
		fmt.Println("no unread messages")
	}
}

func cmdLog(args []string) {
	n := 20
	if len(args) > 0 {
		if k, err := strconv.Atoi(args[0]); err == nil {
			n = k
		}
	}
	data, _ := os.ReadFile(filepath.Join(home(), "messages.log"))
	me := self()
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		f := strings.SplitN(l, "\t", 6)
		if len(f) == 6 && (f[1] == me.ID || f[2] == me.ID) {
			lines = append(lines, fmt.Sprintf("%s  %s → %s  [%s] %s", f[0][11:16], nameOf(f[1]), nameOf(f[2]), f[4], f[5]))
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	fmt.Println(strings.Join(lines, "\n"))
}
