package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	origV, origC := version, gitCommit
	t.Cleanup(func() {
		version, gitCommit = origV, origC
	})

	version, gitCommit = "v1.2.3", "unknown"
	if got := versionString(); got != "stargate v1.2.3" {
		t.Fatalf("version only: got %q", got)
	}

	version, gitCommit = "v1.2.3", "abc1234"
	if got := versionString(); got != "stargate v1.2.3 (abc1234)" {
		t.Fatalf("version+commit: got %q", got)
	}

	version, gitCommit = "", "unknown"
	if got := versionString(); got != "stargate dev" {
		t.Fatalf("empty version: got %q", got)
	}
}

func TestRunCLINoArgs(t *testing.T) {
	var out, err bytes.Buffer
	handled, code := runCLI(nil, &out, &err)
	if handled || code != 0 {
		t.Fatalf("nil args: handled=%v code=%d", handled, code)
	}
	handled, code = runCLI([]string{}, &out, &err)
	if handled || code != 0 {
		t.Fatalf("empty args: handled=%v code=%d", handled, code)
	}
}

func TestRunCLIVersionAndHelp(t *testing.T) {
	origV, origC := version, gitCommit
	version, gitCommit = "v9.9.9", "deadbeef"
	t.Cleanup(func() {
		version, gitCommit = origV, origC
	})

	cases := []struct {
		name       string
		args       []string
		wantOut    string
		wantErrSub string
		wantCode   int
		wantHandle bool
	}{
		{name: "double-dash-version", args: []string{"--version"}, wantOut: "stargate v9.9.9 (deadbeef)", wantCode: 0, wantHandle: true},
		{name: "version-cmd", args: []string{"version"}, wantOut: "stargate v9.9.9 (deadbeef)", wantCode: 0, wantHandle: true},
		{name: "short-v", args: []string{"-v"}, wantOut: "stargate v9.9.9 (deadbeef)", wantCode: 0, wantHandle: true},
		{name: "help-flag", args: []string{"--help"}, wantOut: "Usage: stargate", wantCode: 0, wantHandle: true},
		{name: "help-cmd", args: []string{"help"}, wantOut: "Usage: stargate", wantCode: 0, wantHandle: true},
		{name: "short-h", args: []string{"-h"}, wantOut: "Usage: stargate", wantCode: 0, wantHandle: true},
		{name: "unknown-flag", args: []string{"--bogus"}, wantErrSub: "unknown flag", wantCode: 2, wantHandle: true},
		{name: "unknown-cmd", args: []string{"serve"}, wantErrSub: "unknown command", wantCode: 2, wantHandle: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			handled, code := runCLI(tc.args, &stdout, &stderr)
			if handled != tc.wantHandle {
				t.Fatalf("handled: got %v want %v", handled, tc.wantHandle)
			}
			if code != tc.wantCode {
				t.Fatalf("exit code: got %d want %d\nstdout=%q\nstderr=%q", code, tc.wantCode, stdout.String(), stderr.String())
			}
			if tc.wantOut != "" && !strings.Contains(stdout.String(), tc.wantOut) {
				t.Fatalf("stdout missing %q: %q", tc.wantOut, stdout.String())
			}
			if tc.wantErrSub != "" && !strings.Contains(stderr.String(), tc.wantErrSub) {
				t.Fatalf("stderr missing %q: %q", tc.wantErrSub, stderr.String())
			}
			// Unknown paths should also print usage on stderr.
			if tc.wantCode == 2 && !strings.Contains(stderr.String(), "Usage: stargate") {
				t.Fatalf("stderr missing usage: %q", stderr.String())
			}
		})
	}
}
