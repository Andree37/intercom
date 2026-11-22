package main

import (
	"strings"
	"testing"
)

// A paste with line breaks into the message prompt stays text: the first line
// must not submit early, and nothing after it may run as a hotkey (q quits,
// a/i/f/s act on the selected session).
func TestPasteNeverBecomesHotkeys(t *testing.T) {
	var ran []string
	typing := true
	var input []string
	key := func(k string) bool {
		if typing {
			if k == "\r" || k == "\n" {
				typing = false // submitted
				return true
			}
			input = append(input, k)
			return true
		}
		ran = append(ran, k) // a hotkey
		return true
	}
	feed([]byte("Use option B\nquit the old flag\r"), func() bool { return typing }, key)
	if len(ran) != 0 {
		t.Fatalf("pasted text ran as hotkeys: %q", ran)
	}
	if got := strings.Join(input, ""); got != "Use option B quit the old flag" {
		t.Fatalf("input = %q", got)
	}
	// Enter followed by more keys in the same read: the rest is dropped.
	typing, ran, input = true, nil, nil
	feed([]byte("yes\rqa"), func() bool { return typing }, key)
	if len(ran) != 0 {
		t.Fatalf("keys after Enter ran: %q", ran)
	}
}

// The real pane: a pasted multi-line message keeps the pane open and sends
// one message whose body has the line break turned into a space.
func TestWatchPasteSubmitsOnce(t *testing.T) {
	w := &icWatch{sess: []*Session{human, {ID: "x", Name: "x", Kind: "claude"}}, sel: 1, open: map[string]bool{}}
	var sent []string
	w.key([]byte("\r")) // open the prompt for session x
	w.submit = func(t string) { sent = append(sent, t) }
	alive := feed([]byte("first line\nq then a\r"), func() bool { return w.inputOn }, func(k string) bool { return w.key([]byte(k)) })
	if !alive {
		t.Fatal("pasting a q closed the pane")
	}
	if len(sent) != 1 || sent[0] != "first line q then a" {
		t.Fatalf("sent = %q", sent)
	}
}
