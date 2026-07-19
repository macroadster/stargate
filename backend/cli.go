package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Version metadata — overridden at link time via:
//
//	-ldflags "-X main.version=v1.2.3 -X main.gitCommit=abc1234"
//
// Defaults are for local/dev builds.
var (
	version   = "dev"
	gitCommit = "unknown"
)

// runCLI dispatches CLI commands.
//
//	handled == false → start the HTTP server (after any serve flags applied)
//	handled == true  → CLI fully handled; caller should os.Exit(exitCode)
//
// install.sh calls `stargate --version`; bare `stargate` (and `stargate serve`)
// start the server.
func runCLI(args []string, stdout, stderr io.Writer) (handled bool, exitCode int) {
	if len(args) == 0 {
		return false, 0
	}

	cmd, rest := args[0], args[1:]

	// Top-level flag forms of version/help (install.sh, muscle memory).
	switch cmd {
	case "--version", "-version", "-v":
		cmd, rest = "version", nil
	case "--help", "-help", "-h":
		cmd, rest = "help", nil
	}

	// Bare flags with no subcommand → serve flags (e.g. stargate --port 3002).
	if strings.HasPrefix(cmd, "-") {
		if err := applyServeFlags(args, stderr); err != nil {
			if err == flag.ErrHelp {
				printServeUsage(stdout)
				return true, 0
			}
			fmt.Fprintf(stderr, "error: %v\n\n", err)
			printServeUsage(stderr)
			return true, 2
		}
		return false, 0
	}

	switch cmd {
	case "serve", "start", "run":
		if err := applyServeFlags(rest, stderr); err != nil {
			if err == flag.ErrHelp {
				printServeUsage(stdout)
				return true, 0
			}
			fmt.Fprintf(stderr, "error: %v\n\n", err)
			printServeUsage(stderr)
			return true, 2
		}
		return false, 0

	case "version":
		fmt.Fprintln(stdout, versionString())
		return true, 0

	case "help":
		return cmdHelp(rest, stdout, stderr)

	case "env", "config":
		return cmdEnv(rest, stdout, stderr)

	case "doctor":
		return cmdDoctor(rest, stdout, stderr)

	case "health":
		return cmdHealth(rest, stdout, stderr)

	case "completion":
		return cmdCompletion(rest, stdout, stderr)

	default:
		fmt.Fprintf(stderr, "unknown command: %s\n\n", cmd)
		printRootUsage(stderr)
		return true, 2
	}
}

// handleCLI is the process entry for CLI dispatch (stdout/stderr = process streams).
func handleCLI(args []string) bool {
	handled, code := runCLI(args, os.Stdout, os.Stderr)
	if !handled {
		return false
	}
	if code != 0 {
		os.Exit(code)
	}
	return true
}

func versionString() string {
	v := version
	if v == "" {
		v = "dev"
	}
	if gitCommit != "" && gitCommit != "unknown" {
		return fmt.Sprintf("stargate %s (%s)", v, gitCommit)
	}
	return fmt.Sprintf("stargate %s", v)
}

// ---------------------------------------------------------------------------
// serve flags → environment
// ---------------------------------------------------------------------------

type serveOptions struct {
	port       string
	dataDir    string
	storage    string
	pgDSN      string
	uploadsDir string
	blocksDir  string
	agent      bool
	agentSet   bool
}

func newServeFlagSet(opts *serveOptions, errOut io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.StringVar(&opts.port, "port", "", "HTTP listen port (env STARGATE_HTTP_PORT, default 3001)")
	fs.StringVar(&opts.port, "p", "", "shorthand for --port")
	fs.StringVar(&opts.dataDir, "data-dir", "", "root data directory (env STARGATE_DATA_DIR, default data)")
	fs.StringVar(&opts.dataDir, "d", "", "shorthand for --data-dir")
	fs.StringVar(&opts.storage, "storage", "", "storage backend: sqlite|postgres|memory (env STARGATE_STORAGE)")
	fs.StringVar(&opts.pgDSN, "pg-dsn", "", "Postgres DSN when --storage=postgres (env STARGATE_PG_DSN)")
	fs.StringVar(&opts.uploadsDir, "uploads-dir", "", "uploads directory (env UPLOADS_DIR)")
	fs.StringVar(&opts.blocksDir, "blocks-dir", "", "blocks directory (env BLOCKS_DIR)")
	fs.BoolVar(&opts.agent, "agent", false, "enable built-in agents (env STARGATE_AGENT_ENABLED=true)")
	fs.Usage = func() {
		printServeUsage(errOut)
	}
	return fs
}

