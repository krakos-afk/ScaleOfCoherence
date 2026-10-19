# Scalability

Capacity view for ScaleOfCoherence: what the current single-backend deployment
handles, where it breaks first, and what changes by growth stage.

**Basis.** The numbers below are measured, not assumed: a seeded Target-A
dataset (section 2) driven through the real server with the real PostgreSQL
schema, using `bench/` (section 8, reproducible on any machine). They describe
one host — AMD Ryzen 7 7840HS (16 threads, NVMe), PostgreSQL 17.11 with
default container settings, server and database sharing the host, loopback
HTTP, warm cache — so read them as "what this class of hardware does", and
scale reads with database CPU and writes with NVMe commit rate.

## 1. Deployment and targets

| Component | Current plan |
|---|---|
| Backend | One Go container (Azure Container Apps): API + SPA, 1–2 vCPU, 1–2 GB |
| Database | One PostgreSQL 17 instance, SSD |
| Frontend | Static files served by the same container |
| Auth | Stateless JWT (HS256); bcrypt (cost 10) on login |
| Async work | None (no outbox or notification tables) |

- **Target A** (challenge brief): ~5k registered users, ~2k concurrent,
  tens of thousands of items.
- **Target B** (~2M registered users, ~20k concurrent).

## 2. Measured footprint (Target A dataset)

Seeded: 1 org, 100 users, 100k items (half closed), 1.5M logged changes
(15 per item), 400k activity rows (4 per item), 200k comments (2 per item).

| Table | Rows | Total (heap + indexes) | Bytes/row |
|---|---|---|---|
| `items` | 100k | 252 MB | 2,646 |
| `item_changes` | 1.5M | 1,027 MB | 718 |
| `item_stage_activity` | 400k | 128 MB | 335 |
| `item_comments` | 200k | 64 MB | 333 |
| **Database total** | | **1.47 GB** | |

- `items` is 1.5 KB/row of indexes alone; the trigram indexes dominate.
- An item costs a predictable **~15 KB for its whole lifetime** (row + 15
  versions + activity + comments): 1M items ≈ 15 GB, Target B's 20–40M items
  ≈ 0.3–0.6 TB, and `item_changes` is ~70% of it.
- Target A fits in RAM on a 16 GB database. Target B does not; partition and
  archive history (section 6).

## 3. Measured latency and throughput

Sequential latency, warm cache (n = 500–1000):

| Request | p50 | p95 |
|---|---|---|
| `GET /items?limit=25` (list page + total count) | 26.9 ms | 30.6 ms |
| `GET /items?q=` (trigram over 100k items) | 51.9 ms | 54.8 ms |
| `GET /items/{id}` | 0.20 ms | 0.28 ms |
| `GET /items/{id}/timeline` (15 versions) | 0.30 ms | 0.41 ms |
| `POST /items` (create transaction) | 2.96 ms | 3.31 ms |
| `POST /items/{id}/activity/claim` (lock + version bump + change row) | 3.49 ms | 4.21 ms |

Concurrent throughput on the same host (app and database share 16 threads):

| Workload | Result |
|---|---|
| list, 16 concurrent | 175 req/s |
| search, 8 concurrent | 151 req/s |
| create, 8 concurrent | 1,753 req/s (NVMe group commit) |
| bcrypt compare, single thread | 51.9 ms, 19/s per core |
| bcrypt compare, 8 threads | 178/s |

Login throughput is bounded by the rate limits (10/min per email, 20/min per
IP), not by bcrypt; the bcrypt cost above is what a login burst costs in CPU.

## 4. What this says

- **The database is the first limit, at any load.** List/search cost tens of
  milliseconds (count and trigram work); primary-key reads cost microseconds.
  The Go server itself is not the constraint — it served all of the above
  while also hosting PostgreSQL.
- **Capacity depends on the endpoint mix, so size from per-vCPU anchors:**
  ~35 list req/s, ~19 search req/s, thousands of detail/timeline req/s, and
  ~300 serial write tx/s (1,700+/s with group commit) per database CPU.
  The brief's 600 req/s peak fits a 4 vCPU database only if list and search
  are a minority of the traffic — which is what makes the count and
  pagination fixes in section 5 the priority, not more replicas.
- **Writes are not a bottleneck at this scale:** create and claim commit in
  ~3 ms, and contention only exists between users acting on the same item.
- **Login bursts are a CPU cost, not a throughput wall:** 2,000 logins in 2
  minutes (~17/s) consumes about one core.
- **Storage is linear:** ~15 KB per item lifetime (section 2).

## 5. Current weak points (cheap fixes first)

| Issue | Impact | Fix |
|---|---|---|
| Single backend replica | Deploy or crash is downtime | Run ≥ 2 replicas (JWT is stateless) |
| Count per list request | Measured 27 ms p50 for a 25-row page; grows with table size | Cap counts ("1000+") or estimate |
| Page/limit pagination | Deep pages scan and discard rows | Keyset pagination on `(sort_key, id)` |
| Trigram `ILIKE` search | Measured 52 ms p50 at 100k items in one org | Per-org result limits, then a dedicated search engine |
| Dashboard fan-out | N+1 requests per dashboard load | One aggregated dashboard endpoint |
| Role/team lookup per request | About half of all DB queries (the re-read is deliberate, see ENGINEERING_DECISIONS.md) | Short-TTL in-process cache, invalidated on role changes |
| History growth | `item_changes` is append-only and unbounded (70% of storage) | Partition, and archive closed items older than N months |
| No async processing | No notifications or secondary work | Outbox table + worker (`FOR UPDATE SKIP LOCKED`) |

## 6. Changes by growth stage

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

## 7. What stays unchanged

| Design choice | Why it holds at 2M users |
|---|---|
| Optimistic locking on `items.version`, one `item_changes` row per version | Conflicts only occur on the same item; a 409 costs ~1 ms and writes nothing |
| Partial unique index (one open activity per item) and version triggers | Per-item constraints, no global locks |
| Composite `org_id` foreign keys | Tenant isolation and the basis for sharding |
| Stateless JWT | Replicas scale with no session affinity |
| Partial indexes for "my work" and pending approvals | Query cost depends on one user's open work, not total data size |

## 8. Reproduce

`bench/` contains the seeding script and load generator used for every number
above, with exact commands in `bench/README.md`. Re-measure before relying on
these numbers on different hardware or with a different endpoint mix.
