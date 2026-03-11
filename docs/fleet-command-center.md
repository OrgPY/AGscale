# Fleet Command Center (Local + SSH Agent Discovery)

This mode is outside Kubernetes CR orchestration.
It lets AGScale CLI discover agent software on local/SSH hosts and schedule tasks to a subset of discovered agents.

## 1) Scan and persist inventory

Local only:

```bash
./bin/agscale fleet scan --local --scan-mode agent-aware --write
```

Local + SSH hosts:

```bash
./bin/agscale fleet scan \
  --local \
  --scan-mode agent-aware \
  --hosts devbox-1,devbox-2 \
  --ssh-config ~/.ssh/config \
  --write
```

Inventory path defaults to:

`~/.agscale/inventory/agents.json`

## 2) View discovered agents

```bash
./bin/agscale fleet list
./bin/agscale fleet list --status healthy --protocol http
./bin/agscale fleet inspect <agent-id-or-name>
```

## 3) Schedule task on subset

```bash
./bin/agscale fleet run incident-triage \
  --selector 'scope=local' \
  --max-agents 4 \
  --topology dag \
  --explain-selection
```

Dry-run without invoking endpoints:

```bash
./bin/agscale fleet run incident-triage \
  --selector 'scope=local' \
  --max-agents 4 \
  --topology dag \
  --explain-selection \
  --dry-run
```

## Notes

- Phase 1 protocols: HTTP/OpenAI-compatible and MCP.
- SSH probing uses existing key-based auth from `~/.ssh/config`.
- Inventory is local persisted state (not auto-registered to Kubernetes CRDs).
- Default mode is `agent-aware`:
  - finds agent-like processes by signatures (`openclaw`, `opencode`, `claude`, `aider`, `mcp`, ...)
  - correlates with listening ports, then probes only those endpoints
- Use `--scan-mode ports` only when you explicitly want broad port sweep behavior.