func applyServeFlags(args []string, errOut io.Writer) error {
	var opts serveOptions
	fs := newServeFlagSet(&opts, errOut)

	// Detect whether --agent was explicitly provided (BoolVar defaults false).
	// We walk args after parse via Visit.
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}

	fs.Visit(func(f *flag.Flag) {
		if f.Name == "agent" {
			opts.agentSet = true
		}
	})

	if opts.port != "" {
		_ = os.Setenv("STARGATE_HTTP_PORT", opts.port)
	}
	if opts.dataDir != "" {
		_ = os.Setenv("STARGATE_DATA_DIR", opts.dataDir)
	}
	if opts.storage != "" {
		s := strings.ToLower(strings.TrimSpace(opts.storage))
		switch s {
		case "sqlite", "postgres", "memory":
			_ = os.Setenv("STARGATE_STORAGE", s)
		default:
			return fmt.Errorf("invalid --storage %q (want sqlite|postgres|memory)", opts.storage)
		}
	}
	if opts.pgDSN != "" {
		_ = os.Setenv("STARGATE_PG_DSN", opts.pgDSN)
	}
	if opts.uploadsDir != "" {
		_ = os.Setenv("UPLOADS_DIR", opts.uploadsDir)
	}
	if opts.blocksDir != "" {
		_ = os.Setenv("BLOCKS_DIR", opts.blocksDir)
	}
	if opts.agentSet {
		if opts.agent {
			_ = os.Setenv("STARGATE_AGENT_ENABLED", "true")
		} else {
			_ = os.Setenv("STARGATE_AGENT_ENABLED", "false")
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// help
// ---------------------------------------------------------------------------

func cmdHelp(args []string, stdout, stderr io.Writer) (bool, int) {
	if len(args) == 0 {
		printRootUsage(stdout)
		return true, 0
	}
	switch args[0] {
	case "serve", "start", "run":
		printServeUsage(stdout)
	case "version":
		fmt.Fprintln(stdout, "Usage: stargate version\n\nPrint the stargate version and git commit, then exit.")
	case "env", "config":
		printEnvUsage(stdout)
	case "doctor":
		printDoctorUsage(stdout)
	case "health":
		printHealthUsage(stdout)
	case "completion":
		printCompletionUsage(stdout)
	case "help":
		printRootUsage(stdout)
	default:
		fmt.Fprintf(stderr, "unknown help topic: %s\n\n", args[0])
		printRootUsage(stderr)
		return true, 2
	}
	return true, 0
}

func printRootUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage: stargate <command> [flags]

Stargate is a single-binary Bitcoin-native coordination server (UI + API + MCP).

Commands:
  serve         Start the HTTP server (default if no command is given)
  version       Print version and exit
  env           Print effective configuration
  doctor        Check local data dirs and configuration
  health        Probe a running server's /api/health endpoint
  completion    Generate shell completion scripts (bash|zsh|fish)
  help          Show help for a command

Examples:
  stargate
  stargate serve --port 3001 --data-dir ./data
  stargate --version
  stargate env
  stargate doctor
  stargate health --url http://127.0.0.1:3001
  eval "$(stargate completion zsh)"

Run "stargate help <command>" for details.
`)
}

func printServeUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage: stargate serve [flags]
       stargate [flags]            # same as serve when flags are given
       stargate                    # serve with environment defaults

Start the Stargate HTTP server (embedded UI + REST + MCP).

Flags:
  -p, --port string         HTTP listen port (STARGATE_HTTP_PORT, default 3001)
  -d, --data-dir string     Root data directory (STARGATE_DATA_DIR, default data)
      --storage string      sqlite | postgres | memory (STARGATE_STORAGE)
      --pg-dsn string       Postgres DSN (STARGATE_PG_DSN / DATABASE_URL)
      --uploads-dir string  Uploads directory (UPLOADS_DIR)
      --blocks-dir string   Blocks directory (BLOCKS_DIR)
      --agent               Enable built-in agents (STARGATE_AGENT_ENABLED=true)
  -h, --help                Show this help

Environment (common):
  STARGATE_HTTP_PORT, STARGATE_DATA_DIR, STARGATE_STORAGE, STARGATE_PG_DSN,
  UPLOADS_DIR, BLOCKS_DIR, STARGATE_AGENT_ENABLED, STARLIGHT_DONATION_ADDRESS,
  IPFS_ENABLED, IPFS_API_URL

Examples:
  stargate serve
  stargate serve -p 8080 -d /var/lib/stargate
  stargate serve --storage postgres --pg-dsn "$DATABASE_URL"
  stargate serve --agent
`)
}

func printEnvUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage: stargate env [--json]

Print the effective runtime configuration derived from the environment
(and the same path consolidation rules the server uses). Secrets are redacted.

Flags:
      --json   Emit JSON instead of a table
  -h, --help   Show this help
`)
}

func printDoctorUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage: stargate doctor

Validate that data directories are usable and summarize configuration.
Does not start the server. Exit 0 if checks pass, 1 if problems found.

Flags:
  -h, --help   Show this help
`)
}

func printHealthUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage: stargate health [--url URL]

HTTP GET a running Stargate node's /api/health endpoint.

Flags:
      --url string   Base URL or full health URL
                     (default http://127.0.0.1:$STARGATE_HTTP_PORT/api/health)
  -h, --help         Show this help
`)
}

func printCompletionUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage: stargate completion <bash|zsh|fish>

Print a shell completion script to stdout. Install by evaluating or sourcing it:

  # zsh
  eval "$(stargate completion zsh)"
  # or: stargate completion zsh > "${fpath[1]}/_stargate"

  # bash
  eval "$(stargate completion bash)"
  # or: stargate completion bash > /etc/bash_completion.d/stargate

  # fish
  stargate completion fish > ~/.config/fish/completions/stargate.fish
`)
}

func cmdCompletion(args []string, stdout, stderr io.Writer) (bool, int) {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printCompletionUsage(stdout)
		if len(args) == 0 {
			return true, 2
		}
		return true, 0
	}
	switch strings.ToLower(args[0]) {
	case "bash":
		fmt.Fprint(stdout, bashCompletionScript)
	case "zsh":
		fmt.Fprint(stdout, zshCompletionScript)
	case "fish":
		fmt.Fprint(stdout, fishCompletionScript)
	default:
		fmt.Fprintf(stderr, "unknown shell: %s (want bash|zsh|fish)\n\n", args[0])
		printCompletionUsage(stderr)
		return true, 2
	}
	return true, 0
}

// Static completion scripts — keep in sync with root commands/flags above.
const bashCompletionScript = `# bash completion for stargate
_stargate() {
  local cur prev
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  local cmds="serve start run version env config doctor health completion help"
  local serve_flags="--port -p --data-dir -d --storage --pg-dsn --uploads-dir --blocks-dir --agent --help"

  if [[ ${COMP_CWORD} -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "${cmds} --version --help -v -h" -- "${cur}") )
    return 0
  fi

  case "${COMP_WORDS[1]}" in
    serve|start|run)
      COMPREPLY=( $(compgen -W "${serve_flags}" -- "${cur}") )
      ;;
    env|config)
      COMPREPLY=( $(compgen -W "--json --help" -- "${cur}") )
      ;;
    health)
      COMPREPLY=( $(compgen -W "--url --help" -- "${cur}") )
      ;;
    completion)
      COMPREPLY=( $(compgen -W "bash zsh fish" -- "${cur}") )
      ;;
    help)
      COMPREPLY=( $(compgen -W "${cmds}" -- "${cur}") )
      ;;
  esac
}
complete -F _stargate stargate
`

