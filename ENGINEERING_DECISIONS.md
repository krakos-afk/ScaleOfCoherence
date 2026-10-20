# Engineering Decisions

The core design choices, in brief:

1. **Versioned items with one history track.** Every mutation bumps
   `items.version` with a conditional update in the same transaction that
   writes its `item_changes` row, so writes are atomic and history has no gaps.
   Stage activity is merged into that history; each activity-bearing change
   stores a snapshot of the activity state (stage, team, assignee, completion
   and approval notes) in its `item_changes.changes` payload, so timeline
   entries remain accurate after later rework. Comments are anchored to the same
   `(org_id, item_id, version)` track but returned as their own feed and never
   bump the version. Stale writes are rejected with `409` (see Optimistic
   concurrency).
2. **Multi-tenant isolation with layered authorization.** Every row carries
   `org_id`; org usertypes (`admin`, `user`) combine with per-team roles
   (`viewer`, `editor`, `commentator`, `leader`). Each endpoint enforces the
   caller's membership and relevant role, including claim/complete/approve/
   reassign/advance and close/reopen.
3. **Team-owned workflow stages.** The model is org → teams and workflows →
   stages → items: stages are ordered and each is assigned to a team, and items
   sit on a stage. Any editor or leader on that team may claim the open
   activity, keeping assignment flexible; a stage can optionally require an
   approver. Tighter stage-to-role restrictions are a later refinement.
4. **No async user-facing features.** There is no queue or worker tier because
   no feature needs one (bulk imports are not implemented); the only background
   work is idempotency-key pruning, described below.
5. **Text-only custom fields in the UI.** The frontend models them as
   `Record<string, string>`. The API and database accept any JSON object, so
   TEXT is a product convention, not a server-side constraint.
6. **Attention-first dashboard.** `/items/attention` ranks the work assigned to
   the caller plus approvals waiting on them, so the dashboard answers "what
   needs me now".

---

# Known TradeOffs and Limitations

The trade-offs behind them, and why they are acceptable now:

1. **Startup validation over partial configuration.** All configuration is
   loaded and validated once at startup (`internal/config`); `JWT_SECRET` is
   required (≥ 32 bytes) with no default, so a misconfigured server refuses to
   boot. `-migrate-only` applies migrations as a separate job, so replicas can
   start with `MIGRATE_ON_BOOT=false` for side-by-side rollouts.
2. **Optimistic concurrency, with stale views.** Only the request holding the
   current item version wins; later ones get `409` with the current version and
   must refetch. Writes stay lock-free, but a user on a stale view can lose an
   edit attempt. There is no SSE or push channel yet, so a conflict surfaces
   only on submit; the frontend discards local state and refetches.
3. **Membership re-read over trusting the token.** Every authenticated request
   re-reads the caller's org membership from the database instead of trusting
   the `typ` claim, so deleted users lose access and demotions apply
   immediately. The cost is one indexed lookup per request; claims are UI
   gating only, and a short-TTL cache is the next step if the lookup shows in
   profiles (see Scalability.md).
4. **Per-replica rate limits over shared state.** Login and signup use
   in-memory token buckets per client IP and email (`internal/ratelimit`,
   `golang.org/x/time/rate`; the standard library has no token bucket with
   retry-after math). Limits are per replica, so N replicas allow N times the
   configured rate; acceptable at the current replica count, with a shared
   Redis-backed limiter as the upgrade path. Client IPs come from the first
   `X-Forwarded-For` hop, so ingress must overwrite, not append, the header.
5. **Drain and split probes.** On SIGTERM/SIGINT the server stops accepting
   connections, drains in-flight requests for up to 25 s, then closes the pool;
   the container has a 30 s stop grace period. `/healthz` is liveness only and
   never touches the database, so an outage cannot cause restart loops;
   `/readyz` pings it and returns `503` while it is unreachable.
6. **Database guardrails over generic errors.** Every pooled connection carries
   `lock_timeout=5s`, `statement_timeout=15s` and
   `idle_in_transaction_session_timeout=30s`. Lock and statement timeouts map
   to `503` (`database busy, try again`) rather than `500`s, because they mean
   "retry", not "bug".
7. **Database-coordinated idempotency.** Item mutations honor an
   `Idempotency-Key`, stored per `(org_id, user_id, key)` in
   `idempotency_keys` (migration `000010`); the database coordinates because
   retries can land on any replica. The first request stores a hash of method +
   URI + body and then the response; a replay (same key and hash) returns the
   stored response with `Idempotency-Replayed: true` without re-executing; a
   different hash returns `422`; a concurrent duplicate returns `409`. `5xx`
   responses release the key so transient failures are retryable, while `4xx`
   responses are stored because they are deterministic. Keys are isolated per
   org and user, and pruned after 24h at boot and hourly.
