# Team Builder Quickstart

This guide shows how to use AGScale team-builder CLI to create and run an agent team quickly.
For cluster install order and operator troubleshooting baseline, see `docs/runbook-openclaw-operator.md`.

## 0) Prerequisites

- AGScale CRDs/controller installed
- At least one `Agent` already exists (example uses `support-agent`)

Check:

```bash
./bin/agscale cluster doctor -n default
./bin/agscale cluster get agents -n default
```

## 1) Build CLI

```bash
mkdir -p .cache/go-build
GOCACHE=$(pwd)/.cache/go-build make build-cli
```

## 2) Create a team bundle with wizard

```bash
./bin/agscale team init support-team-easy -n default --profile openclaw --out support-team-easy.yaml
```

Wizard collects:

- entry agent
- step DAG (`dependsOn`)
- protocol (`http`/`mcp`/`a2a`)
- optional `integrationRef` for `a2a` steps
- optional policy refs
- optional `EventSource` + `IntegrationEndpoint`

## 3) Validate generated YAML

```bash
./bin/agscale team validate -f support-team-easy.yaml -n default
```

## 4) Apply and run

Option A: apply manually, then run manually

```bash
kubectl apply -n default -f support-team-easy.yaml
./bin/agscale team run support-team-easy -n default
```

Option B: one-command guided create + apply + first run

```bash
./bin/agscale team up support-team-easy -n default --profile openclaw --out support-team-easy.yaml
```

## 5) Watch the run in real time

```bash
./bin/agscale cluster get runs -n default
./bin/agscale team watch <RUN_NAME> -n default
```

`team watch` shows:

- workflow phase
- step status/duration/error
- policy decisions
- failure hint (if failed)

## 6) Existing static demo

If you want a non-wizard sample, use:

```bash
kubectl apply -k examples/team-builder/openclaw-basic
./bin/agscale team run support-team-basic -n default
```

For A2A + remote integration example:

```bash
kubectl apply -k examples/team-builder/openclaw-a2a
./bin/agscale team watch support-team-a2a-run-01 -n default
```

## 7) Command center auto-team (subset scheduling)

Build a team from existing agents without using all agents:

```bash
./bin/agscale team command-center incident-401 \
  -n default \
  --selector 'agscale.io/domain=support' \
  --require-skills web-search \
  --max-agents 3 \
  --topology dag \
  --protocol http \
  --out incident-401-team.yaml \
  --explain-selection \
  --apply --run
```

Behavior:

- selects only ready agents (`readyReplicas > 0` and status URL available)
- applies label/skill filters and excludes non-matching agents
- chooses top N (`--max-agents`) and generates an `AgentWorkflow`
- default topology is `dag` (set `--topology sequential` if needed)
- `--explain-selection` prints per-agent inclusion/exclusion reasons
- optionally applies and creates first run
