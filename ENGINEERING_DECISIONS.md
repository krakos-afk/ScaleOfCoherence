# Engineering Decisions



---
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

---

# Tests

## How to run

| Command | Scope |
|---|---|
| `go test ./... -count=1` | all Go tests; integration tests create scratch databases (`scaleco_test`, `scaleco_api_test`) and skip (exit 0) when Postgres is unreachable; `TEST_DATABASE_URL` overrides the DSN |
| `go test -coverpkg=./... -coverprofile=cover.out ./...` | coverage; see `go tool cover -func=cover.out`. Total: **64.8% of statements** (2026-10-19) |
| `npx tsc -b`, `npx eslint src`, `npm run build` in `frontend/` | frontend types, lint, bundle (no frontend test runner exists) |
| `docker build .`, `docker compose up` | image/boot smoke, manual (DEPLOYMENT.md §7) |

CI (`.github/workflows/ci.yml`) runs all of the above against an ephemeral
Postgres service, plus `govulncheck` and `npm audit`.

## Test inventory

### Unit tests (no database)

| Package | Tests | Covers |
|---|---|---|
| `internal/config` | `TestLoadDefaults`, `TestLoadRequiresStrongSecret`, `TestLoadOverrides`, `TestLoadRejectsInvalidValues` | env parsing, required `JWT_SECRET` ≥ 32 bytes, defaults, invalid values |
| `internal/ratelimit` | `TestAllowBurstThenDeny`, `TestKeysAreIndependent`, `TestRefillAfterDelay` | token bucket burst/refill, per-key isolation |
| `internal/auth` | `TestNormalizeEmail`, `TestValidatePassword`, `TestAuthenticateRejectsBadTokens` | email normalization, 12–72 byte policy, malformed/expired/wrong-secret JWTs |
| `internal/api` (middleware) | `TestRequestIDEchoesValidIncoming`, `TestRequestIDGeneratesWhenMissingOrInvalid`, `TestSecurityHeaders`, `TestHSTSOnlyBehindHTTPSProxy`, `TestBodyLimit`, `TestPanicRecovery`, `TestReadyAndHealthHandlers`, `TestWithSPA`, `TestWithSPADisabledWhenDirEmpty` | request IDs, security headers, HSTS behind proxy only, 413 limit, panic → 500 JSON, liveness/readiness, SPA fallback |
| `internal/api` (idempotency, fake store) | 12 tests incl. `TestIdempotencyReplaysStoredResponse`, `TestIdempotencyConflicts`, `TestIdempotencyReleasesKeyOnServerErrors`, `TestIdempotencyReleasesKeyOnPanic` | pass-through paths, key validation, claim/replay, hash mismatch 422, in-progress 409, key release on 5xx/panic, oversize handling |
| `internal/api/controllers` | `TestWriteErrorMapsDatabaseTimeoutsTo503`, `TestWriteErrorStatusMapping`, `TestLoginDistinguishesNothingBetweenUnknownEmailAndWrongPassword`, `TestDecodeRejectsOversizedBody`, `TestLoginRateLimitedPerIP`, `TestSignupRateLimitedPerIP` | error → status mapping, uniform login failure, body decode limit, per-IP rate limits |

### Integration tests (real PostgreSQL)

