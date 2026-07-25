// Command edc (Event-Driven Coding-agents) is a transport-agnostic Claude Code "channel": an MCP
// stdio server whose only job is to make ANY session injectable. It declares the
// claude/channel capability and runs a local /inject HTTP listener, so external deterministic
// tools (crons, watchers, daemons) can POST an event that arrives as a session turn.
//
// No Telegram, no outbound transport, no tools — a "fake channel". The philosophy: give any
// Claude session the ability to be woken by events instead of only by a human typing.
package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

var version = "0.3.0"

// Config is the inject listener's config. The channel capability itself needs nothing; the
// HTTP listener is opt-in and fails closed without a secret. Values come from env vars first,
// then a config file. The file matters for the plugin install path: a plugin-loaded server
// doesn't inherit the launching shell's env, so the file is how you hand it a port and secret
// there.
type Config struct {
	InjectPort   string // EDC_INJECT_PORT / inject_port — explicit port; "auto"/empty = per-session kernel-assigned port
	InjectSecret string // EDC_INJECT_SECRET / inject_secret — required to bind (fail closed)
	InjectBind   string // EDC_INJECT_BIND / inject_bind — default 127.0.0.1; a Tailscale/LAN IP for remote emitters
}

// fileConfig mirrors Config in ~/.config/edc/config.json. Env vars take precedence over it.
type fileConfig struct {
	InjectPort   string `json:"inject_port"`
	InjectSecret string `json:"inject_secret"`
	InjectBind   string `json:"inject_bind"`
}

func loadConfig() Config {
	fc := loadConfigFile()
	return Config{
		InjectPort:   firstNonEmpty(envClean("EDC_INJECT_PORT"), fc.InjectPort),
		InjectSecret: firstNonEmpty(envClean("EDC_INJECT_SECRET"), fc.InjectSecret),
		InjectBind:   firstNonEmpty(envClean("EDC_INJECT_BIND"), fc.InjectBind, "127.0.0.1"),
	}
}

// envClean reads an env var but treats an unexpanded "${VAR}" placeholder as unset. A plugin
// manifest that maps env like "EDC_INJECT_PORT": "${EDC_INJECT_PORT}" gets that literal passed
// through verbatim when the var isn't set in the host's environment (Claude Code does this on the
// plugin load path). Without this guard the literal would win over the config file and the
// listener would try to bind a garbage port. So on the plugin path the config file is the source.
func envClean(key string) string {
	v := os.Getenv(key)
	if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
		return ""
	}
	return v
}

// loadConfigFile reads $EDC_CONFIG, else $XDG_CONFIG_HOME/edc/config.json, else
// ~/.config/edc/config.json. A missing or malformed file is not an error — it yields no values.
func loadConfigFile() fileConfig {
	path := configPath()
	if path == "" {
		return fileConfig{}
	}
	b, err := os.ReadFile(path) //nolint:gosec // G304: a config path under the user's own home
	if err != nil {
		return fileConfig{}
	}
	var fc fileConfig
	_ = json.Unmarshal(b, &fc)
	return fc
}

func configPath() string {
	if p := os.Getenv("EDC_CONFIG"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "edc", "config.json")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func main() {
	// Subcommands select an adapter. Default (no args) stays the Claude Code channel MCP server,
	// unchanged, so every existing plugin install keeps working.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "codex":
			os.Exit(runCodex(os.Args[2:]))
		case "opencode":
			os.Exit(runOpenCode(os.Args[2:]))
		}
	}

	cfg := loadConfig()
	srv := &server{out: newOut(os.Stdout), cfg: cfg}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Reap state files left behind by sessions that died without cleanup (kill -9, crash,
	// reboot) before this process writes its own.
	cleanOrphanStateFiles(stateDir())

	go srv.runInject()

	// Serve until Claude Code closes the MCP pipe (stdin EOF) or a signal arrives — either
	// way, drop the state file so emitters stop discovering a dead listener.
	done := make(chan struct{})
	go func() {
		srv.serve(os.Stdin)
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	srv.removeStateFile()
}
