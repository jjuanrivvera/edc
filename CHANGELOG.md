# Changelog

## v0.5.1

**Codex adapter hardening.** Fixes found while running `edc codex serve` as a Telegram Hub.

- **Trust framing**: `wrapEvent` now distinguishes `MESSAGE FROM OPERATOR` (source `TELEGRAM`
  with `context.user_id` == `EDC_OWNER_ID`) from `SYSTEM EVENT (untrusted data)`, and the
  developer instructions tell the model to treat operator messages as direct requests. Everything
  else stays data — investigate, draft, never act.
- **Delivery without an MCP**: the Telegram reply no longer depends on a channel MCP. The model is
  told to end operator-facing turns with plain final text; `forwardFinalMessage` extracts that
  text from the real `turn/completed` payload (`turn.items[].type=="agentMessage"`,
  `phase=="final_answer"`) — the previously expected top-level `last_agent_message` field is not
  what the app-server sends — and delivers it with `tgctl message send` (`EDC_TG_BOT` /
  `EDC_TG_CHAT`).
- **Typing indicator**: `startTyping`/`stopTyping` re-send `tgctl message action --action typing`
  every 4s while a turn runs, so the operator sees the bot working (parity with the Claude
  channel).
- **Sandbox knobs**: `EDC_CODEX_SANDBOX` overrides `sandbox_mode` at launch (unset = the
  app-server reads its Codex config file); `EDC_CODEX_PERMISSION=full` requests an unrestricted
  `permission_profile` in `thread/start`. Note: `sandbox_mode=danger-full-access` as a `-c` flag
  is filtered by app-server requirements; `workspace-write` + `network_access=true` in the config
  file is what actually grants write + network.

## v0.5.0

**Plexus.** edc is one half of **Plexus** (with
[`plexus`](https://github.com/jjuanrivvera/plexus)) — and it stays a fully independent binary
you can use on its own to make any coding-agent session injectable, with or without plexus.

- OpenCode plugin renamed `mesh.ts` → `plexus.ts` (drop it into `~/.config/opencode/plugins/`); the
  Codex plugin and all product references move to Plexus.
- **LICENSE**: MIT.

Earlier `v0.4.x` tags shipped without changelog notes; they brought the OpenCode adapter
(`edc opencode serve`, daemon and TUI-inject modes) with decoupled `opencode serve` launch and
inject-port fixes.

## v0.3.0

Codex support, and a rename from `event-driven-claude` to `edc` ("Event-Driven Coding-agents").

- **Codex adapter** (`edc codex serve`): fronts a long-lived `codex app-server` thread and injects
  each `/inject` event as a `turn/start` over the app-server JSON-RPC protocol. Reuses the emitter
  (event parse, Bearer auth, state file) verbatim — only the receiver changes. Picks an
  account-valid model via `model/list` (skips `gpt-5.4`, which 400s on ChatGPT accounts).
- **plexus integration**: the adapter self-registers as `agent=codex` with its inject port,
  heartbeats while serving, and deregisters on exit (best-effort — a missing plexus never takes
  the adapter down).
- **Codex plugin** (`.codex-plugin/`): SessionStart / PreToolUse / Stop hooks that register
  *interactive* Codex sessions into plexus (`agent=codex`) and heartbeat them, plus the
  `edc-codex-serve` skill. Every Codex session shows up in the mesh, not just the daemon.
- Default mode (the Claude Code `claude/channel` MCP server) is unchanged.
- Go module path renamed to `github.com/jjuanrivvera/edc`.

## v0.2.0

Per-session automatic ports + state-file discovery. `edc` runs one process per Claude Code
session, so a fixed `inject_port` meant only the first session on the machine got a listener.

- `inject_port: "auto"` (or empty) now binds `127.0.0.1:0` (respecting `inject_bind`) and takes
  the kernel-assigned port, so every session gets its own listener. An explicit numeric port
  keeps the previous behavior.
- On listener start, `edc` writes `~/.local/state/edc/<session_id>.json` (`0600`) with
  `{"port":N,"pid":N,"bind":"..."}` so hooks/emitters can discover the session's port.
  `<session_id>` comes from `$CLAUDE_SESSION_ID` (the MCP handshake carries no session id),
  falling back to `pid-<pid>`. Session ids are sanitized to stay filesystem-safe.
- The state file is removed on clean shutdown (stdin EOF or SIGTERM/SIGINT), and state files
  orphaned by crashed sessions (dead pid) are reaped at the next `edc` startup.
- Fail-closed unchanged: no secret ⇒ no listener, ever.

## v0.1.2

- Fix the plugin load path. Claude Code passes a plugin manifest's `"${EDC_INJECT_PORT}"` through
  **verbatim** (unexpanded) when the var isn't set in the host env, and that literal was winning
  over the config file — the listener tried to bind a garbage port and never came up. `edc` now
  treats an unexpanded `${...}` value as unset, so the config file is the source on the plugin
  path. The shipped `.mcp.json` also drops its `env` block (it only produced those placeholders).
  With this, `claude plugin install` + `~/.config/edc/config.json` brings the `/inject` listener
  up. Verified end-to-end.

## v0.1.1

- Config file support: `edc` reads `inject_port` / `inject_secret` / `inject_bind` from
  `~/.config/edc/config.json` (override with `$EDC_CONFIG`) when the env vars aren't set — env
  still takes precedence. This makes the plugin install path work: a plugin-loaded MCP server
  doesn't inherit the launching shell's env, so the file is how you hand it a port and secret.

## v0.1.0

Initial release.

- Transport-agnostic Claude Code channel: an MCP stdio server that declares the
  `claude/channel` capability and runs a local `/inject` HTTP endpoint, so any session can be
  woken by external events (crons, watchers, daemons) instead of only by a human typing.
- Security: fail-closed (no listener without a port **and** a secret), Bearer auth with a
  constant-time compare, `meta.source` always `system` (no impersonation), caller `context`
  namespaced under `ctx_*`, bounded request body + HTTP timeouts.
- Ships the plugin manifest (`.mcp.json`, `.claude-plugin/marketplace.json`) and prebuilt
  binaries (`edc`) for macOS/Linux/Windows.
