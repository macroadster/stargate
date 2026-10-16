package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"stargate-backend/tui"
)

func printConsoleUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage: stargate console [--url URL] [--once] [--view welcome|monitor|logs]
       stargate status   # alias
       stargate tui      # alias

Attach a status console to a running Stargate node.

The welcome view tells you which URL to open in a browser. The monitor view
is the live chain, scanner, block monitor, IPFS mirror, and agents. Press
q to disconnect. The node keeps running.

With no TTY, or with --once, print one snapshot and exit.

Flags:
      --url string    Base URL, /api/health URL, or /api/console/status URL
                      (default http://127.0.0.1:$STARGATE_HTTP_PORT)
      --once          Print one snapshot and exit
      --view string   welcome, monitor, or logs
  -h, --help          Show this help

Keys (full screen):
  w welcome    m monitor    l logs    o open browser
  ? help       q disconnect
`)
}

func cmdConsole(args []string, stdout, stderr io.Writer) (bool, int) {
	urlFlag := ""
	viewFlag := ""
	once := false
	fs := flag.NewFlagSet("console", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&urlFlag, "url", "", "base URL of a running node")
	fs.BoolVar(&once, "once", false, "print one snapshot and exit")
	fs.StringVar(&viewFlag, "view", "", "welcome, monitor, or logs")
	fs.Usage = func() { printConsoleUsage(stderr) }
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printConsoleUsage(stdout)
			return true, 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return true, 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "error: unexpected argument: %s\n", fs.Arg(0))
		return true, 2
	}

	base := strings.TrimSpace(urlFlag)
	if base == "" {
		base = "http://127.0.0.1:" + envOr("STARGATE_HTTP_PORT", "3001")
	}
	view := tui.ViewFromEnv(viewFlag)
	if strings.TrimSpace(viewFlag) == "" {
		view = tui.ViewFromEnv(os.Getenv("STARGATE_TUI_VIEW"))
	}

	fetch := func() tui.Snapshot {
		snap, err := tui.Fetch(base, 3*time.Second)
		if err != nil {
			return tui.Unreachable(base, err)
		}
		return snap
	}

	outFile, _ := stdout.(*os.File)
	interactive := !once && outFile != nil && tui.IsTerminal(outFile) && tui.IsTerminal(os.Stdin)
	if !interactive {
		snap := fetch()
		fmt.Fprint(stdout, tui.FormatPlain(snap))
		if snap.FetchError != "" {
			fmt.Fprintf(stderr, "console: %s\n", snap.FetchError)
			return true, 1
		}
		return true, 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	src := &tui.PollSource{Interval: 2 * time.Second, Fetch: fetch}
	src.Start(ctx)
	err := tui.Run(ctx, src, outFile, os.Stdin, tui.Options{
		View:   view,
		Color:  tui.ColorEnabled(),
		ASCII:  tui.ASCIIRequested(),
		Attach: true,
		OnQuit: stop,
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(stderr, "console: %v\n", err)
		return true, 1
	}
	return true, 0
}
