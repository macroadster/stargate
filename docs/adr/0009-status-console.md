# ADR 0009: Status console on a terminal

- **Status:** Accepted
- **Date:** 2026-10-16
- **Deciders:** Stargate maintainers
- **Tags:** operator, cli, transport

## Context

`stargate` with no arguments starts the node. A new operator needs to be told
to open the browser UI. Someone who already runs a node wants the chain tip,
peer count, scanner, block monitor, IPFS mirror, and agent switch without
tailing JSON access logs.

Docker and systemd capture stdout as a log. A full-screen console there would
hide those logs.

## Decision

When stdin and stdout are a terminal, `TERM` is not `dumb`, and
`STARGATE_TUI` is not `off`, the process draws a status console instead of
streaming logs.

- The welcome view prints the browser URL, the guides, and a short status.
- The monitor view is the live chain, scanner, block monitor, IPFS mirror,
  and agents. The log view is the process log.
- `q` or Ctrl-C stops the node. `o` opens the browser URL.
- `stargate console` (aliases `status`, `tui`) attaches to a running node
  through `GET /api/console/status`. `q` disconnects and leaves the node up.
- No TTY, `STARGATE_TUI=off`, or `--no-tui` keeps the log stream.
- The status payload has no DSN, RPC password, or API key.

`backend/tui` draws and polls. The process collects the sample. The package
does not own chain or storage rules.

## Consequences

**Positive**

- A first `stargate` in a terminal shows where the UI is.
- Operators can watch sync, lag, and the mirror without another tool.
- Pod logs stay line-oriented.

**Costs**

- A terminal session no longer shows a raw log unless `--no-tui` is set.
  The log view and `GET /api/console/status` still carry recent lines.
- Attaching costs one local HTTP poll every two seconds. That poll is not
  repeated in the log panel.

## Related

- [CONSOLE.md](../arch/CONSOLE.md)
- [ENV.md](../arch/ENV.md)
- ADR 0001 (single binary), ADR 0006 (btcd)
