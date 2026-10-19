# Scalability

Capacity estimates for ScaleOfCoherence: what the current single-backend deployment handles, where it breaks first, and what changes for roughly 2 million registered users.

**Basis.** These are back-of-envelope estimates from typical PostgreSQL and Go behaviour and from the schema and endpoints in [DEPENDENCY_MAP.md](DEPENDENCY_MAP.md). They are **not measurements**. Treat every figure as roughly ±2×, and confirm with the load tests in section 8 before relying on any of them.

---

## 1. Deployment assumed

| Component | Current plan (README) | Size assumed for estimates |
|---|---|---|
| Backend | One Go container (Azure Container Apps), serving API and SPA | 1–2 vCPU, 1–2 GB RAM, 1 replica |
| Database | One PostgreSQL 17 instance | 4 vCPU, 16 GB RAM, SSD storage |
| Frontend | Static files, no router or state library | Negligible load |
| Auth | Stateless JWT (HS256), bcrypt on login | No session store |
| Async work | None yet (no outbox or notification tables in migrations 1–9) | — |

---

## 2. Workload model

| Parameter | Target A: challenge brief | Target B: ~2M registered users |
|---|---|---|
| Registered users | 5,000 | 2,000,000 |
| Peak simultaneous users | 2,000 | 20,000 (1% of registered) |
| Requests per active user | 0.1 req/s (one every ~10 s) | same |
| Average request rate | 200 req/s | 2,000 req/s |
| Peak request rate (3× average) | 600 req/s | 6,000 req/s |
| Write share of requests | ~10% | ~10% |
| Peak write transactions | ~60/s | ~600/s |
| Peak DB queries (~2 per request: auth check + data) | ~1,200/s | ~12,000/s |
| Busy DB connections at peak (req/s × ~6 ms DB time) | ~4 | ~36 |

---

## 3. Data volume

Assumptions: ~15 versions per item (create, claim, complete, approve, edits, moves), ~4 activity rows per item, ~2 comments per item. Row sizes include indexes.

| Table | Row size | Target A rows | Target A size | Target B rows | Target B size |
|---|---|---|---|---|---|
| items (total, incl. closed) | ~1.5 KB | 100k (50k active) | ~150 MB | 40M (20M active) | ~60 GB |
| items trigram indexes | — | — | ~50 MB | — | ~40 GB |
| item_changes | ~0.6 KB | 1.5M | ~0.9 GB | 600M | ~360 GB |
| item_stage_activity | ~0.35 KB | 400k | ~140 MB | 160M | ~56 GB |
| item_comments | ~0.6 KB | 200k | ~120 MB | 80M | ~48 GB |
| users, orgs, teams, members, workflows | small | < 100k | < 50 MB | < 10M | < 5 GB |
| **Total** | | | **~1.5 GB** | | **~0.6 TB** |

- Target A fits entirely in RAM (16 GB), so reads are served from cache.
- `item_changes` is the fastest-growing table, at about 1 GB per 1.5M changes. It is append-only.
- Target B no longer fits in memory on one node. Hot data (open items and recent history) must fit, and cold data must be moved or partitioned.

---

## 4. Cost per operation (estimated, warm cache, single Postgres on SSD)

| Operation | DB work | Est. latency | Notes |
|---|---|---|---|
| JWT verify (every request) | none | < 0.1 ms | Stateless, no DB call |
| Login / create user | 1 query + bcrypt | 50–100 ms CPU | Bcrypt cost 10 assumed. ~12–20 logins/s per vCPU. CPU-bound, not DB-bound |
| Authorization lookup (role, team) | 1 indexed lookup | 0.2–0.5 ms | Runs on most requests |
| `GET /items` page (25 rows, filters) | 1 query, org-scoped index | 2–8 ms | Plus a count query: +3–20 ms at 100k items |
| `GET /items?q=` (trigram search) | GIN bitmap scan | 10–50 ms | At ~100k items |
| `GET /items/{id}` | 1 PK lookup | ~1 ms | |
| `GET /items/{id}/timeline` (50 entries) | index on `(item_id, version)` | 2–5 ms | Plus a join to activity |
| `GET /items/attention`, `/items/my` | partial indexes (migration 9) | 2–10 ms | Scales with the caller's open work, not total items |
| Claim / complete / approve / reassign | 1 tx, 4–6 statements, 2 triggers | 5–15 ms | Dominated by commit fsync (~2–5 ms on cloud SSD) |
| Item update (version check) | 1 tx, 2–3 statements | 4–10 ms | |
| Create item (with stage) | 1 tx, 3 inserts | 5–12 ms | |
| Stale-version request (409) | 1 UPDATE matching 0 rows | 1–2 ms | No write happens, so retry storms are cheap |
| 100 users claim the same item at once | row lock on one `items` row | < 0.5 s total | 1 succeeds, 99 get 409 |
| Hot-item ceiling | single-row serial commits | ~100–300 updates/s per item | Far above human collaboration rates |

