package main

import (
	"fmt"
	"io"
	"os"
	"strings"
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

// runCLI inspects args for non-server invocations (version, help).
//
//	handled == false → start the HTTP server (no CLI args)
//	handled == true  → CLI fully handled; caller should os.Exit(exitCode)
//
// install.sh (and users) call `stargate --version`; without this dispatch the
// binary would start the full server and hang forever.
func runCLI(args []string, stdout, stderr io.Writer) (handled bool, exitCode int) {
	if len(args) == 0 {
		return false, 0
	}

	switch args[0] {
	case "version", "--version", "-version", "-v":
		fmt.Fprintln(stdout, versionString())
		return true, 0
	case "help", "--help", "-help", "-h":
		printUsage(stdout)
		return true, 0
	default:
		if strings.HasPrefix(args[0], "-") {
			fmt.Fprintf(stderr, "unknown flag: %s\n\n", args[0])
		} else {
			fmt.Fprintf(stderr, "unknown command: %s\n\n", args[0])
		}
		printUsage(stderr)
		return true, 2
	}
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

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage: stargate [command]

Stargate is a single-binary Bitcoin-native coordination server (UI + API + MCP).

Commands:
  (none)     Start the HTTP server (default)
  version    Print version and exit
  help       Show this help

Flags:
  --version, -v    Same as "version"
  --help, -h       Same as "help"

Environment (common):
  STARGATE_HTTP_PORT   Listen port (default 3001)
  STARGATE_STORAGE     sqlite (default) | postgres | memory
  STARGATE_DATA_DIR    Data directory root
`)
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
