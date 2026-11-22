# intercom

Agent sessions, and you, message each other. One small Go binary, `ic`, with plain-text state and no
background process.

Coding agents in parallel step on each other because they can't talk. intercom gives every Claude Code
session a name and an inbox, and delivers messages only at moments that can't break the agent's work:

| When | How |
|---|---|
| Between two tool calls | `PostToolUse` hook adds the message to the agent's context |
| At the end of a turn | `Stop` hook holds the stop once, so the agent handles its mail first |
| When a prompt is sent | `UserPromptSubmit` hook adds it before the agent reads the prompt |
| The agent is idle (in [herdr](https://herdr.dev)) | herdr types one short nudge, which fires the prompt hook |

Messages for the human show up as a herdr notification and in `ic inbox`.

## Install

```sh
go install github.com/Andree37/intercom/cmd/ic@latest
ic install claude        # merges ic's hooks into ~/.claude/settings.json (needs jq; keeps a backup)
```

New Claude sessions join automatically. Running ones join after a restart.

## Use

```sh
ic who                               # live sessions, their herdr pane and state
ic send reviewer "map.zig is mine until the tests pass"
ic send tab "look at the failing test"     # the other agent in this herdr tab
ic send pane:w5:p1 "..."                   # by herdr pane
ic ask code-tree "is the parser API final?"  # waits up to 100s for the reply
ic ask all "which port does the api use?"    # every live session; answers arrive in your inbox
ic done                              # your open questions to all
ic done 20261122-152350-aca2f8       # got the answer: close it (a unique id suffix works too)
ic reply 20261122-152350-1da110 "yes"
ic inbox                             # read your messages (as the human, or by hand)
ic name reviewer                     # rename this session
ic log                               # recent messages to or from you
```

## Asking everyone

`ic ask all` puts the question in every live agent's inbox and returns at once; answers arrive like any
message. When one answers well enough, `ic done <question>` closes it:

- agents that hadn't seen it yet never will: their copy is deleted
- agents that saw it but haven't answered get one note telling them to drop it
- any later `ic reply` to it is refused

Only the asker can close a question.

## The pane

`ic watch` is a live view for a terminal pane: every session with its herdr state, every message as it
flows, and your unread mail highlighted. Pick a session and press `enter` to message it, `a` to ask and
wait, `A` to ask every session, `d` to close your newest open question, `t` to show or hide your questions to
all beside it (`tab` moves between the two; there `enter` expands one to show who has seen it and every
answer, `d` closes the selected one, `esc` goes back), `i` to read your inbox, `f` to jump to its herdr pane. In herdr, the
[herdr-intercom](https://github.com/Andree37/herdr-intercom) plugin opens it with a key.

Inside a session, `ic` knows who it is from `IC_ID`, which the SessionStart hook exports. A session
gets a default name from its folder (`code-tree`, `code-tree-2`, ...).

Agents are told about intercom when their session starts, so they can message each other without any
prompt from you.

## State

Everything is plain text under `$IC_HOME` (default `~/.local/state/intercom`):

```
sessions/<id>               id, name, kind, cwd, herdr pane/tab, started, seen, ended
inbox/<id>/<msg>.msg        unread: "key value" header, blank line, body
inbox/<id>/read/<msg>.msg   delivered
questions/<id>              a question asked to all: asker, recipients, closed time, body
messages.log                one line per message
```

## Used by

- [code-tree](https://github.com/Andree37/code-tree): `ct talk` sends a message about a work node to
  whoever owns it, and the agent working a node hears when you answer, accept or decide on it.