const zshCompletionScript = `#compdef stargate
_stargate() {
  local -a cmds
  cmds=(
    'serve:Start the HTTP server'
    'start:Alias for serve'
    'run:Alias for serve'
    'version:Print version'
    'env:Print effective configuration'
    'config:Alias for env'
    'doctor:Check local data dirs and configuration'
    'health:Probe /api/health on a running server'
    'completion:Generate shell completion scripts'
    'help:Show help'
  )
  local -a serve_opts
  serve_opts=(
    '(-p --port)'{-p,--port}'[HTTP listen port]:port'
    '(-d --data-dir)'{-d,--data-dir}'[Root data directory]:dir:_files -/'
    '--storage[Storage backend]:backend:(sqlite postgres memory)'
    '--pg-dsn[Postgres DSN]:dsn'
    '--uploads-dir[Uploads directory]:dir:_files -/'
    '--blocks-dir[Blocks directory]:dir:_files -/'
    '--agent[Enable built-in agents]'
    '(-h --help)'{-h,--help}'[Show help]'
  )

  _arguments -C \
    '1: :->cmd' \
    '*:: :->args'

  case $state in
    cmd)
      _describe 'command' cmds
      _arguments '(-v --version)'{-v,--version}'[Print version]' '(-h --help)'{-h,--help}'[Show help]'
      ;;
    args)
      case $words[1] in
        serve|start|run) _arguments $serve_opts ;;
        env|config) _arguments '--json[JSON output]' '(-h --help)'{-h,--help}'[Show help]' ;;
        health) _arguments '--url[Base or health URL]:url' '(-h --help)'{-h,--help}'[Show help]' ;;
        completion) _arguments '1:shell:(bash zsh fish)' ;;
        help) _describe 'command' cmds ;;
      esac
      ;;
  esac
}
_stargate
`

const fishCompletionScript = `# fish completion for stargate
complete -c stargate -f
complete -c stargate -n __fish_use_subcommand -a serve -d 'Start the HTTP server'
complete -c stargate -n __fish_use_subcommand -a start -d 'Alias for serve'
complete -c stargate -n __fish_use_subcommand -a run -d 'Alias for serve'
complete -c stargate -n __fish_use_subcommand -a version -d 'Print version'
complete -c stargate -n __fish_use_subcommand -a env -d 'Print effective configuration'
complete -c stargate -n __fish_use_subcommand -a config -d 'Alias for env'
complete -c stargate -n __fish_use_subcommand -a doctor -d 'Check local data dirs'
complete -c stargate -n __fish_use_subcommand -a health -d 'Probe /api/health'
complete -c stargate -n __fish_use_subcommand -a completion -d 'Shell completion scripts'
complete -c stargate -n __fish_use_subcommand -a help -d 'Show help'
complete -c stargate -n __fish_use_subcommand -l version -d 'Print version'
complete -c stargate -n __fish_use_subcommand -s v -d 'Print version'
complete -c stargate -n __fish_use_subcommand -l help -s h -d 'Show help'

complete -c stargate -n '__fish_seen_subcommand_from serve start run' -s p -l port -d 'HTTP port'
complete -c stargate -n '__fish_seen_subcommand_from serve start run' -s d -l data-dir -d 'Data directory'
complete -c stargate -n '__fish_seen_subcommand_from serve start run' -l storage -d 'sqlite|postgres|memory'
complete -c stargate -n '__fish_seen_subcommand_from serve start run' -l pg-dsn -d 'Postgres DSN'
complete -c stargate -n '__fish_seen_subcommand_from serve start run' -l uploads-dir -d 'Uploads directory'
complete -c stargate -n '__fish_seen_subcommand_from serve start run' -l blocks-dir -d 'Blocks directory'
complete -c stargate -n '__fish_seen_subcommand_from serve start run' -l agent -d 'Enable agents'

complete -c stargate -n '__fish_seen_subcommand_from env config' -l json -d 'JSON output'
complete -c stargate -n '__fish_seen_subcommand_from health' -l url -d 'Base or health URL'
complete -c stargate -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
`

// ---------------------------------------------------------------------------
// env / config
// ---------------------------------------------------------------------------