---

## 5. Capacity of the current deployment

The database, not the Go server, is the first limit. One Go vCPU handles roughly 1,000–2,000 req/s (JSON encoding of list pages dominates), which is above what the database can serve.

| Database size | Sustained mixed requests/s | Simultaneous users supported at peak (capacity ÷ 0.3 req/s each) | First bottleneck |
|---|---|---|---|
| 2 vCPU / 8 GB | 300–450 | ~1,000–1,500 | DB CPU |
| **4 vCPU / 16 GB (assumed)** | **700–1,000** | **~2,300–3,300** | DB CPU on list, search and count queries |
| 8 vCPU / 32 GB | 1,500–2,000 | ~5,000–6,500 | DB CPU, then write commits |

| Resource | Limit of one backend replica | Notes |
|---|---|---|
| Request throughput | ~1,000–2,000 req/s per vCPU | DB-bound before this |
| Memory | ~100 MB for 3,000 keep-alive connections | Goroutine per connection |
| DB connections needed | ~4 busy at Target A peak | Pool limit is 20 by default (`DB_MAX_OPEN_CONNS`), with lifetime and idle recycling configured |
| Login bursts | ~12–20 logins/s per vCPU | 2,000 users logging in within 2 minutes (~17/s) saturates one vCPU and slows other requests |
| Availability | Single point of failure | A deploy or crash causes downtime. No DB high availability unless enabled on the managed server |

**Result.** The current plan comfortably meets Target A (hundreds to a few thousand simultaneous users, tens of thousands of active items, ~1.5 GB of history). Expect it to run at ~60–85% DB CPU at the 2,000-user peak, so a 4 vCPU database is the minimum, and it is the first thing to scale. Registered-user count (thousands to ~100k) does not matter on its own. Concurrency and query mix decide the limit. Realistic ceiling before architectural changes: **~3,000 simultaneous users and ~50k–100k registered users**.

---

## 6. Bulk operations (estimated)

There are no bulk endpoints today, so bulk work goes through per-item transactions or direct SQL.

| Operation | Target A size | Estimate | At Target B size |
|---|---|---|---|
| Import 50k items via API (1 tx each, serial) | 50k | 200–400 s | impractical, use COPY |
| Same, 8 parallel workers | 50k | 30–60 s | — |
| Same with `COPY` into `items` + `item_changes` in one tx | 50k | 5–15 s | ~1–3 min per 1M rows |
| Reassign or close 10k items via API (serial) | 10k | ~80 s | needs async job |
| Same, 8 workers | 10k | 10–15 s | — |
| Same, set-based SQL (must also write `item_changes`) | 10k | 2–5 s | seconds to minutes per org |
| `CREATE INDEX` on `item_changes` | 1.5M rows | 5–15 s (2× with `CONCURRENTLY`) | 1–3 h on 600M rows |
| Trigram GIN index build on items | 100k | 5–20 s | 1–3 h on 40M rows |
| Add FK or CHECK (full validation scan) | 1.5M rows | 1–3 s | 10–30+ min. Use `NOT VALID` then `VALIDATE` |
| Add nullable column | any | instant (PG17 metadata-only) | instant |
| `pg_dump` / restore | 1.5 GB | 1–3 min / 5–10 min | Use managed snapshots and point-in-time restore |

Bulk writes must always insert the matching `item_changes` rows in the same transaction. The deferred trigger `check_item_version_logged` rejects the commit otherwise.

---

## 7. Where it breaks and what changes

### 7.1 Current weak points (cheap fixes first)

Items marked *verify* are inferred from DEPENDENCY_MAP.md and need confirming in code.

