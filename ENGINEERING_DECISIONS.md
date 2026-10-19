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
