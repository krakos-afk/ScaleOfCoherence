# Benchmarks

Reproduces the measurements in [Scalability.md](../Scalability.md):
a seeded Target-A dataset (one org, 100k items, 1.5M changes, 400k activity
rows, 200k comments) and HTTP load against the real server. Run everything on
a scratch database.

```bash
export DATABASE_URL='postgres://postgres:postgres@localhost:5432/scaleco_scale?sslmode=disable'
export JWT_SECRET=bench-secret-0123456789abcdefghij

# 1. Schema
psql 'postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable' \
  -c 'DROP DATABASE IF EXISTS scaleco_scale' -c 'CREATE DATABASE scaleco_scale'
go run ./cmd/server -migrate-only

# 2. Data (bulk load disables triggers; re-enables them before ANALYZE)
HASH=$(go run ./bench/genhash bench-password-1234 | head -1)
sed "s|__BENCH_HASH__|$HASH|" bench/seed.sql | psql "$DATABASE_URL" -f -

# 3. Server
PORT=18098 go run ./cmd/server &

# 4. Token
TOKEN=$(curl -s -X POST http://127.0.0.1:18098/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"bench1@example.com","password":"bench-password-1234"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')
```

Measured on 2026-10-19: AMD Ryzen 7 7840HS (16 threads, NVMe, 14 GB RAM,
~3 GB free), PostgreSQL 17.11 default container settings, server and database
on the same host, loopback HTTP, warm cache.

## Latency (sequential, n=500–1000)

```bash
ID=50000000-0000-0000-0000-0000000004d2   # any seeded item id
go run ./bench/load -mode list     -n 1000 -token "$TOKEN"
go run ./bench/load -mode search   -n 1000 -token "$TOKEN"
go run ./bench/load -mode detail   -n 1000 -token "$TOKEN" -item "$ID"
go run ./bench/load -mode timeline -n 1000 -token "$TOKEN" -item "$ID"
go run ./bench/load -mode create   -n 500  -token "$TOKEN"
```

`login` mode measures the rate-limited response (10/min per email), not
bcrypt; for bcrypt cost use `go run ./bench/genhash -bench 40 bench-password-1234`.

## Throughput (concurrent, fixed duration)

```bash
go run ./bench/load -mode list   -d 15s -c 16 -token "$TOKEN"
go run ./bench/load -mode search -d 15s -c 8  -token "$TOKEN"
go run ./bench/load -mode create -d 20s -c 8  -token "$TOKEN"
```

## Claim latency

Claim needs an open, unassigned activity per item. Prepare 1,500 of them:

```bash
psql "$DATABASE_URL" <<'SQL'
DROP TABLE IF EXISTS bench_claim;
CREATE TEMP TABLE bench_claim AS
SELECT a.id AS act_id, a.item_id
FROM item_stage_activity a
WHERE a.completed_at IS NULL AND a.released_at IS NULL
  AND a.assigned_user_id IS NOT NULL
ORDER BY a.item_id
LIMIT 1500;
UPDATE item_stage_activity SET assigned_user_id = NULL
WHERE id IN (SELECT act_id FROM bench_claim);
\copy (SELECT item_id FROM bench_claim) TO 'claim_ids.txt'
SQL

go run ./bench/load -mode claim -n 1000 -token "$TOKEN" -itemfile claim_ids.txt
```

## Sizes

```bash
psql "$DATABASE_URL" -c "
SELECT relname, n_live_tup,
       pg_total_relation_size(oid) AS total_bytes,
       pg_table_size(oid) AS table_bytes,
       pg_indexes_size(oid) AS index_bytes
FROM pg_class
WHERE relname IN ('items','item_changes','item_stage_activity','item_comments')
ORDER BY total_bytes DESC;"
```
