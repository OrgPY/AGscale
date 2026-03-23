# Fleet Command Center (Local + SSH Agent Discovery)

This mode is outside Kubernetes CR orchestration.
It lets AGScale CLI discover agent software on local/SSH hosts and schedule tasks to a subset of discovered agents.

## 1) Scan and persist fleet state

Local only:

```bash
./bin/agscale fleet agent scan --local --scan-mode agent-aware --write
```

Register remote hosts once:

```bash
./bin/agscale fleet host add devbox-1 --address devbox-1 --labels scope=ssh,env=dev
./bin/agscale fleet host add devbox-2 --address devbox-2 --labels scope=ssh,env=dev
```

Local + registered SSH hosts:

```bash
./bin/agscale fleet agent scan \
  --local \
  --scan-mode agent-aware \
  --ssh-config ~/.ssh/config \
  --write
```

Agent inventory now defaults to:

`~/.agscale/fleet/agents.json`

Additional host and run state is stored beside it in:

`~/.agscale/fleet/hosts.json`

`~/.agscale/fleet/runs.json`

Legacy `~/.agscale/inventory/agents.json` is still read automatically when the new path does not exist yet.

## 2) View hosts and discovered agents

```bash
./bin/agscale fleet host list
./bin/agscale fleet host doctor --write
./bin/agscale fleet agent list
./bin/agscale fleet agent list --status healthy --protocol http
./bin/agscale fleet agent inspect <agent-id-or-name>
./bin/agscale fleet agent pin <agent-id-or-name> --endpoint https://agent.example --reachability direct
./bin/agscale fleet agent unpin <agent-id-or-name>
./bin/agscale fleet agent chat <agent-id-or-name> --message "hello" --ssh-config ~/.ssh/config
```

## 3) Schedule task on subset

```bash
./bin/agscale fleet run dispatch incident-triage \
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

Run history:

```bash
./bin/agscale fleet run history --limit 20
./bin/agscale fleet run inspect <run-id>
```

## Notes

- Phase 1 protocols: HTTP/OpenAI-compatible and MCP.
- OpenAI-compatible endpoints use the first model discovered from `/v1/models`; if the endpoint hides models behind auth, set `AGSCALE_OPENAI_MODEL` (or `AGSCALE_OPENAI_MODEL_<AGENT_NAME>`).
- SSH probing uses existing key-based auth from `~/.ssh/config`.
- Remote agents discovered as `http://127.0.0.1:<port>` or `http://localhost:<port>` are treated as `ssh-loopback` and are invoked through ephemeral SSH local port-forwards during `fleet run` and `fleet agent chat`.
- Use `fleet agent pin` when discovery is not deterministic or you need to override a discovered endpoint with an explicitly managed one.
- Fleet state is local persisted state (not auto-registered to Kubernetes CRDs).
- Default mode is `agent-aware`:
  - finds agent-like processes by signatures (`openclaw`, `opencode`, `claude`, `aider`, `mcp`, ...)
  - correlates with listening ports, then probes only those endpoints
- Use `--scan-mode ports` only when you explicitly want broad port sweep behavior.