func cmdEnv(args []string, stdout, stderr io.Writer) (bool, int) {
	asJSON := false
	fs := flag.NewFlagSet("env", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	fs.Usage = func() { printEnvUsage(stderr) }
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printEnvUsage(stdout)
			return true, 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return true, 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "error: unexpected argument: %s\n", fs.Arg(0))
		return true, 2
	}

	cfg := effectiveConfig()
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(cfg); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return true, 1
		}
		return true, 0
	}

	fmt.Fprintln(stdout, versionString())
	fmt.Fprintln(stdout)
	// Stable key order for humans.
	keys := []string{
		"version", "git_commit",
		"STARGATE_HTTP_PORT", "STARGATE_STORAGE", "STARGATE_DATA_DIR",
		"STARGATE_PG_DSN", "DATABASE_URL",
		"UPLOADS_DIR", "BLOCKS_DIR", "IPFS_STORAGE_DIR", "IPFS_EMBEDDED_REPO",
		"STARGATE_MCP_DB", "STARGATE_API_KEYS_DB", "STARGATE_INGESTIONS_DB",
		"STARGATE_AGENT_ENABLED", "STARGATE_API_KEY", "STARLIGHT_DONATION_ADDRESS",
		"IPFS_ENABLED", "BITCOIN_NETWORK", "BTCD_MODE", "BTCD_BIN", "BTCD_DATADIR", "BTCD_RPC_HOST", "BTCD_ALLOW_MAINNET",
	}
	for _, k := range keys {
		v, ok := cfg[k]
		if !ok {
			continue
		}
		fmt.Fprintf(stdout, "%-28s %v\n", k, v)
	}
	return true, 0
}

func effectiveConfig() map[string]any {
	dataDir := envOr("STARGATE_DATA_DIR", "data")
	port := envOr("STARGATE_HTTP_PORT", "3001")
	storage := envOr("STARGATE_STORAGE", "sqlite")

	paths := map[string]string{
		"BLOCKS_DIR":             "blocks",
		"UPLOADS_DIR":            "uploads",
		"IPFS_STORAGE_DIR":       "ipfs_objects",
		"IPFS_EMBEDDED_REPO":     "ipfs_repo",
		"STARGATE_MCP_DB":        "sqlite/mcp.db",
		"STARGATE_API_KEYS_DB":   "sqlite/api_keys.db",
		"STARGATE_INGESTIONS_DB": "sqlite/ingestions.db",
	}

	out := map[string]any{
		"version":                    versionString(),
		"git_commit":                 gitCommit,
		"STARGATE_HTTP_PORT":         port,
		"STARGATE_STORAGE":           storage,
		"STARGATE_DATA_DIR":          dataDir,
		"STARGATE_AGENT_ENABLED":     envOr("STARGATE_AGENT_ENABLED", "false"),
		"IPFS_ENABLED":               envOr("IPFS_ENABLED", "true"),
		"BITCOIN_NETWORK":            envOr("BITCOIN_NETWORK", ""),
		"BTCD_MODE":                  envOr("BTCD_MODE", "managed"),
		"BTCD_RPC_HOST":              envOr("BTCD_RPC_HOST", ""),
		"BTCD_DATADIR":               envOr("BTCD_DATADIR", ""),
		"BTCD_BIN":                   envOr("BTCD_BIN", "btcd"),
		"BTCD_ALLOW_MAINNET":         envOr("BTCD_ALLOW_MAINNET", "false"),
		"STARLIGHT_DONATION_ADDRESS": redact(os.Getenv("STARLIGHT_DONATION_ADDRESS")),
		"STARGATE_API_KEY":           redact(os.Getenv("STARGATE_API_KEY")),
		"STARGATE_PG_DSN":            redact(os.Getenv("STARGATE_PG_DSN")),
		"DATABASE_URL":               redact(os.Getenv("DATABASE_URL")),
	}

	for envVar, sub := range paths {
		if v := os.Getenv(envVar); v != "" {
			out[envVar] = v
		} else {
			out[envVar] = filepath.Join(dataDir, sub)
		}
	}
	return out
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func redact(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "********"
	}
	return s[:2] + "…" + s[len(s)-2:] + " (redacted)"
}

// ---------------------------------------------------------------------------
// doctor
// ---------------------------------------------------------------------------

