# Engineering Decisions

# Known TradeOffs and Limitations
The system currently has several implementation lacking 

## Configuration and secrets

All configuration is loaded and validated once at startup (`internal/config`).
`JWT_SECRET` is required (>= 32 bytes) and never has a default; the server
refuses to boot without it. `-migrate-only` lets deployments run migrations as
a separate job so replicas can start with `MIGRATE_ON_BOOT=false`, which is
required for side-by-side rollouts.

## Authorization re-read

Every authenticated request re-reads the caller's org membership from the
database rather than trusting the `typ` claim, so deleted users lose access and
demotions take effect immediately. The cost is one indexed lookup per request;
token claims are UI gating only. A short-TTL in-process cache is the next step
if that lookup shows up in profiles (see Scalability.md).

## Rate limiting

Login and signup are rate-limited per client IP and per email with in-memory
token buckets (`internal/ratelimit`, `golang.org/x/time/rate`; chosen because
the standard library has no token bucket with retry-after math). Limits are
**per replica**: behind N replicas the effective allowance is N times the
configured rate. Acceptable at the current replica count; a shared
Redis-backed limiter is the upgrade path. Client IPs come from the first
X-Forwarded-For hop, so ingress must overwrite, not append, the header.

## Graceful shutdown and probes

On SIGTERM/SIGINT the server stops accepting connections, drains in-flight
requests for up to 25 s, then closes the database pool; the container gets a
30 s stop grace period. `/healthz` is liveness only (never touches the
database, so a database outage cannot cause restart loops); `/readyz` pings the
database and returns 503 when it is unreachable.

## Database guardrails

Every pooled connection carries `lock_timeout=5s`,
`statement_timeout=15s` and `idle_in_transaction_session_timeout=30s`. Lock and
statement timeouts map to 503 (`database busy, try again`) instead of surfacing
as generic 500s: they mean "retry", not "bug".

## Idempotency keys

Item mutations honor an `Idempotency-Key` header, stored per (org, user, key)
in `idempotency_keys` (migration `000010`). The database is the coordination
point because retries can land on any replica; no in-process cache can make
that guarantee. Semantics:

- first request: insert the key with a hash of method + URI + body, run the
  handler, then store the response;
- replay (same key, same hash): return the stored status/body with
  `Idempotency-Replayed: true` without re-executing;
- same key, different hash: `422` (client bug, not a conflict to resolve);
- concurrent request with the same key: `409`;
- 5xx responses delete the key so retries are not poisoned by transient
  failures; 4xx responses are stored because they are deterministic.

Keys are scoped by organization and user, so keys can collide neither across
tenants nor across users. Retention is 24h, pruned at boot and hourly by a
single background loop; the table stays small (one row per retried mutation).

The frontend does not send keys yet: buttons are disabled while a request is
in flight and 409 conflicts trigger discard-and-refetch, so duplicate writes
cannot originate from the UI. The keys exist for API clients and future retry
logic.

## Deployment pipeline

`main` deploys itself in three ordered jobs (`.github/workflows/deploy.yml`):
image build/push to GHCR tagged with the commit SHA, migrations via
`docker run <image> -migrate-only`, then a new Container Apps revision that
receives traffic only after its own `/readyz` returns 200. Traffic shifting is
an explicit step rather than "latest revision takes over" for two reasons:
readiness probes alone can flap, and keeping named revisions with pinned
traffic makes rollback a one-line `traffic set` instead of a redeploy.

Migrations run as a separate process, not at app boot (`MIGRATE_ON_BOOT=false`
in production): boot-time migration couples schema change to replica start
order and retry storms, and a migration failure would crash-loop every
replica. In the pipeline a failed migration stops the rollout before any
replica sees the new binary.

The workflow authenticates to Azure with OIDC federated credentials (no
long-lived cloud secrets in GitHub) and pins image tags to the commit SHA
(never `latest`), so any revision in the portal can be traced to a commit.
Deployment concurrency is not cancelled in progress (`cancel-in-progress:
false`): aborting mid-rollout could leave traffic unpinned.

Migrations are forward-only and additive. Both the current and the previous
revision must run against the same schema, which makes rollback purely a
traffic operation.