| File | Tests | Covers |
|---|---|---|
| `repository/items_integration_test.go` | `TestConcurrentClaimSameVersion`, `TestReassignSingleVersion`, `TestStaleActivityVersionRejected`, `TestItemVersionMustBeLogged`, `TestCompleteApproveContiguousVersions`, `TestTimelineOrderingAndPagination`, `TestCrossOrgIsolation`, `TestStaleVersionAllMutations` | version-track invariants and triggers, optimistic locking under concurrency, stale-version 409s, timeline ordering, org isolation |
| `repository/comments_integration_test.go` | `TestCommentCreatePermissionsAndVersion`, `TestCommentVersionAnchoring`, `TestCommentDeletePermissions` | comment permissions, anchoring to the version they were written against |
| `repository/lists_integration_test.go` | `TestTeamListSearchAndSort`, `TestWorkflowListSearchAndSort`, `TestTeamMembersSearchAndSort`, `TestOrgUserListSearchAndSort` | search filters, sort whitelists, pagination on list endpoints |
| `repository/attention_integration_test.go` | `TestAttentionAssignedAndApprovals` | "my open work" and pending-approval queries |
| `repository/auth_integration_test.go` | `TestGetMembershipReReadsFromDatabase`, `TestGetByEmailIsCaseInsensitive` | membership re-read (deleted/demoted users), case-insensitive login |
| `repository/idempotency_integration_test.go` | `TestIdempotencyClaimLifecycle`, `TestIdempotencyClaimConcurrency` (8 workers, exactly one fresh claim), `TestIdempotencyKeysAreScopedPerOrgAndUser`, `TestIdempotencyDeleteExpired` | DB claim lifecycle, concurrency, tenant/user scoping, retention cleanup query |
| `database/postgres_integration_test.go` | `TestConnectAppliesPoolLimitsAndTimeouts` | pool limits and per-connection timeouts |
| `api/integration_test.go` (full router + DB) | `TestOrgIsolationAcrossEndpoints` (21 probes incl. search leakage), `TestAuthorizationMatrix` (admin/member/viewer), `TestIdempotencyEndToEnd` (replay header, single row, 422) | HTTP-level tenant isolation, role matrix enforcement, idempotency across the real stack |
| `api/journeys_integration_test.go` (full router + DB) | `TestJourneyOrgSetup`, `TestJourneyItemLifecycle`, `TestJourneyApprovalLoop`, `TestJourneyReassign`, `TestJourneyOptimisticConcurrency`, `TestJourneyCloseReopen`, `TestJourneyComments` | end-to-end user workflows: org setup and directory reads, create → claim → complete → advance with dashboard reads and timeline, approval pending/reject/re-approve, reassignment, 409 resync cycle, close/reopen, comment deletion rules |

## What is covered

Boot-time config validation; authentication (JWT parsing, password policy,
uniform login failures, email normalization); authorization (membership
re-read per request, role matrix at HTTP level, org isolation on every
list/get/search path tested); end-to-end user journeys (org setup, item
lifecycle, approval loop, reassignment, optimistic-concurrency resync,
close/reopen, comments); rate limiting; HTTP hardening (request IDs,
security headers, body limit, panic recovery, probes, SPA fallback, error
mapping incl. DB timeouts → 503); database invariants (version track,
triggers, optimistic locking, concurrent claims, comment anchoring); list
search/sort/pagination; attention queries; idempotency (claim, replay,
conflict, release, expiry, concurrency, scoping).

## What is not covered

- **Frontend has no automated tests at all.** Only typecheck, lint and build
  run. UI behaviors (409 discard-and-refetch, busy/disabled states, friendly
  429/503 messages, auth form) are verified manually.
- **Happy paths through HTTP for a few endpoints** are never executed in
  tests: `DeleteUser` and `RemoveMember`. The journey tests cover the success
  paths of the activity lifecycle (`AdvanceStage`, `CompleteActivity`,
  `ApproveActivity`, `ReassignActivity`, `GetOpenActivity`), `ListMy`,
  `Attention`, `ListUserNames`, `GetStage`, comments and item CRUD through
  the real router.
- **`cmd/server`** (`main`, `pruneIdempotencyKeys`) and the `Migrate`/`Ping`
  wrappers have 0% coverage; the cleanup query is tested, the background loop
  that calls it is not.
- **Migrations**: only fresh-database boot to the latest version is tested. No
  test upgrades a populated database from an older version, and there is no
  rollback test (migrations are forward-only by policy).
- **Load and performance**: `bench/` (Scalability.md §8) reproduces the
  Target-A measurements; there is no soak test, contention ramp, or CI
  performance gate, and none of it has been validated on production-sized
  hardware.
- **Container behavior**: CI only runs `docker build`. Startup probes, graceful
  drain on SIGTERM, and `/readyz` under a stopped database were verified
  manually during Phase 1, not automated.
- **Concurrent duplicate idempotent requests at HTTP level**: concurrency is
  tested at the repository layer, not through the router.
- **Rate limiter edge cases**: `X-Forwarded-For` parsing and multi-replica
  behavior (allowance multiplies per replica) are not tested.
- **Observability**: request log fields/format and request-ID correlation in
  logs are not asserted (headers are).
- **Browser end-to-end and accessibility**: none.