8. **Idempotency is API-only for now.** The frontend does not send keys: it
   disables buttons while a request is in flight and discards state on `409`,
   so duplicate writes cannot originate from the UI. The keys exist for API
   clients and future retry logic.

---

# Tests

## How to run

| Command | Scope |
|---|---|
| `go test ./... -count=1` | all Go tests; integration tests create scratch databases (`scaleco_test`, `scaleco_api_test`) and skip (exit 0) when Postgres is unreachable; `TEST_DATABASE_URL` overrides the DSN |
| `go test -coverpkg=./... -coverprofile=cover.out ./...` | coverage; see `go tool cover -func=cover.out`. Total: **64.8% of statements** (2026-10-19) |
| `npx tsc -b`, `npx eslint src`, `npm run build` in `frontend/` | frontend types, lint, bundle (no frontend test runner exists) |
| `docker build .`, `docker compose up` | image/boot smoke, manual (DEPLOYMENT.md) |

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
| `repository/items_integration_test.go` | `TestConcurrentClaimSameVersion`, `TestReassignSingleVersion`, `TestStaleActivityVersionRejected`, `TestItemVersionMustBeLogged`, `TestCompleteApproveContiguousVersions`, `TestTimelineActivitySnapshots`, `TestTimelineAssignmentSnapshotAfterRejection`, `TestTimelineOrderingAndPagination`, `TestCrossOrgIsolation`, `TestStaleVersionAllMutations` | version-track invariants and triggers, optimistic locking under concurrency, stale-version 409s, immutable timeline activity snapshots, timeline ordering, org isolation |
| `repository/comments_integration_test.go` | `TestCommentCreatePermissionsAndVersion`, `TestCommentVersionAnchoring`, `TestCommentDeletePermissions` | comment permissions, anchoring to the version they were written against |
| `repository/lists_integration_test.go` | `TestTeamListSearchAndSort`, `TestWorkflowListSearchAndSort`, `TestTeamMembersSearchAndSort`, `TestOrgUserListSearchAndSort` | search filters, sort whitelists, pagination on list endpoints |
| `repository/attention_integration_test.go` | `TestAttentionAssignedAndApprovals` | "my open work" and pending-approval queries |
| `repository/auth_integration_test.go` | `TestGetMembershipReReadsFromDatabase`, `TestGetByEmailIsCaseInsensitive` | membership re-read (deleted/demoted users), case-insensitive login |
| `repository/idempotency_integration_test.go` | `TestIdempotencyClaimLifecycle`, `TestIdempotencyClaimConcurrency` (8 workers, exactly one fresh claim), `TestIdempotencyKeysAreScopedPerOrgAndUser`, `TestIdempotencyDeleteExpired` | DB claim lifecycle, concurrency, tenant/user scoping, retention cleanup query |
| `database/postgres_integration_test.go` | `TestConnectAppliesPoolLimitsAndTimeouts` | pool limits and per-connection timeouts |
| `api/integration_test.go` (full router + DB) | `TestOrgIsolationAcrossEndpoints` (21 probes incl. search leakage), `TestAuthorizationMatrix` (admin/member/viewer), `TestIdempotencyEndToEnd` (replay header, single row, 422) | HTTP-level tenant isolation, role matrix enforcement, idempotency across the real stack |
| `api/demo_integration_test.go` (full router + DB) | `TestDemoStartupSeedIsIdempotent` | startup demo seeding does not create duplicate items on restart |
| `api/journeys_integration_test.go` (full router + DB) | `TestJourneyOrgSetup`, `TestJourneyItemLifecycle`, `TestJourneyApprovalLoop`, `TestJourneyReassign`, `TestJourneyOptimisticConcurrency`, `TestJourneyCloseReopen`, `TestJourneyComments` | end-to-end user workflows: org setup and directory reads, create → claim → complete → advance with dashboard reads and timeline, approval pending/reject/re-approve, reassignment, 409 resync cycle, close/reopen, comment deletion rules |

## What is covered

The inventories above cover the decision points in this document: config
validation, authentication, layered authorization and tenant isolation, the
version/history invariants, the end-to-end journeys, rate limiting, HTTP
hardening and error mapping, list/attention queries, and idempotency.

## What is not covered

- **Frontend has no automated tests at all.** Only typecheck, lint and build
  run. UI behaviors (409 discard-and-refetch, busy/disabled states, friendly
  429/503 messages, auth form) are verified manually.
- **HTTP success paths for `DeleteUser` and `RemoveMember`** are not tested;
  the other endpoints' success paths are exercised by the journey tests.
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
  manually, not automated.
- **Concurrent duplicate idempotent requests at HTTP level**: concurrency is
  tested at the repository layer, not through the router.
- **Rate limiter edge cases**: `X-Forwarded-For` parsing and multi-replica
  behavior (allowance multiplies per replica) are not tested.
- **Observability**: request log fields/format and request-ID correlation in
  logs are not asserted (headers are).
- **Browser end-to-end and accessibility**: none.
