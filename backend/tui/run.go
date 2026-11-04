package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ErrNotATTY is returned when the console is asked to take a screen that is
// not a terminal. Callers print FormatPlain instead.
var ErrNotATTY = errors.New("stdout is not a terminal")

// Options controls one Run.
type Options struct {
	View   View
	Color  bool
	ASCII  bool
	Attach bool
	// OnQuit runs when the operator presses q (or Ctrl-C delivered as a byte).
	// For the server process this cancels the server. For an attached console
	// it only ends the screen.
	OnQuit func()
	// OnReady runs after the alternate screen is up. The server uses it to
	// stop mirroring logs onto the terminal.
	OnReady func()
	// OnExit runs after the terminal is restored.
	OnExit func()
}

// ShouldRun reports whether this process should take the terminal.
// A pipe, a dumb TERM, or STARGATE_TUI=off keeps the log stream.
func ShouldRun(out, in *os.File) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("STARGATE_TUI"))) {
	case "0", "false", "off", "no":
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return IsTerminal(out) && IsTerminal(in)
}

// ColorEnabled honors NO_COLOR and STARGATE_TUI_COLOR.
func ColorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("STARGATE_TUI_COLOR"))) {
	case "0", "false", "off", "no":
		return false
	}
	return os.Getenv("TERM") != "dumb"
}

// ASCIIRequested is STARGATE_TUI_ASCII=1.
func ASCIIRequested() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("STARGATE_TUI_ASCII"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// ViewFromEnv maps STARGATE_TUI_VIEW. Unknown values open the welcome.
func ViewFromEnv(v string) View {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "monitor", "m", "status":
		return ViewMonitor
	case "logs", "log", "l":
		return ViewLogs
	default:
		return ViewWelcome
	}
}

// Run takes the terminal and draws until ctx is canceled or the operator quits.
// out and in must be the real terminal files. A pipe returns ErrNotATTY.
func Run(ctx context.Context, src Source, out, in *os.File, opt Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if src == nil {
		src = NewHub()
	}
	if out == nil || in == nil || !IsTerminal(out) || !IsTerminal(in) {
		return ErrNotATTY
	}
	restore, err := enableRaw(in)
	if err != nil {
		return err
	}
	defer restore()
	fmt.Fprint(out, "\x1b[?1049h\x1b[?25l")
	defer fmt.Fprint(out, "\x1b[?25h\x1b[?7h\x1b[0m\x1b[?1049l")
	if opt.OnReady != nil {
		opt.OnReady()
	}
	defer func() {
		if opt.OnExit != nil {
			opt.OnExit()
		}
	}()

	keys := make(chan byte, 32)
	go readKeys(in, keys)

	sess := session{view: opt.View}
	if sess.view == "" {
		sess.view = ViewWelcome
	}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()

	draw := func() {
		w, h := terminalSize(out)
		snap := src.Snapshot()
		vs := ViewState{
			View:   sess.view,
			Help:   sess.help,
			Color:  opt.Color,
			ASCII:  opt.ASCII,
			Attach: opt.Attach,
			Flash:  sess.flash,
		}
		if sess.flash != "" && time.Since(sess.flashAt) > 4*time.Second {
			sess.flash = ""
			vs.Flash = ""
		}
		frame := Render(snap, vs, w, h)
		fmt.Fprint(out, frame)
	}
	draw()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			draw()
		case b := <-keys:
			if b == 0x1b {
				sess.help = false
				if !discardEscape(keys) {
					// lone esc already closed help
				}
				draw()
				continue
			}
			quit, open := applyKey(&sess, b)
			if quit {
				if opt.OnQuit != nil {
					opt.OnQuit()
				}
				return nil
			}
			if open {
				snap := src.Snapshot()
				if err := openBrowser(snap.UIURL); err != nil {
					sess.flash = "could not open the browser: " + err.Error()
				} else {
					sess.flash = "opened " + snap.UIURL
				}
				sess.flashAt = time.Now()
			}
			draw()
		}
	}
}

type session struct {
	view    View
	help    bool
	flash   string
	flashAt time.Time
}

func applyKey(s *session, k byte) (quit, open bool) {
	switch k {
	case 'q', 'Q', 0x03:
		return true, false
	case 'w', 'W', '1':
		s.view = ViewWelcome
		s.help = false
	case 'm', 'M', '2':
		s.view = ViewMonitor
		s.help = false
	case 'l', 'L', '3':
		s.view = ViewLogs
		s.help = false
	case '?', 'h', 'H':
		s.help = !s.help
	case 'o', 'O':
		return false, true
	case '\t':
		if s.help {
			s.help = false
			return false, false
		}
		switch s.view {
		case ViewWelcome:
			s.view = ViewMonitor
		case ViewMonitor:
			s.view = ViewLogs
		default:
			s.view = ViewWelcome
		}
	case 0x1b:
		s.help = false
	}
	return false, false
}

func readKeys(in *os.File, out chan<- byte) {
	buf := make([]byte, 1)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			select {
			case out <- buf[0]:
			default:
			}
		}
		if err != nil {
			return
		}
	}
}

// discardEscape waits briefly so an arrow-key sequence does not become commands.
// It returns true when extra bytes were consumed.
func discardEscape(keys <-chan byte) bool {
	timer := time.NewTimer(20 * time.Millisecond)
	defer timer.Stop()
	extra := false
	for {
		select {
		case <-keys:
			extra = true
		case <-timer.C:
			return extra
		}
	}
}

func openBrowser(raw string) error {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return errors.New("no UI URL yet")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", raw)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	default:
		cmd = exec.Command("xdg-open", raw)
	}
	return cmd.Start()
}
