package main

import (
	"strings"
	"testing"
)

// testPane is a pane with two agents, bob selected, that records the ic
// commands it would run instead of running them.
func testPane() (*icWatch, *[][]string) {
	ran := &[][]string{}
	w := &icWatch{
		sess: []*Session{human, {ID: "b", Name: "bob", Kind: "claude"}, {ID: "c", Name: "cat", Kind: "claude"}},
		sel:  1, open: map[string]bool{},
	}
	w.runner = func(args ...string) { *ran = append(*ran, args) }
	return w, ran
}

// drive feeds reads to the pane through the parser, then lets any held input
// time out. It reports whether the pane is still open.
func drive(w *icWatch, reads ...string) bool {
	var in inputParser
	handle := func(evs []inputEvent) bool {
		for _, ev := range evs {
			if ev.isPaste {
				w.paste(ev.paste)
			} else if !w.key([]byte(ev.key)) {
				return false
			}
		}
		return true
	}
	for _, r := range reads {
		if !handle(in.push([]byte(r))) {
			return false
		}
	}
	return handle(in.flush())
}

// chunks splits s every n bytes, like a terminal handing a paste over in reads.
func chunks(s string, n int) []string {
	var out []string
	for len(s) > n {
		out = append(out, s[:n])
		s = s[n:]
	}
	return append(out, s)
}

// A multi-line paste into an open prompt is text however the reads split it:
// no early send, no hotkeys (q quits, d closes a question, A asks everyone),
// one message with the line breaks as spaces.
func TestPasteSplitAnywhereStaysText(t *testing.T) {
	paste := pasteStart + strings.Repeat("x", 62) + "\nquit the old flag\n\ndo it Aé 🎉\n" + pasteEnd
	for n := 1; n <= len(paste); n++ {
		w, ran := testPane()
		drive(w, "\r") // open the prompt to bob
		reads := append(chunks(paste, n), "\r")
		if !drive(w, reads...) {
			t.Fatalf("split every %d bytes: the pane closed", n)
		}
		want := strings.Repeat("x", 62) + " quit the old flag do it Aé 🎉"
		if len(*ran) != 1 || (*ran)[0][0] != "send" || (*ran)[0][2] != want {
			t.Fatalf("split every %d bytes: ran %q", n, *ran)
		}
	}
}

// A real Enter always sends, even when the next keys share its read.
func TestEnterSendsEvenWithKeysInTheSameRead(t *testing.T) {
	w, ran := testPane()
	drive(w, "\r")
	drive(w, "ok\rjs")
	if len(*ran) != 1 || (*ran)[0][2] != "ok" || (*ran)[0][1] != "b" {
		t.Fatalf("ran %q", *ran)
	}
	if !w.inputOn || w.prompt != "to cat" { // j moved to cat, s opened a prompt to it
		t.Fatalf("after Enter, j and s: inputOn=%v prompt=%q", w.inputOn, w.prompt)
	}
}

// A paste with no prompt open never runs as keys: on an agent it becomes a
// draft to that agent, elsewhere it is ignored.
func TestPasteWithNoPromptIsNeverKeys(t *testing.T) {
	w, ran := testPane()
	if !drive(w, pasteStart+"do it\nquick question: ok?\n"+pasteEnd) {
		t.Fatal("the pane closed")
	}
	if len(*ran) != 0 || !w.inputOn || string(w.input) != "do it quick question: ok?" {
		t.Fatalf("ran %q, inputOn=%v, draft %q", *ran, w.inputOn, string(w.input))
	}
	w, ran = testPane()
	w.sel = 0 // human
	if !drive(w, pasteStart+"q\n"+pasteEnd) || len(*ran) != 0 || w.inputOn {
		t.Fatalf("paste on human: ran %q, inputOn=%v", *ran, w.inputOn)
	}
}

// Keys that start with Esc are one key, not an Esc plus letters: Option+arrow,
// F1, an arrow cut by a read. Only an Esc with nothing after it is Esc.
func TestEscapeSequencesAreWholeKeys(t *testing.T) {
	w, _ := testPane()
	drive(w, "\r", "draft")
	for _, k := range []string{"\x1bb", "\x1bf", "\x1bOP", "\x1b[15~", "\x1b\x7f"} {
		if !drive(w, k) || !w.inputOn || string(w.input) != "draft" {
			t.Fatalf("%q: inputOn=%v draft=%q", k, w.inputOn, string(w.input))
		}
	}
	w, _ = testPane()
	if !drive(w, "\x1bOP") || !drive(w, "\x1b", "[A") || w.sel != 0 { // F1 does nothing, split Up moves up
		t.Fatalf("F1 then a split Up: sel=%d", w.sel)
	}
	if drive(w, "\x1b") { // a lone Esc still closes the pane
		t.Fatal("lone Esc did not close the pane")
	}
}

func TestCleanStripsTerminalControl(t *testing.T) {
	got := clean("hi \x1b]52;c;ZWNobw==\x07 \x1b[2J\u009b31m\tthere\nnext")
	if strings.ContainsAny(got, "\x1b\x07\u009b\t") || !strings.Contains(got, "hi ") || !strings.Contains(got, "\nnext") {
		t.Fatalf("clean = %q", got)
	}
}

func TestPasteTextWhitespace(t *testing.T) {
	for in, want := range map[string]string{
		"para one\n\npara two\n":        "para one para two",
		"name\tvalue\r\nport\t8080\r\n": "name value port 8080",
	} {
		if got := pasteText(in); got != want {
			t.Errorf("pasteText(%q) = %q, want %q", in, got, want)
		}
	}
}

// A message that looks like a flag is still a message.
func TestFlagLikeTextAfterDoubleDash(t *testing.T) {
	args := []string{"--", "bob", "--no-wake"}
	_, noWake, rest := flag(args, "no-wake", false)
	if noWake || strings.Join(positional(rest), " ") != "bob --no-wake" {
		t.Fatalf("noWake=%v rest=%q", noWake, positional(rest))
	}
	_, _, rest = flag([]string{"--", "bob", "--"}, "topic", true)
	if strings.Join(positional(rest), " ") != "bob --" {
		t.Fatalf("rest=%q", positional(rest))
	}
}

// The end of a long draft, where the cursor is, stays in view.
func TestPromptShowsTheEndOfALongDraft(t *testing.T) {
	line := promptLine("to bob", []rune(strings.Repeat("a", 200)+" END"), 80)
	if !strings.HasSuffix(line, "END▏") || len([]rune(line)) > 80 {
		t.Fatalf("prompt line %q", line)
	}
	line = promptLine("ask every session (close with d once answered)", []rune("which port?"), 50)
	if !strings.Contains(line, "which port?") {
		t.Fatalf("narrow prompt line %q", line)
	}
}
