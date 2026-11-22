package main

// Terminal input for `ic watch`: raw bytes in, whole keys and whole pastes out.
//
// A read can end anywhere: inside an escape sequence, inside a multi-byte
// character, or in the middle of a paste. The parser keeps whatever is
// incomplete and finishes it with the next read, so where the bytes happen to
// be split never changes what a key or a paste means.
//
// Pastes are told apart from typing by bracketed paste (the pane turns it on
// with \x1b[?2004h): the terminal wraps pasted text in \x1b[200~ ... \x1b[201~.
// A line break inside a paste is text; a line break outside one is Enter.

import (
	"bytes"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	pasteStart = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
	// A lone Esc is only an Esc once nothing follows it for this long;
	// otherwise it is the start of an arrow, function or Alt key.
	escWait = 30 * time.Millisecond
	// A paste whose end marker never comes is taken as finished after this.
	pasteWait = time.Second
)

// inputEvent is one key ("q", "\r", "\x1b[A", "\x1bb") or one whole paste.
type inputEvent struct {
	key     string
	paste   string
	isPaste bool
}

type inputParser struct {
	buf     []byte
	inPaste bool
	pasted  []byte
}

// pending says whether input is held back, waiting for more bytes, and how long
// to wait before taking it as it is (see flush).
func (p *inputParser) pending() (bool, time.Duration) {
	if p.inPaste {
		return true, pasteWait
	}
	return len(p.buf) > 0, escWait
}

// push adds one read and returns every event it completes.
func (p *inputParser) push(b []byte) []inputEvent {
	p.buf = append(p.buf, b...)
	return p.parse(false)
}

// flush is called when no more bytes came within the wait: a held Esc is a
// real Esc, a cut-off sequence is taken as is, an unfinished paste is done.
func (p *inputParser) flush() []inputEvent {
	return p.parse(true)
}

func (p *inputParser) parse(final bool) []inputEvent {
	var out []inputEvent
	for {
		if p.inPaste {
			if i := bytes.Index(p.buf, []byte(pasteEnd)); i >= 0 {
				p.pasted = append(p.pasted, p.buf[:i]...)
				p.buf = p.buf[i+len(pasteEnd):]
				out = append(out, inputEvent{paste: string(p.pasted), isPaste: true})
				p.inPaste, p.pasted = false, nil
				continue
			}
			if final {
				p.pasted = append(p.pasted, p.buf...)
				p.buf = nil
				out = append(out, inputEvent{paste: string(p.pasted), isPaste: true})
				p.inPaste, p.pasted = false, nil
				return out
			}
			// Keep a possible start of the end marker; the rest is pasted text.
			keep := 0
			for k := len(pasteEnd) - 1; k > 0; k-- {
				if bytes.HasSuffix(p.buf, []byte(pasteEnd[:k])) {
					keep = k
					break
				}
			}
			p.pasted = append(p.pasted, p.buf[:len(p.buf)-keep]...)
			p.buf = p.buf[len(p.buf)-keep:]
			return out
		}
		if len(p.buf) == 0 {
			return out
		}
		n, key, complete := nextKey(p.buf)
		if !complete {
			if !final {
				return out
			}
			if p.buf[0] == 0x1b { // held Esc: a real Esc; what followed is parsed on its own
				n, key = 1, "\x1b"
			} else { // a broken character: drop the byte
				p.buf = p.buf[1:]
				continue
			}
		}
		p.buf = p.buf[n:]
		if key == pasteStart {
			p.inPaste = true
			continue
		}
		if key != "" {
			out = append(out, inputEvent{key: key})
		}
	}
}

// nextKey reads one key from the front of b: its length, the key, and whether
// it is complete (false means more bytes are needed to know).
func nextKey(b []byte) (int, string, bool) {
	if b[0] != 0x1b {
		if !utf8.FullRune(b) {
			return 0, "", false
		}
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size <= 1 {
			return 1, "", true // invalid byte: skip it
		}
		return size, string(b[:size]), true
	}
	if len(b) == 1 {
		return 0, "", false
	}
	switch b[1] {
	case '[': // CSI: ESC [ params final
		for j := 2; j < len(b); j++ {
			if b[j] >= 0x40 && b[j] <= 0x7e {
				return j + 1, string(b[:j+1]), true
			}
			if j > 32 { // not a real sequence: give up on it
				return j + 1, "", true
			}
		}
		return 0, "", false
	case 'O': // SS3: F1-F4 and application-mode arrows
		if len(b) < 3 {
			return 0, "", false
		}
		return 3, string(b[:3]), true
	case 0x1b: // Esc Esc: the first one stands alone
		return 1, "\x1b", true
	}
	if !utf8.FullRune(b[1:]) { // Alt+key: ESC then the key
		return 0, "", false
	}
	_, size := utf8.DecodeRune(b[1:])
	return 1 + size, string(b[:1+size]), true
}

// pasteText turns a paste into one line of draft text: line breaks (and blank
// lines) become a single space, tabs become spaces, control characters go.
func pasteText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimRight(clean(strings.ReplaceAll(l, "\t", " ")), " "); strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, " ")
}

// clean removes what could steer a terminal from text that came from someone
// else (an agent's message, a session name): escape and other control
// characters. Tabs become spaces; line breaks stay, for callers that split.
func clean(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteByte(' ')
		case r == '\n' || !unicode.IsControl(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}
