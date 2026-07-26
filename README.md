<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/edc-mark-ondark.svg">
    <img alt="edc" src="assets/edc-mark-onlight.svg" width="104" height="104">
  </picture>
</p>

<h1 align="center">edc — Event-Driven Coding-agents</h1>

**Drive any coding-agent session with external events.** `edc` is a transport-agnostic event
injector: a small local `/inject` HTTP endpoint that turns an event (a cron, a watcher, a daemon,
another agent) into a **turn** in a running session. One agnostic emitter, one adapter per agent:

- **Claude Code** — an MCP stdio server declaring the `claude/channel` capability.
- **Codex** — [`edc codex serve`](#codex-adapter-edc-codex-serve), injecting events as `turn/start`
  into a live `codex app-server` thread.
- **OpenCode** — [`edc opencode serve`](#opencode-adapter-edc-opencode-serve),
  injecting events as `POST /session/{id}/prompt_async` into a live OpenCode server session.

The philosophy: a session woken by events, not only by a person typing. (`edc` began as
Event-Driven *Claude*; it now covers any coding agent.)

`edc` is half of **Plexus**: it feeds events in, while **[plexus](https://github.com/jjuanrivvera/plexus)**
sees, attaches, and launches those sessions. 📖 **Full Plexus documentation →
<https://jjuanrivvera.github.io/plexus/>**

---

## The problem

A Claude Code session is **reactive**: it does nothing until a human types. There is no clean
way for the rest of your system — cron jobs, file/log watchers, home automation, another agent —
to hand the running session an event. The usual workarounds are all bad:

- **A polling loop inside the session** (`/loop 5m` reading a file) burns model tokens on every
  tick even when nothing happened.
- **A second bot / a chat transport** couples the session to Telegram/Slack just to receive a
  local event — and bots can't message themselves.
- **Nothing**: the session simply can't be woken by the world around it.

`edc` fixes exactly one thing: a transport-agnostic, low-cost way to push an event into a
running session as a turn. **Event-driven, not polling** — the session sleeps at zero token
cost until an event actually arrives, no matter how many sessions you run.

## How it works — Claude channel

`edc` is a tiny MCP stdio server (~250 lines, pure Go stdlib, no dependencies) that does two
things:

1. **Declares the `claude/channel` capability** during the MCP handshake — that is what lets a
   server *push* turns into the session (not just answer tool calls).
2. **Runs a local `/inject` HTTP listener.** Any tool `POST`s a JSON event; the server emits it
   into the session as a turn (`notifications/claude/channel`, `meta.source="system"`).

```
[ cron / file watcher / log tailer / another agent ]
        │  POST /inject  (Bearer secret)
        ▼
   edc (MCP stdio server)  ── notifications/claude/channel ──▶  the Claude session (a new turn)
```

That's the whole thing. **No Telegram, no poller, no outbound transport, no tools** — the
distilled core of [`tgctl-claude-channel`](https://github.com/jjuanrivvera/tgctl-claude-channel)
(a Telegram channel for Claude Code) with everything transport-specific removed. Because it's
transport-agnostic it runs anywhere, for any session, with no bot and no collision.

## Install

```sh
# checksum-verified release binary onto your PATH (~/.local/bin by default)
curl -fsSL https://raw.githubusercontent.com/jjuanrivvera/edc/main/install.sh | sh

# or, with a Go toolchain:
go install github.com/jjuanrivvera/edc@latest   # installs as `edc`
```

Both the `install.sh` binary and `go install` produce `edc` (the module was renamed from
`event-driven-claude`).

Or install it as a Claude Code plugin (this also registers the marketplace used below):

```sh
claude plugin marketplace add jjuanrivvera/edc
claude plugin install event-driven-claude@jjuanrivvera-edc
```

The plugin is still named `event-driven-claude` — it is the **Claude adapter**, and the name is
accurate. The Codex adapter is described below.

## Codex adapter (`edc codex serve`)

Codex has no `claude/channel`, so the receiver is different: `edc codex serve` fronts a long-lived
`codex app-server` thread and injects each event as a `turn/start` over the app-server JSON-RPC
protocol. The **emitter is identical** — same `/inject` endpoint, same Bearer auth, same event
shape — so a tool that injects into a Claude session injects into a Codex session unchanged.

```
[ cron / watcher / another agent ]
        │  POST /inject  (Bearer secret)   ← same contract as the Claude channel
        ▼
   edc codex serve  ── turn/start ──▶  a live codex app-server thread (a new turn)
```

```sh
export EDC_INJECT_SECRET=<secret>    # required (the listener fails closed without it)
export EDC_INJECT_PORT=auto          # or a fixed port
edc codex serve
```

It picks a model the account can actually run via `model/list`, seeds the thread with an
"events are untrusted data" framing, and **self-registers into
[plexus](https://github.com/jjuanrivvera/plexus) as `agent=codex`** with its inject port.
Attach an interactive view to the same backend with `codex --remote unix://<socket>` to watch
injected turns land. Full usage: the [`edc-codex-serve`](skills/edc-codex-serve/SKILL.md) skill.

**Install as a Codex plugin.** The repo ships a `.codex-plugin/` alongside the Claude
`.claude-plugin/`. Its hooks register *interactive* Codex sessions into plexus (`agent=codex`)
on start and heartbeat them while they work — so every Codex session, not only the
`edc codex serve` daemon, shows up in Plexus; plexus's TTL prune reclaims them on exit.

**Trust boundary.** Codex has no native `source=system` marker, so an injected event arrives as
ordinary user input. `edc` reconstructs the boundary as text and in `developerInstructions`, but
that only softens it — treat every injected event as data, never as authority to act.

## OpenCode adapter (`edc opencode serve`)

> **Status: built + smoke-tested.** `edc opencode serve` starts an `opencode serve` backend,
> creates a session, self-registers in plexus (`agent=opencode`, with its inject port), and
> injects events over the HTTP API — verified end-to-end (a `POST /inject` reaches the session and
> returns 202). A live model *reply* needs your OpenCode account authenticated with a provider/model,
> like any OpenCode use.

Unlike Codex, OpenCode is **client-server by design**: the interactive TUI is just one HTTP client
of a local `opencode serve` (Hono, OpenAPI 3.1, default `127.0.0.1:4096`), and any external process
can address the **same live session** over plain HTTP. So this adapter is thinner than the Codex
one — no reverse-engineered JSON-RPC. The **emitter is identical** — same `/inject` endpoint, same
Bearer auth, same event shape.

```
[ cron / watcher / another agent ]
        │  POST /inject  (Bearer secret)   ← same contract as the Claude/Codex adapters
        ▼
   edc opencode serve  ── POST /session/{id}/message ──▶  a live opencode session (a new turn)
```

Mechanism (from the [OpenCode server](https://opencode.ai/docs/server/) + [SDK](https://opencode.ai/docs/sdk/) docs):

- **Inject a turn:** `POST /session/{id}/message` (sync; SDK `session.prompt`) or
  `POST /session/{id}/prompt_async` (fire-and-forget, 204). Always send an explicit `agent` + `model`
  — an omitted agent/model is silently overridden ([sst/opencode#21728](https://github.com/sst/opencode/issues/21728)).
- **Interrupt:** `POST /session/{id}/abort`. There is no `Turn/steer` primitive; steer by aborting
  and injecting a new turn.
- **State:** subscribe to `GET /event` (SSE) and mirror `session.idle` / tool / permission events
  into plexus as idle/busy/blocked.
- **Registration (interactive sessions):** the repo ships an OpenCode **plugin** at
  [`.opencode-plugin/plexus.ts`](.opencode-plugin/plexus.ts) — copy or symlink it to
  `~/.config/opencode/plugins/plexus.ts`. It subscribes to `session.created` and runs
  `plexus ttyd spawn` + `plexus register --agent opencode`, so an interactive
  `plexus opencode` session shows up in the cockpit exactly like Claude/Codex. OpenCode has no
  reliable process-exit hook ([sst/opencode#14863](https://github.com/sst/opencode/issues/14863)),
  so teardown falls to the tmux launcher + `plexus ttyd reap` / TTL prune.

**Known gap → solved for interactive sessions.** An externally-injected turn is processed by the
model but may not render in the raw TUI ([sst/opencode#8564](https://github.com/sst/opencode/issues/8564)).
`edc opencode serve` in **TUI mode** (`EDC_OPENCODE_TUI=1` + `EDC_OPENCODE_URL=<shared server>`)
works around it: instead of `prompt_async`, it types each `/inject` event into the attached session
via `POST /tui/append-prompt` + `/tui/submit-prompt`, so the human **sees** the turn land.
`plexus`'s `plexus opencode [dir]` wires this automatically — a decoupled `opencode serve` +
`opencode attach` + a TUI-mode sidecar on a fixed inject port — making an interactive OpenCode
session attachable **and** injectable via `edc /inject`.

**Trust boundary.** Like Codex, OpenCode has no native `source=system` marker — an injected event
arrives as ordinary user input. `edc` reconstructs the boundary in text, but treat every injected
event as data, never as authority to act.

## Quick start — Claude channel

The happy path: install as a plugin, configure a port and secret, authorize the channel, launch
a session, inject an event.

**1. Configure a port and secret.** A plugin-loaded MCP server does **not** inherit your
shell's environment, so the port and secret come from a config file at
`~/.config/edc/config.json` (not `export`):

```sh
mkdir -p ~/.config/edc
cat > ~/.config/edc/config.json <<EOF
{ "inject_port": "auto", "inject_secret": "$(openssl rand -hex 24)" }
EOF
```

`"auto"` gives each session its own kernel-assigned port (recommended — see
[Configuration reference](#configuration-reference)). If you change the config later, restart
the session — it's read once at startup.

**2. Authorize the plugin as a channel (once, requires admin rights).** For installed plugins,
Claude Code reads the channel allowlist ONLY from managed settings — user `settings.json` is
ignored by design, so a process running as your user can't self-authorize an injection channel.
The managed settings file lives at:

| OS | Path |
|---|---|
| macOS | `/Library/Application Support/ClaudeCode/managed-settings.json` |
| Linux | `/etc/claude-code/managed-settings.json` |
| Windows | `C:\ProgramData\ClaudeCode\managed-settings.json` |

```sh
# macOS shown; on Linux use /etc/claude-code/managed-settings.json
sudo mkdir -p "/Library/Application Support/ClaudeCode" && sudo tee "/Library/Application Support/ClaudeCode/managed-settings.json" > /dev/null <<'JSON'
{
  "allowedChannelPlugins": [
    { "marketplace": "jjuanrivvera-edc", "plugin": "event-driven-claude" }
  ]
}
JSON
```

On Windows, create the file at the path above from an elevated (Administrator) shell with the
same JSON content. The schema is an **array of `{marketplace, plugin}` objects** (not strings).
See [Channel authorization](#channel-authorization-the-gates) for why this lives at the
machine level.

**3. Launch a session that requests the channel** (every launch):

```sh
claude --channels "plugin:event-driven-claude@jjuanrivvera-edc"
```

On a fresh session, a `Channels (experimental) … inject directly` line means the capability
registered.

**4. Inject an event** from any process on the box:

```sh
STATE=$(ls ~/.local/state/edc/*.json | head -1)   # or the specific session's file
PORT=$(jq -r .port "$STATE")
curl -sS -XPOST "http://127.0.0.1:$PORT/inject" \
  -H "Authorization: Bearer <your-secret>" \
  -d '{"source":"CI","event":"build_failed","text":"main build failed at step \"test\""}'
```

The session wakes with the event as a new turn.

## Configuration reference

Values come from **env vars first, then the config file** at `~/.config/edc/config.json`
(override the path with `$EDC_CONFIG`; `$XDG_CONFIG_HOME/edc/config.json` is respected).
The same layout applies on every OS — on Windows `~` is `%USERPROFILE%`. The
file is what makes the plugin install path work — a plugin-loaded MCP server doesn't inherit
the launching shell's env. On the `server:` path (a `.mcp.json` entry), env vars **do** work.
A manifest env value passed through unexpanded (a literal `${...}`) is treated as unset.

```json
{ "inject_port": "auto", "inject_secret": "a-long-random-secret", "inject_bind": "127.0.0.1" }
```

| Env | Config file | Meaning |
|---|---|---|
| `EDC_INJECT_PORT` | `inject_port` | `"auto"` or empty ⇒ per-session kernel-assigned port, published in the state file (recommended); a number ⇒ that exact port (only one session on the machine can bind it) |
| `EDC_INJECT_SECRET` | `inject_secret` | required Bearer secret; unset ⇒ refuses to bind (fail closed) |
| `EDC_INJECT_BIND` | `inject_bind` | bind address, default `127.0.0.1` (a Tailscale/LAN IP for remote emitters) |
| `CLAUDE_SESSION_ID` | — | names the state file `~/.local/state/edc/<session_id>.json`; unset ⇒ `pid-<pid>` |
| `XDG_STATE_HOME` | — | state dir base, default `~/.local/state` |

### State files

`edc` runs **one process per Claude Code session**, and sessions can't share a port. With
`"auto"` the kernel picks the port, so emitters discover it through a per-session **state file**:

- Path: `$XDG_STATE_HOME/edc/<session_id>.json`, defaulting to
  `~/.local/state/edc/<session_id>.json` — the same layout on every OS (on Windows the home
  directory is `%USERPROFILE%`). Mode `0600`. `<session_id>` is `$CLAUDE_SESSION_ID` when the
  host exports it (the MCP handshake carries no session id), else `pid-<pid>`. Session ids are
  sanitized to stay filesystem-safe.
- Content: `{ "port": 52341, "pid": 48210, "bind": "127.0.0.1" }`
- Lifecycle: written when the listener binds; removed on clean exit (stdin EOF or
  SIGTERM/SIGINT). Emitters should trust a file **only while its `pid` is alive**
  (`kill -0 <pid>` on unix).
- Orphans left by crashed sessions (dead pid) are reaped at the next `edc` startup — except on
  Windows, where there is no cheap signal-0 liveness check, so pids are assumed alive and stale
  files are removed by clean exits only.

## Injecting events

The emitter is any process with network reach to the bind address; it needs the **port**
(explicit, or read from the session's state file) and the **secret**:

```sh
curl -sS -XPOST "http://127.0.0.1:$PORT/inject" \
  -H "Authorization: Bearer $EDC_INJECT_SECRET" \
  -d '{"source":"CI","event":"build_failed","text":"main build failed at step \"test\"","context":{"commit":"a1b2c3d"}}'
```

**Request** — `POST /inject`, `Authorization: Bearer <secret>`, JSON body:

| Field | Required | Meaning |
|---|---|---|
| `text` | yes | human-readable event description; becomes the turn's content |
| `source` | no | caller-declared origin (e.g. `"CRON"`, `"HA"`); relayed as `meta.injected_by` |
| `event` | no | machine-readable key (e.g. `"build_failed"`); relayed as `meta.event` |
| `context` | no | object, relayed under `ctx_*`-prefixed meta keys; non-string values keep their JSON encoding (`42`, `true`, nested objects) |
| *(anything else)* | no | unknown top-level fields are accepted and relayed as `ctx_*` too — attach `correlation_id`, `reply_to`, etc. without a schema change |

**Responses**: `202` (accepted, a channel frame was emitted — see
[the gates](#channel-authorization-the-gates) for why that is not the same as delivered),
`400` (malformed JSON, wrong field type, or missing `text` — the error names the
offending field), `401` (no/wrong secret — no turn is emitted), `405` (non-POST),
`413` (body over 64 KiB).

Arrives in the session as:

```json
{ "content": "main build failed at step \"test\"",
  "meta": { "source": "system", "injected_by": "CI", "event": "build_failed",
            "ts": "…", "ctx_commit": "a1b2c3d" } }
```

**Discovering the port**: read the session's state file (above). A machine-wide registry (for
example [plexus](https://github.com/jjuanrivvera/plexus), which tracks which sessions are alive) can
consume the state files to route events to the right session; fan-out to several sessions is the
emitter's job — post to each port.

**Cost lives on the emitter.** Every injected event wakes the session and spends tokens. `edc`
does no filtering; the emitter must pre-filter and only inject what is worth waking the model
for — cheap deterministic sensor outside, expensive reasoning inside.

## Channel authorization (the gates)

Emitting the notification is not enough: **Claude Code gates injected turns behind two explicit
opt-ins**, and silently drops them otherwise.

1. **Machine-level allowlist (once, requires admin rights).** For *installed* plugins, the
   allowlist is read ONLY from managed settings — see the per-OS path table in
   [Quick start step 2](#quick-start). The schema is an array of `{marketplace, plugin}`
   objects, not strings.

2. **Per-session opt-in (every launch).** Sessions accept channel turns only when started with:

   ```sh
   claude --channels "plugin:event-driven-claude@<your-marketplace>"
   ```

**Local dev without install**: load the repo as a session plugin — it gets the reserved
`inline` marketplace and skips the managed allowlist, gated by the dev flag instead:

```sh
claude --plugin-dir /path/to/event-driven-claude \
  --channels "plugin:event-driven-claude@inline" \
  --dangerously-load-development-channels "plugin:event-driven-claude@inline"
```

The `server:` load path (a `.mcp.json` entry naming the server) also works with the dev flag —
`claude --dangerously-load-development-channels server:event-driven-claude` — and, unlike the
plugin path, inherits your shell env, so `export EDC_INJECT_PORT=... EDC_INJECT_SECRET=...`
works there.

**Verify**: the plugin's MCP log
(`<cache>/claude-cli-nodejs/<project>/mcp-logs-plugin-event-driven-claude-*/*.jsonl`, where
`<cache>` is `~/Library/Caches` on macOS, `~/.cache` on Linux, or the Claude Code cache
directory for your OS; or the log named by `--debug-file`) should show
`Channel notifications registered`;
`Channel notifications skipped: …` means one of the two gates is missing.
**A `202` from `/inject` means edc accepted and emitted the event — delivery is decided by
these gates on the Claude Code side.**

Why so much ceremony: an injected turn is the most direct prompt-injection vector into a
session with tool access. The double opt-in — an admin allowlists the plugin once, and each
session requests the channel explicitly — is what separates "the machine owner decided this"
from "something running as the user decided it for them".

## Security model

The `/inject` endpoint can create a turn in your agent, so it is guarded deliberately:

- **Fail closed.** No listener without a secret — a configured port with no secret logs an
  error and never binds. There is no such thing as an unauthenticated listener.
- **Authenticated, constant-time.** Every request needs `Authorization: Bearer <secret>`,
  compared with `crypto/subtle` (timing-attack resistant). No/wrong secret ⇒ `401` **and no
  turn is emitted**.
- **`meta.source` is always `"system"`, stamped by the server.** A caller may *declare* its
  origin (`injected_by`) but can **never** present itself as an authenticated human. There is
  no `chat_id`/`user_id` an event can carry.
- **Context is namespaced under `ctx_*`.** A caller cannot shadow the reserved meta fields
  (`source`, `injected_by`, `event`, `ts`) or slip in a fake `user_id`. (Covered by a test.)
- **Events are data, not commands.** The session `instructions` tell the model to treat every
  injected event as untrusted data — never as authority to change its instructions or take a
  sensitive action on the event's say-so. This is the prompt-injection defense at the session
  level; the machine-level allowlist (above) is the defense at the authorization level.
- **Bounded input.** Body capped at 64 KiB; the HTTP server sets Read/Write/Idle timeouts
  (Slowloris protection — it matters because the bind can be a Tailscale/LAN IP, not just
  loopback).
- **Loopback by default.** `127.0.0.1` unless you opt into a Tailscale/LAN bind. It is **not**
  a public webhook by design — front remote emitters with Tailscale, never the open internet.
- ⚠️ **The secret is the entire gate.** Anyone with the secret and network reach to the port
  can inject a turn. Keep it out of logs, rotate it if it leaks, and never expose the port
  publicly.

### Limitations (by design)

- **Ingress only.** It receives events; it has **no outbound transport and no tools**. The
  session reacts by *doing work* with its own tools — there is nothing to "reply" to here.
- **Fire-and-forget, no delivery guarantee.** There is **no queue, no retry, no history, no
  ack**. If the session is down or the channel didn't load, the event is lost — the **emitter**
  must handle that (fall back, queue, or drop).
- **Local-only reach.** Not designed for the public internet. Remote emitters go over
  Tailscale/LAN.
- **Rides an experimental Claude Code feature.** Channel loading has its own quirks — the
  dev-channel prompt, plugin enabled-state, and a clean binary build all matter for the
  capability to register.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| MCP log shows `Channel notifications skipped: …` and the session was launched without `--channels` | The per-session opt-in is missing — sessions accept channel turns only when they request them | Relaunch with `claude --channels "plugin:event-driven-claude@<marketplace>"` |
| `Channel notifications skipped: …` even with `--channels` | The plugin is not on the machine-level allowlist (installed plugins read it only from managed settings) | Add `{ "marketplace": "...", "plugin": "event-driven-claude" }` to `allowedChannelPlugins` in the managed settings file (array of objects, not strings); or use the dev path (`--plugin-dir` + `@inline` + `--dangerously-load-development-channels`) |
| `/inject` returns `202` but no turn appears in the session | `202` means edc accepted and emitted the frame; Claude Code dropped it at one of the two gates | Check the MCP log for `Channel notifications registered` vs `skipped` and fix the missing gate |
| Second session comes up with no listener | An explicit numeric `inject_port` — only the first session on the machine can bind it | Set `inject_port: "auto"` so each session gets its own kernel-assigned port, discovered via its state file |
| State file is named `pid-<pid>.json` instead of a session id | The host didn't export `CLAUDE_SESSION_ID` (the MCP handshake carries no session id) | Export `CLAUDE_SESSION_ID` in the launch environment, or key discovery off the pid-named file (the `pid` field still tells you liveness) |
| Listener never binds, log says `a port is configured but no secret` | Fail-closed: a port with no secret never binds | Set `inject_secret` in the config file (plugin path) or `EDC_INJECT_SECRET` (server path) |
| Config change has no effect | Config is read once at startup | Restart the session |
| Emitters find a port that no longer answers | Stale state file from a crashed session | Trust a state file only while its `pid` is alive (`kill -0` on unix); orphans are reaped at the next `edc` startup (unix only — on Windows stale files are removed by clean exits) |
