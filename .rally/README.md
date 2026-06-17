# Rally Data Directory

This directory contains rally's workspace configuration and local runtime data.

## Tracked Files
- `config.toml` — Agent model configuration and runtime settings
- `agents/` — Role instruction files
- `README.md` — This guide
- `summary.jsonl` — Append-only run summary digest, when enabled by the current workflow

## Local State

Machine-managed runtime records live under `.rally/state/`. That directory is gitignored and not shared through repository history.

- `state/tries.jsonl` — One line per agent execution attempt
- `state/messages.jsonl` — Inbox messages for agents
- `state/relays.jsonl` — Relay session records
- `state/agent_status.jsonl` — Agent pause/freeze state history
- `state/hook-audit.jsonl` — Laps hook audit trail
- `state/run-state.json` — Current run handoff and lap recording state
- `state/current_task.md` — Most recent assembled prompt

## Quick Reference for Agents
- View recent tries (last 10): `tail -10 .rally/state/tries.jsonl | jq .`
- View pending messages: `cat .rally/state/messages.jsonl | jq 'select(.status==\"pending\")'`
- View current relay status: `tail -1 .rally/state/relays.jsonl | jq .`
- Counts: `wc -l .rally/state/*.jsonl`
