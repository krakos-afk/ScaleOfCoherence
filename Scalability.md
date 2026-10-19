# Scalability

Capacity view for ScaleOfCoherence: what the current single-backend deployment
handles, which resource binds first as load grows, and what changes by growth
stage.

**Basis.** These are order-of-magnitude planning anchors derived from the
schema and endpoints in [DEPENDENCY_MAP.md](DEPENDENCY_MAP.md) and typical
PostgreSQL/Go behaviour. They are **not measurements**. Confirm with the
validation tests in §6 before committing to any capacity number.

---

## 1. Deployment assumed

| Component | Current plan |
|---|---|
| Backend | One Go container (Azure Container Apps): API + SPA, 1–2 vCPU, 1–2 GB |
| Database | One PostgreSQL 17 instance, SSD |
| Frontend | Static files served by the same container |
| Auth | Stateless JWT (HS256); bcrypt on login |
| Async work | None (no outbox or notification tables) |

Two targets the design is judged against: **Target A** — the challenge brief,
~5k registered users with ~2k concurrent. **Target B** — ~2M registered users
with ~20k concurrent.

## 2. Which resource binds first

The database, not the Go server.

| Resource | Behaviour |
|---|---|
| Server CPU | Handles more req/s than a single Postgres in this class can serve |
| Database | List, search and count queries dominate DB CPU |
| Writes | One primary is comfortable for this write share; per-item row locks mean contention only between users acting on the *same* item, and a losing optimistic-lock attempt (409) writes nothing |
| Login | bcrypt is the only significant per-request CPU cost; already rate-limited and concurrency-capped, but size CPU for login peaks |
| Connections | Tens busy at Target A peak; pool capped at 20 per replica |
| Storage | History is append-only; the history tables are the first data to outgrow one node |

**Result.** The current plan comfortably meets Target A. Registered-user count
barely matters on its own; concurrency and query mix set the limit. Realistic
ceiling before architectural change: roughly **3k simultaneous users**.

## 3. Current weak points (cheap fixes first)

| Issue | Impact | Fix |
|---|---|---|
| Single backend replica | Deploy or crash is downtime | Run ≥ 2 replicas (JWT is stateless) |
| Page/limit pagination | Deep pages scan and discard rows | Keyset pagination on `(sort_key, id)` |
| Count per list request | Cost grows with table size | Cap counts ("1000+") or estimate |
| Dashboard fan-out | N+1 requests per dashboard load | One aggregated dashboard endpoint |
| Role/team lookup per request | About half of all DB queries (the re-read is deliberate, see ENGINEERING_DECISIONS.md) | Short-TTL in-process cache, invalidated on role changes |
| Trigram `ILIKE` search | Fine at 10⁵ items per org, degrades beyond | Per-org result limits, then a dedicated search engine |
| No async processing | No notifications or secondary work | Outbox table + worker (`FOR UPDATE SKIP LOCKED`) |
| History growth | `item_changes` is append-only and unbounded | Partition, and archive closed items older than N months |

## 4. Changes by growth stage

| Area | Phase 1: ~10k simultaneous, ~200k registered | Phase 2: ~2M registered, 20–40k simultaneous |
|---|---|---|
| Backend | 2–4 replicas, autoscale on concurrent requests | ~10–12 vCPU total across replicas |
| Database | 8 vCPU / 32 GB, high availability on | 16–32 vCPU / 128+ GB, HA on |
| Connections | Pool limits per replica | PgBouncer in transaction mode (verify pgx prepared-statement handling) |
| Read scaling | One read replica for lists, search, timeline, dashboard | 2–3 replicas. Lag is tolerable because every write carries the expected version, so a stale read fails with 409 instead of corrupting data |
| Caching | In-process role/team cache | Redis for role cache, rate limits and token revocation |
| Search | Keep trigram with per-org limits | Dedicated search engine, fed from the outbox |
| History | Partition `item_changes`, activity and comments; archive closed items | Same |
| Async | Outbox + worker | Workers scaled independently |
| Writes | One primary is comfortable | Beyond one primary's write ceiling, shard by `org_id` |
| Observability | `pg_stat_statements`, p95/p99, DB CPU, pool wait, 409 rate | Same, plus replica and autovacuum lag |

**Partitioning note.** Partition keys must appear in every unique constraint.
Hash-partitioning `item_changes` on `item_id` keeps `UNIQUE (item_id, version)`
and `UNIQUE (org_id, item_id, version)` valid, but the composite foreign keys
from activity and comments to `item_changes` need to be reviewed.

**Sharding note.** `org_id` is already in every composite FK, so it is a
natural shard key and tenant data never crosses orgs. Tables with a unique
constraint on `id` alone would need `org_id` added (for example for Citus).

## 5. What stays unchanged

| Design choice | Why it holds at 2M users |
|---|---|
| Optimistic locking on `items.version`, one `item_changes` row per version | Conflicts only occur on the same item; a 409 is cheap and writes nothing |
| Partial unique index (one open activity per item) and version triggers | Per-item constraints, no global locks |
| Composite `org_id` foreign keys | Tenant isolation and the basis for sharding |
| Stateless JWT | Replicas scale with no session affinity |
| Partial indexes for "my work" and pending approvals | Query cost depends on one user's open work, not total data size |

## 6. Validation plan

Run these before trusting any number above, against a database loaded to
Target A volume (100k items).

| Test | Scenario | Pass criteria |
|---|---|---|
| Mixed browse | 90% reads (list, filter, search, detail, timeline), 10% writes, ramp to peak | p95 reads < 200 ms, p95 writes < 300 ms, errors < 0.1% excluding intended 409s, DB CPU < 70% |
| Contended claim | 100 parallel claims on one item, many items | Exactly 1 success per item, one open activity row, contiguous versions |
| Login burst | Many logins while the browse test runs | Browse p95 stays in target |
| Bulk import | 50k items via API, 8 workers | Finishes without connection errors |
| Soak | 1 hour at average load | No growth in memory, connections or replication lag |

Measure with `pg_stat_statements` (slowest and most frequent queries), pool
wait time, and per-endpoint p50/p95/p99. Replace the planning anchors above
with measured values as they become available.
