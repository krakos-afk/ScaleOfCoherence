# Deployment

ScaleOfCoherence is deployed on Azure as a single Go container. The React SPA
and the migrations are embedded in the same image, so there is no separate
frontend or migration artifact to ship.

## Shape

- **Azure Container Registry** stores the image, tagged with the commit SHA.
  Tags, not `latest`, are what make a rollback a re-point rather than a rebuild.
- **Azure Container Apps** runs it: external ingress on port 8080, TLS
  terminated by the platform, 1–3 replicas at 1 vCPU / 2 GB. Each deployment
  is a new revision; rollback means sending traffic back to a previous SHA.
- **Azure Database for PostgreSQL Flexible Server** is the database, reached
  through a `DATABASE_URL` secret with `sslmode=require`. Pool limits and the
  lock/statement/idle-in-transaction timeouts are compiled in; the pool is per
  replica, so N replicas can open up to N × 20 connections.

CI (`.github/workflows/ci.yml`) runs checks only. Building/pushing the image and
updating the revision are manual steps.

## Configuration

Configuration is environment-only (`internal/config`); `.env.example` lists
every variable. The Container App supplies `DATABASE_URL` and `JWT_SECRET`
(≥ 32 bytes) as secrets, with `MIGRATE_ON_BOOT=false` so replicas can roll
side by side; the embedded SPA path is already set by the image.

## Migrations

Migrations are forward-only and additive: the running revision and the next
one must both work against the same schema. They are applied by running the
same image once with `-migrate-only` before the new revision takes traffic.
Because migrations never drop or rename in place, a failed release rolls back
by restoring the previous image, not the schema.

## Health and logs

`/healthz` is liveness only and never touches the database, so a database
outage cannot cause restart loops. `/readyz` pings the database and takes a
replica out of traffic when it fails. SIGTERM starts a 25 s drain, under the
30 s stop grace period. Logs are structured JSON with a `request_id` per
request, echoed as `X-Request-ID`.

## Local parity

`docker compose up --build` runs the same image (non-root, SPA served by Go,
embedded migrations) against PostgreSQL 17; set `APP_PORT` to avoid clashing
with a local server on 8080.
