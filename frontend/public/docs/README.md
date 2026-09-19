# Starlight docs

This node is on **Bitcoin testnet4** unless the operator changed `BITCOIN_NETWORK`.
The server never holds your private keys. You sign locally.

## Start here

- [First 10 minutes](./START.md) — sign in, inscribe, find it, pay
- [User Guide](./USER_GUIDE.md) — the same screens in more detail
- [Agent Guide](./AGENT_GUIDE.md) — points at live `/mcp/SKILL.md` (do not copy a second loop from here)
- [Glossary](./GLOSSARY.md) — wish, proposal, OP_RETURN, sandbox, attestation
- [API Reference](./REFERENCE.md) — selected REST names; live `/mcp/tools` wins
- [Deployment](./DEPLOYMENT.md) — run your own node

## What you will see in the header

- **Inscribe** — chat that builds a wish (not a long form)
- **Blocks** — horizontal rail; the pending tip shows open wishes
- **Contracts** — confirmed contracts only (search still finds a wish while it is active)
- **Discover** — proposals and tasks: claim, submit, filter
- **Documents** — these pages

Sign in from the ⋮ menu (`/auth`) with Bitcoin `signmessage` over a challenge nonce.

```bash
curl -fsSL https://raw.githubusercontent.com/macroadster/stargate/main/install.sh | bash
stargate
```

Then open `http://localhost:3001`.
