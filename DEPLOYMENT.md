# Deployment runbook

ScaleOfCoherence runs as a single Go container (API + embedded SPA + embedded
migrations) on **Azure Container Apps**, backed by managed **PostgreSQL**.
Frontend assets and migrations ship inside the image, so there is nothing else
to deploy.

- `deploy/provision.sh` — one-time (and safe-to-rerun) Azure provisioning.
- `.github/workflows/deploy.yml` — staged rollout on every push to `main`.
- `.github/workflows/ci.yml` — gofmt/vet/tests/govulncheck, tsc/eslint/audit,
  Docker build.

## 1. Release flow

```text
push to main
  → CI (ci.yml): build, vet, test, govulncheck, tsc, eslint, npm audit, image build
  → build image and push ghcr.io/<owner>/<repo>:<sha>
  → migrate job:  docker run <image> -migrate-only   (against production DB)
  → deploy job:   new revision at 0% traffic
                  poll the revision's /readyz until 200 (max 5 min)
                  shift 100% traffic to the new revision
  → on failure:   the unhealthy revision is deactivated, traffic stays on the old one
```

Migrations are forward-only and must stay backward compatible with the
currently running revision (add columns/tables; never drop or rename in the
same release). Rollback is therefore always "shift traffic back", never "run
down-migrations".

## 2. Environments and secrets

| Name | Kind | Used by | Notes |
|---|---|---|---|
| `AZURE_CLIENT_ID` | repo secret | deploy job | federated identity (OIDC, no client secret) |
| `AZURE_TENANT_ID` | repo secret | deploy job | |
| `AZURE_SUBSCRIPTION_ID` | repo secret | deploy job | |
| `AZURE_APP_NAME` | repo variable | deploy job | e.g. `scaleco` |
| `AZURE_RESOURCE_GROUP` | repo variable | deploy job | e.g. `rg-scaleco` |
| `DATABASE_URL` | repo secret | migrate job | production DSN, `sslmode=require` |
| `JWT_SECRET` | repo secret | migrate job | must match the Container App secret |
| `GHCR_TOKEN` | script env only | provision.sh | PAT with `read:packages` |

Create the OIDC federated credential once (no long-lived Azure secrets):

```bash
az ad app create --display-name scaleco-deploy
APP_ID=$(az ad app list --display-name scaleco-deploy --query '[0].appId' -o tsv)
az ad sp create --id "$APP_ID"

az ad app federated-credential create --id "$APP_ID" --parameters '{
  "name": "github-main",
  "issuer": "https://token.actions.githubusercontent.com",
  "subject": "repo:<owner>/<repo>:ref:refs/heads/main",
  "audiences": ["api://AzureADTokenExchange"]
}'

az role assignment create --assignee "$APP_ID" \
  --role "Contributor" --scope "/subscriptions/<subscription-id>"
```

Set the GitHub secrets/variables above to the printed `APP_ID`, tenant and
subscription IDs.

## 3. First-time provisioning

```bash
export RESOURCE_GROUP=rg-scaleco
export APP_NAME=scaleco
export LOCATION=westeurope
export CONTAINERAPP_ENV=cae-scaleco
export IMAGE=ghcr.io/<owner>/<repo>:<sha>   # any tag pushed by CI
export GHCR_USER=<owner>
export GHCR_TOKEN=<PAT with read:packages>
export DATABASE_URL='postgres://scaleco:<password>@<host>:5432/scaleco?sslmode=require'
export JWT_SECRET=$(openssl rand -base64 48)

az login
bash deploy/provision.sh
```

`provision.sh` creates the resource group, the Container Apps environment, and
the app from `deploy/containerapp.yaml`, then pins 100% of traffic to the first
revision. The template configures:

- **Liveness**: `GET /healthz` (process is up), 10 s period.
- **Readiness**: `GET /readyz` (database reachable, migrations applied), 5 s
  period — replicas take no traffic until it returns 200.
- **Startup**: `GET /healthz`, 30 × 3 s (image start + migration check).
- `terminationGracePeriodSeconds: 30` so in-flight requests drain (server
  drains for up to 25 s on SIGTERM).
- Multiple-revision mode for staged rollouts, 1–3 replicas with HTTP
  concurrency scale rule.

## 4. Release checklist

Before merging to `main`:

- [ ] CI green on the branch (`ci.yml`).
- [ ] New migrations are additive and numbered sequentially (`000011+`).
- [ ] If env vars changed: update `README.md` config table, `.env.example`,
      `deploy/containerapp.yaml`, and repo secrets in the same PR.
- [ ] `docker build . && docker run` smoke-tested locally if Dockerfile,
      dependencies, or boot path changed.

After `deploy.yml` finishes:

- [ ] `curl -fsS https://<fqdn>/healthz` → `{"status":"ok"}`.
- [ ] `curl -fsS https://<fqdn>/readyz` → 200 (`{"status":"ready"}`).
- [ ] `az containerapp revision list -n $APP_NAME -g $RESOURCE_GROUP -o table`
      shows the new revision `Active` / 100%.
- [ ] Log in once through the SPA (SPA served from the same origin).
- [ ] Spot-check one mutation (create item) and one admin action.
- [ ] Old revision remains `Active` (rollback target) until the next release.

## 5. Rollback

```bash
# list revisions; the previous healthy one has the previous r<suffix>
az containerapp revision list -n "$APP_NAME" -g "$RESOURCE_GROUP" \
  --query '[].{name:name, traffic:properties.trafficWeight, active:properties.active}' -o table

# shift all traffic back
az containerapp ingress traffic set -n "$APP_NAME" -g "$RESOURCE_GROUP" \
  --revision-weight "<app>--<previous-suffix>=100"
```

Because migrations are additive, the previous revision keeps working against
the migrated schema. If the bad revision is also broken for direct FQDN
traffic (e.g. a probe loop), deactivate it:
`az containerapp revision deactivate -n <app>--<suffix> -g $RESOURCE_GROUP`.

## 6. Health endpoints, errors and logs

| Signal | Meaning | Action |
|---|---|---|
| `/healthz` 200 | process alive | — |
| `/readyz` 200 | DB reachable, migrations applied | — |
| `/readyz` 503 | DB down, credentials wrong, or migrations pending | check DATABASE_URL secret, Postgres status; readiness gates traffic automatically |
| `503 {"error":"database busy, try again"}` | lock/statement timeout (5 s / 15 s) | transient; clients retry (frontend shows a friendly message) |
| `429` with `Retry-After` | rate limit (login/signup) | expected under abuse; tune limits in `internal/ratelimit` if too strict |
| `422/409` on retried mutation | idempotency key reused with a different/live request | client bug; see README "Retrying requests safely" |

Logs (structured JSON on stdout via `logRequests`):

```bash
az containerapp logs show -n "$APP_NAME" -g "$RESOURCE_GROUP" --follow
```

Every request log line carries `request_id`; responses echo it in the
`X-Request-ID` header, so a report can be traced to a log entry directly.

## 7. Local production-shaped run

```bash
cp .env.example .env   # fill in DATABASE_URL and JWT_SECRET
APP_PORT=18080 docker compose up --build
curl -fsS http://localhost:18080/readyz
```

`docker compose` runs the same image (non-root uid 10001, SPA served by Go,
migrations embedded) against the compose Postgres 17 service.
