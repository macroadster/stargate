# Status console

Status: **canonical**

`stargate` in a terminal is a status console. A new operator gets the browser
URL. A running node shows chain, scanner, block monitor, IPFS mirror, and
agents as they change.

A pipe, Docker, or systemd session keeps the log stream. `kubectl logs` stays
line-oriented.

## Run

```bash
stargate                  # serve, and take the terminal when it is a TTY
stargate serve --no-tui   # serve, logs only
stargate console          # attach to a node already running on this machine
stargate status --once    # one text snapshot, for a script
stargate console --url http://127.0.0.1:3001 --view monitor
```

`tui` and `status` are aliases of `console`.

Quit a local server with `q` or Ctrl-C. `q` on an attached console
disconnects. The node keeps running.

## Keys

| Key | Action |
| --- | --- |
| `w` or `1` | Welcome: open this URL in a browser |
| `m` or `2` | Monitor: chain, node, block monitor, mirror |
| `l` or `3` | Recent process log |
| `tab` | Cycle those views |
| `o` | Open the browser UI (`open` / `xdg-open`) |
| `?` or `h` | Key help |
| `q` | Stop the local node, or disconnect an attached console |
| `esc` | Close help |

`STARGATE_TUI_VIEW=monitor` opens the monitor instead of the welcome.
`STARGATE_TUI_VIEW=logs` opens the log.

## What the monitor shows

The sample is `GET /api/console/status`. The in-process screen reads the same
struct without a round trip, so it can show "starting Bitcoin node" before
the port is open.

| Row | Source |
| --- | --- |
| Chain height, headers, peers, IBD, hash | chain `NodeStatus` |
| Lag / fork | external tip check (read-only) |
| Scanner | process scanner manager |
| Block monitor | in-memory counters (no storage read) |
| IPFS mirror | uploads topic, wish topic, mesh peers |
| Agents | the same `STARGATE_AGENT_*` switch the orchestrator uses |

The payload does not include a DSN, RPC password, or API key. Recent logs
and the data directory are sent only to a loopback client.

A node that does not have `/api/console/status` yet is read from `/api/health`,
`/bitcoin/v1/health`, and `/api/ipfs-mirror/status`. That fallback has no
block-monitor counters.

## Environment

| Variable | Default | Meaning |
| --- | --- | --- |
| `STARGATE_TUI` | `auto` | `off` / `0` / `false` keeps logs even on a TTY |
| `STARGATE_TUI_VIEW` | `welcome` | `monitor` or `logs` |
| `STARGATE_TUI_COLOR` | on | `off` disables ANSI color. `NO_COLOR` does too |
| `STARGATE_TUI_ASCII` | off | `1` uses `+` borders |

`--no-tui` sets `STARGATE_TUI=off` for that process.

## Related

- [ADR 0009](../adr/0009-status-console.md)
- [ENV.md](./ENV.md)