| Issue | Impact | Fix |
|---|---|---|
| Single backend replica | Downtime on deploy or crash | Run ≥ 2 replicas. JWT is stateless, so this is safe once the secret is shared |
| Page/limit pagination | Deep pages scan and discard rows | Keyset pagination on `(sort_key, id)` |
| Count query per list | 3–20 ms now, much more at millions of rows | Cap counts ("1000+") or estimate |
| Dashboard fans out: `/workflows` then `/workflows/{id}/stages` per workflow | N+1 requests on every dashboard load | One aggregated dashboard endpoint |
| Role and team lookup on every request | ~half of all DB queries; org membership is deliberately re-read per request so demotions apply immediately | Short-TTL in-process cache (30–60 s), invalidated on role changes |
| Bcrypt on the request path | Login bursts starve other requests | Already rate-limited per IP/email and concurrency-capped to the CPU count; still size CPU for login peaks |
| No async processing | No notifications, no secondary work | Outbox table + worker (`FOR UPDATE SKIP LOCKED`), idempotent consumers |
| Trigram `ILIKE '%term%'` search | Fine at 10⁵ items per org, degrades at 10⁷ | See Phase 2 |

### 7.2 Changes by growth stage

| Area | Phase 1: up to ~10k simultaneous, ~200k registered | Phase 2: up to ~2M registered, 20–40k simultaneous |
|---|---|---|
| Backend | 2–4 replicas, autoscale on concurrent HTTP requests | ~10–12 vCPU in total (6,000 req/s at ~50% utilization), spread over replicas |
| DB primary | 8 vCPU / 32 GB, high availability on | 16–32 vCPU / 128+ GB, high availability on |
| Read scaling | One read replica for lists, search, timeline, dashboard | 2–3 replicas. Lag is tolerable because every write carries the expected version, so a stale read fails with 409 instead of corrupting data |
| Connections | Pool limits per replica | PgBouncer in transaction mode (check prepared-statement handling with pgx) |
| Caching | In-process role/team cache | Redis for role cache, rate limits and token revocation |
| Pagination, counts | Keyset pagination, capped counts | Same |
| Search | Keep trigram, add per-org result limits | Dedicated search engine (OpenSearch or similar), fed from the outbox |
| History growth | Keep | Partition `item_changes`, activity and comments (see note), archive closed items older than N months to cold storage |
| Async | Outbox + worker for notifications | Same, workers scaled independently, message broker if outbox polling becomes a load |
| Items table bloat | Lower `fillfactor` (≈ 85–90), tune autovacuum | Same, per-table autovacuum settings |
| Write ceiling | ~600 write tx/s peak is comfortable on one primary | A single primary tops out around 2–5k write tx/s. Beyond that, shard by `org_id` |
| Observability | `pg_stat_statements`, p95/p99 latency, DB CPU, pool wait, 409 rate | Same, plus replica lag and autovacuum lag |

**Partitioning note.** Partition keys must appear in every unique constraint. For `item_changes`, hash-partitioning on `item_id` keeps `UNIQUE (item_id, version)` and `UNIQUE (org_id, item_id, version)` valid, but the composite foreign keys from activity and comments to `item_changes` need to be reviewed.

**Sharding note.** The schema already carries `org_id` in every composite FK, so `org_id` is a natural shard key and tenant data never crosses orgs. Tables with a primary key or unique constraint on `id` alone would need `org_id` added before sharding (for example with Citus).

### 7.3 What stays unchanged

| Design choice | Why it holds at 2M users |
|---|---|
| Optimistic locking on `items.version`, one `item_changes` row per version | Conflicts only occur on the same item. A 409 costs ~1–2 ms and writes nothing |
| Partial unique index (one open activity per item) and version triggers | Per-item constraints, no cross-item or global locks |
| Composite `org_id` FKs | Tenant isolation, and the basis for sharding |
| Stateless JWT | Replicas scale horizontally with no session affinity |
| Partial indexes for "my work" and pending approvals | Query cost depends on one user's open work, not total data size |

---

## 8. Validation plan

Run these before trusting any number above, against a database loaded to Target A volume (100k items, 1.5M changes).

| Test | Scenario | Pass criteria |
|---|---|---|
| Mixed browse | 90% reads (list, filter, search, detail, timeline), 10% writes, ramp to 600 req/s | p95 reads < 200 ms, p95 writes < 300 ms, errors < 0.1% (excluding intended 409s), DB CPU < 70% |
| Contended claim | 100 parallel claims on one item, repeated on many items | Exactly 1 success per item, 99 × 409, one open activity row, versions contiguous |
| Login burst | 2,000 logins in 2 minutes while the browse test runs | Browse p95 stays under target |
| Bulk import | 50k items via API, 8 workers | Finishes < 90 s, no connection errors |
| Soak | 1 hour at average load | No growth in memory, connections or replication lag |

Measure with `pg_stat_statements` (slowest and most frequent queries), pool wait time, and per-endpoint p50/p95/p99. Update the tables in sections 4–6 with the measured values.