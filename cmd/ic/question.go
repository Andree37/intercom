package main

// Questions to everyone: `ic ask all` puts one copy in every live agent's inbox
// and records the question under questions/<id>. `ic done <id>` closes it: copies
// nobody has seen yet are deleted, agents who saw it but haven't answered are
// told to drop it, and replies to it are refused from then on.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Question struct {
	ID, From, Time, To, Closed string // To: recipient session ids, space-separated
	Body                       string
}

var questionKeys = []string{"id", "from", "time", "to", "closed"}

func questionPath(id string) string { return filepath.Join(home(), "questions", filepath.Base(id)) }

func loadQuestion(id string) *Question {
	kv, body, err := readKV(questionPath(id))
	if err != nil {
		return nil
	}
	return &Question{kv["id"], kv["from"], kv["time"], kv["to"], kv["closed"], strings.TrimRight(body, "\n")}
}

func (q *Question) save() error {
	kv := map[string]string{"id": q.ID, "from": q.From, "time": q.Time, "to": q.To, "closed": q.Closed}
	return writeKV(questionPath(q.ID), questionKeys, kv, q.Body)
}

func questionClosed(id string) bool {
	q := loadQuestion(id)
	return q != nil && q.Closed != ""
}

// openQuestions lists the questions `from` asked that are still open, newest first.
func openQuestions(from string) []*Question {
	entries, _ := os.ReadDir(filepath.Join(home(), "questions"))
	var out []*Question
	for _, e := range entries {
		if q := loadQuestion(e.Name()); q != nil && q.From == from && q.Closed == "" {
			out = append(out, q)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID > out[b].ID })
	return out
}

// askAll sends one question to every live agent except the asker.
func askAll(body, topic string, wake bool) *Question {
	if strings.TrimSpace(body) == "" {
		die("empty message")
	}
	me := self()
	q := &Question{ID: newID(), From: me.ID, Time: now(), Body: body}
	var to []string
	var reached []string
	for _, s := range sessions(false) {
		if s.ID == me.ID || s.Kind == "human" {
			continue
		}
		m := &Message{ID: newID(), From: me.ID, To: s.ID, Time: q.Time, Kind: "ask", Topic: topic, Question: q.ID, Body: body}
		if err := m.store(); err != nil {
			continue
		}
		to = append(to, s.ID)
		how := "queued"
		if wake {
			how = nudge(s, me)
		}
		reached = append(reached, fmt.Sprintf("  %s · %s", s.Name, how))
	}
	if len(to) == 0 {
		die("no other live sessions (see `ic who`)")
	}
	q.To = strings.Join(to, " ")
	if err := q.save(); err != nil {
		die("%v", err)
	}
	(&Message{ID: q.ID, From: me.ID, Time: q.Time, Kind: "ask", Body: body}).log("all")
	fmt.Printf("asked %d session(s) · question %s\n%s\nclose it once you have your answer: ic done %s\n",
		len(to), q.ID, strings.Join(reached, "\n"), q.ID)
	return q
}

// cmdDone closes a question. With no id it lists your open questions.
func cmdDone(args []string) {
	me := self()
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		qs := openQuestions(me.ID)
		if len(qs) == 0 {
			fmt.Println("no open questions")
			return
		}
		for _, q := range qs {
			fmt.Printf("%s  %s  %s\n", q.ID, q.Time[11:16], cutTo(oneLine(q.Body), 70))
		}
		return
	}
	q := loadQuestion(args[0])
	if q == nil {
		for _, o := range openQuestions(me.ID) { // allow a unique prefix or suffix
			if strings.HasPrefix(o.ID, args[0]) || strings.HasSuffix(o.ID, args[0]) {
				if q != nil {
					die("%s is ambiguous", args[0])
				}
				q = o
			}
		}
	}
	if q == nil {
		die("no question %s (see `ic done`)", args[0])
	}
	if q.From != me.ID {
		die("question %s was asked by %s; only they can close it", q.ID, nameOf(q.From))
	}
	if q.Closed != "" {
		fmt.Printf("question %s was already closed at %s\n", q.ID, q.Closed)
		return
	}
	q.Closed = now()
	if err := q.save(); err != nil {
		die("%v", err)
	}

	answered := map[string]bool{}
	for _, dir := range []string{inboxDir(me.ID), filepath.Join(inboxDir(me.ID), "read")} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if m := readMessage(filepath.Join(dir, e.Name())); m != nil && m.Question == q.ID && m.Kind == "reply" {
				answered[m.From] = true
			}
		}
	}

	var dropped, told int
	for _, id := range strings.Fields(q.To) {
		if answered[id] {
			continue
		}
		pending := false
		entries, _ := os.ReadDir(inboxDir(id))
		for _, e := range entries {
			p := filepath.Join(inboxDir(id), e.Name())
			if m := readMessage(p); m != nil && m.Question == q.ID && m.Kind == "ask" {
				_ = os.Remove(p)
				pending = true
			}
		}
		if pending {
			dropped++
			continue
		}
		if s := loadSession(id); s == nil || s.Ended != "" {
			continue
		}
		note := &Message{ID: newID(), From: me.ID, To: id, Time: now(), Kind: "closed", Question: q.ID,
			Body: fmt.Sprintf("Question %s is closed: it has been answered. Stop working on it and do not reply.", q.ID)}
		if note.deliver() == nil {
			told++
		}
	}
	fmt.Printf("closed %s · %d answered · %d never saw it (removed) · %d told to drop it\n", q.ID, len(answered), dropped, told)
}

// allQuestions lists every question `from` asked to all, open or closed, newest first.
func allQuestions(from string) []*Question {
	entries, _ := os.ReadDir(filepath.Join(home(), "questions"))
	var out []*Question
	for k := len(entries) - 1; k >= 0; k-- { // ids sort by time
		if q := loadQuestion(entries[k].Name()); q != nil && q.From == from {
			out = append(out, q)
		}
	}
	return out
}

// Recipient is where one recipient stands on a question.
type Recipient struct {
	Name, State, Answer, Time string // State: unseen, seen, answered, dropped, gone
}

// recipients reports each recipient's state, answers first in the order they came.
func (q *Question) recipients() []Recipient {
	answers := map[string]*Message{}
	for _, dir := range []string{inboxDir(q.From), filepath.Join(inboxDir(q.From), "read")} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if m := readMessage(filepath.Join(dir, e.Name())); m != nil && m.Question == q.ID && m.Kind == "reply" {
				if a := answers[m.From]; a == nil || m.ID < a.ID {
					answers[m.From] = m
				}
			}
		}
	}
	has := func(dir string) bool {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if m := readMessage(filepath.Join(dir, e.Name())); m != nil && m.Question == q.ID && m.Kind == "ask" {
				return true
			}
		}
		return false
	}
	var out []Recipient
	for _, id := range strings.Fields(q.To) {
		r := Recipient{Name: short(id)}
		s := loadSession(id)
		if s != nil {
			r.Name = s.Name
		}
		switch a := answers[id]; {
		case a != nil:
			r.State, r.Answer, r.Time = "answered", a.Body, a.Time
		case has(inboxDir(id)):
			r.State = "unseen"
		case q.Closed != "":
			r.State = "dropped"
		case s == nil || s.Ended != "":
			r.State = "gone"
		case has(filepath.Join(inboxDir(id), "read")):
			r.State = "seen"
		default:
			r.State = "dropped"
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(a, b int) bool {
		ra, rb := out[a].State == "answered", out[b].State == "answered"
		if ra != rb {
			return ra
		}
		return ra && out[a].Time < out[b].Time
	})
	return out
}