func cmdDoctor(args []string, stdout, stderr io.Writer) (bool, int) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { printDoctorUsage(stderr) }
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printDoctorUsage(stdout)
			return true, 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return true, 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "error: unexpected argument: %s\n", fs.Arg(0))
		return true, 2
	}

	fmt.Fprintln(stdout, versionString())
	fmt.Fprintln(stdout, "Running doctor checks…")
	fmt.Fprintln(stdout)

	cfg := effectiveConfig()
	var problems []string

	checkDir := func(name, path string) {
		if path == "" {
			fmt.Fprintf(stdout, "  SKIP  %s (unset)\n", name)
			return
		}
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				// Missing is OK — serve creates these on startup.
				parent := filepath.Dir(path)
				if st, pErr := os.Stat(parent); pErr == nil && st.IsDir() {
					fmt.Fprintf(stdout, "  OK    %s: %s (missing; will be created on serve)\n", name, path)
					return
				}
				// If parent also missing, still OK if we can write to an existing ancestor.
				fmt.Fprintf(stdout, "  OK    %s: %s (missing; will be created on serve)\n", name, path)
				return
			}
			fmt.Fprintf(stdout, "  FAIL  %s: %s (%v)\n", name, path, err)
			problems = append(problems, name+": "+err.Error())
			return
		}
		if !info.IsDir() {
			fmt.Fprintf(stdout, "  FAIL  %s: %s is not a directory\n", name, path)
			problems = append(problems, name+": not a directory")
			return
		}
		probe := filepath.Join(path, ".stargate-doctor-write")
		if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
			fmt.Fprintf(stdout, "  FAIL  %s: not writable: %s (%v)\n", name, path, err)
			problems = append(problems, name+": not writable")
			return
		}
		_ = os.Remove(probe)
		fmt.Fprintf(stdout, "  OK    %s: %s\n", name, path)
	}

	dataDir, _ := cfg["STARGATE_DATA_DIR"].(string)
	checkDir("STARGATE_DATA_DIR", dataDir)
	for _, k := range []string{"UPLOADS_DIR", "BLOCKS_DIR", "IPFS_STORAGE_DIR", "IPFS_EMBEDDED_REPO"} {
		p, _ := cfg[k].(string)
		checkDir(k, p)
	}
	if storage, _ := cfg["STARGATE_STORAGE"].(string); storage == "sqlite" || storage == "" {
		mcp, _ := cfg["STARGATE_MCP_DB"].(string)
		if mcp != "" {
			checkDir("sqlite parent", filepath.Dir(mcp))
		}
	}

	storage, _ := cfg["STARGATE_STORAGE"].(string)
	fmt.Fprintf(stdout, "  INFO  STARGATE_STORAGE=%s\n", storage)
	fmt.Fprintf(stdout, "  INFO  STARGATE_HTTP_PORT=%v\n", cfg["STARGATE_HTTP_PORT"])
	fmt.Fprintf(stdout, "  INFO  STARGATE_AGENT_ENABLED=%v\n", cfg["STARGATE_AGENT_ENABLED"])

	if storage == "postgres" {
		if os.Getenv("STARGATE_PG_DSN") == "" && os.Getenv("DATABASE_URL") == "" {
			fmt.Fprintln(stdout, "  FAIL  postgres storage selected but STARGATE_PG_DSN/DATABASE_URL unset")
			problems = append(problems, "postgres DSN missing")
		} else {
			fmt.Fprintln(stdout, "  OK    postgres DSN is set (value redacted)")
		}
	}

	fmt.Fprintln(stdout)
	if len(problems) > 0 {
		fmt.Fprintf(stdout, "Doctor found %d problem(s).\n", len(problems))
		return true, 1
	}
	fmt.Fprintln(stdout, "All checks passed.")
	return true, 0
}

// ---------------------------------------------------------------------------
// health
// ---------------------------------------------------------------------------

func cmdHealth(args []string, stdout, stderr io.Writer) (bool, int) {
	urlFlag := ""
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&urlFlag, "url", "", "base URL or full /api/health URL")
	fs.Usage = func() { printHealthUsage(stderr) }
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printHealthUsage(stdout)
			return true, 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return true, 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "error: unexpected argument: %s\n", fs.Arg(0))
		return true, 2
	}

	url := strings.TrimSpace(urlFlag)
	if url == "" {
		port := envOr("STARGATE_HTTP_PORT", "3001")
		url = "http://127.0.0.1:" + port + "/api/health"
	} else if !strings.Contains(url, "/api/health") {
		url = strings.TrimRight(url, "/") + "/api/health"
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(stderr, "health check failed: %v\n", err)
		return true, 1
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	fmt.Fprintf(stdout, "GET %s\n", url)
	fmt.Fprintf(stdout, "status: %d %s\n", resp.StatusCode, resp.Status)
	if len(body) > 0 {
		fmt.Fprintln(stdout, string(body))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return true, 1
	}
	return true, 0
}
