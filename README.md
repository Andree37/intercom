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
ic reply 20261122-152350-1da110 "yes"
ic inbox                             # read your messages (as the human, or by hand)
ic name reviewer                     # rename this session
ic log                               # recent messages to or from you
```

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
messages.log                one line per message
```

## Used by

- [code-tree](https://github.com/Andree37/code-tree): `ct talk` sends a message about a work node to
  whoever owns it, and the agent working a node hears when you answer, accept or decide on it.
