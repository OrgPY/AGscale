# Support Ticket 401 Demo

Scenario:

- Customer ticket: "After upgrading to v2.3, API returns 401 for valid tokens."
- Team goal:
  1. `triage`: classify severity and impact
  2. `retrieval`: gather likely causes/known fixes
  3. `response`: produce mitigation + root-cause hypothesis + permanent fix plan

## Prerequisites

- AGScale CRDs/controller installed
- `Agent` named `support-agent` exists in `default` namespace

## Run

```bash
kubectl apply -k examples/team-builder/support-ticket-401
./bin/agscale team watch support-ticket-401-run -n default
```

## Verify

```bash
./bin/agscale team status support-ticket-401-run -n default
./bin/agscale team graph support-ticket-401-team -n default
```

Expected:

- run phase reaches `Succeeded`
- step order follows DAG:
  - `triage`
  - `retrieval` (dependsOn `triage`)
  - `response` (dependsOn `triage,retrieval`)
