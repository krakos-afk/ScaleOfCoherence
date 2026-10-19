# Deployment runbook

ScaleOfCoherence runs as a single Go container (API + embedded SPA + embedded
migrations) on **Azure Container Apps**, backed by managed **PostgreSQL**.
Frontend assets and migrations ship inside the image, so there is nothing else
to deploy. Deployment is done manually; CI (`.github/workflows/ci.yml`) only
runs the checks and never pushes or deploys anything.

## 1. Image

Build and push per commit, tagged with the commit SHA:

```bash
IMAGE=ghcr.io/<owner>/<repo>:$(git rev-parse HEAD)
docker build -t "$IMAGE" .
docker push "$IMAGE"
```

Any registry works; private registries need credentials configured on the
Container App. Never deploy `latest`: SHA tags make rollback "redeploy the
previous tag".

## 2. Container App configuration

The app is built for these settings:

- Ingress: external, target port 8080, TLS terminated by the platform.
- Secrets: `JWT_SECRET` (≥ 32 bytes) and the production `DATABASE_URL`
  (`sslmode=require`) as Container App secrets, referenced as env vars; never
  baked into the image.
- `PORT=8080`, `MIGRATE_ON_BOOT=false` (migrations run separately, section 3).
- Probes: liveness `GET /healthz` (period 10 s), readiness `GET /readyz`
  (period 5 s), startup `GET /healthz` (30 × 3 s).
- `terminationGracePeriodSeconds: 30` — the server drains in-flight requests
  for up to 25 s on SIGTERM.
- 1–3 replicas at 1 vCPU / 2 GB each is the tested baseline.

`/healthz` never touches the database (so a database outage cannot cause
restart loops); `/readyz` pings it and a replica with a failing readiness
probe receives no traffic.

## 3. Migrations

Migrations are forward-only and additive: the currently running revision and
the next one must both work against the same schema, so never drop or rename
in the same release. Rollback is redeploying the previous image, never running
"down" migrations.

Apply migrations before rolling out the new revision, as a separate step:

```bash
docker run --rm \
  -e DATABASE_URL='postgres://...prod...?sslmode=require' \
  -e JWT_SECRET='<same secret as the app>' \
  ghcr.io/<owner>/<repo>:<sha> -migrate-only
```

A failed migration stops the release before any replica sees the new binary.
Keep `MIGRATE_ON_BOOT` false with multiple replicas and run the command above
once per release.

## 4. Release checklist

Before:

- [ ] CI run green on the commit (`ci.yml`, triggered manually).
- [ ] New migrations additive and numbered sequentially (`000011+`).
- [ ] Env/secret changes applied to the Container App.

Deploy:

- [ ] Build and push the SHA-tagged image; run `-migrate-only` against prod.
- [ ] Update the Container App to the new image tag.
- [ ] Wait for the new revision to report ready (portal or `az containerapp
      revision list`).

After:

- [ ] `curl -fsS https://<fqdn>/healthz` and `/readyz` both return 200.
- [ ] Log in once through the SPA and create one item.
- [ ] Note the previous image tag: that is the rollback target.

## 5. Rollback

Redeploy the previous image tag. If the bad revision is not receiving traffic
yet, it can simply be deactivated. Because migrations are additive, the
previous revision keeps working against the migrated schema.

## 6. Health endpoints, errors and logs

| Signal | Meaning | Action |
|---|---|---|
| `/healthz` 200 | process alive | — |
| `/readyz` 200 | DB reachable, migrations applied | — |
| `/readyz` 503 | DB down, credentials wrong, or migrations pending | check `DATABASE_URL` secret and Postgres status |
| `503 {"error":"database busy, try again"}` | lock/statement timeout (5 s / 15 s) | transient; clients retry (frontend shows a friendly message) |
| `429` with `Retry-After` | rate limit (login/signup) | expected under abuse; tune limits in `internal/ratelimit` if too strict |
| `422/409` on a retried mutation | idempotency key reused with a different or still-running request | client bug; see README "Retrying requests safely" |

Logs are structured JSON on stdout; every request line carries `request_id`,
echoed as `X-Request-ID` on responses, so a report can be traced to a log
entry directly.

## 7. Local production-shaped run

```bash
cp .env.example .env   # fill in DATABASE_URL and JWT_SECRET
APP_PORT=18080 docker compose up --build
curl -fsS http://localhost:18080/readyz
```

`docker compose` runs the same image (non-root uid 10001, SPA served by Go,
migrations embedded) against the compose Postgres 17 service.
