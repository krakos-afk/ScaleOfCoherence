# ScaleOfCoherence

---

This Project is made for a challenge round. \
Find Architecture Decisions [here](ENGINEERING_DECISIONS.md). \
Find Dependency Decisions [here](DEPENDENCY_MAP.md). \
Find Scalability [here](Scalability.md).

This contains the basic summary of the project, along with instructions on how to run the project locally and my thoughts on the project.

---

## What it is

A multi-tenant collaboration workspace for tracked work. An organisation signs
up (the creator becomes admin), sets up teams and workflows, then runs items
through configurable stages:

- **Organisation & users** — JWT auth; org admins manage members. Every member is
  an `admin` or `user`.
- **Teams & roles** — team members hold a role: `viewer`, `commentator`,
  `editor` or `leader`, which gates stage activity and comments.
- **Workflows** — ordered stages, each owned by a team; a stage can require
  approval by a designated approver.
- **Items** — priority, custom fields, workflow/stage, open/closed, and
  optimistic locking via a per-item `version`.
- **Stage activity** — one open activity per item: claim → complete →
  (optional approve/reject); leaders can reassign, admins can advance stages.
- **Timeline & comments** — every mutation bumps the item version and lands on a
  single timeline; comments are anchored to the version they were written
  against. Nothing exists off the version track.
- **Findability** — trigram-powered search, filters, and sortable columns on
  every list; a dashboard widget surfaces "what needs my attention" (my open
  work plus pending approvals).

The Project uses Go as the server, postgres for the database, and React+Typescript for the frontend. \
Hosted on Azure Container Apps — SPA and API in one container -> [Deployment runbook](DEPLOYMENT.md).

## Tech stack

| Layer | Choice |
|---|---|
| Backend | Go 1.27, stdlib `net/http` (ServeMux), `database/sql` + pgx driver |
| Auth | JWT (HS256), bcrypt password hashing |
| Database | PostgreSQL 17, migrations via golang-migrate |
| Frontend | React 19 + TypeScript + Vite (no router/state libraries — small custom layout + fetch client) |

## Repository layout

```text
cmd/server/                 entrypoint: config, migrations, wiring, HTTP server
internal/api/               router + auth middleware (and controllers/)
internal/auth/              signup/login, JWT, identity helpers
internal/teams/             teams, members, roles
internal/workflows/         workflows and stages
internal/items/             item lifecycle, activity, timeline
internal/comments/          item comments
internal/database/          connection + repository layer (SQL, triggers)
migrations/                 golang-migrate SQL migrations (000001…000009)
frontend/src/               api client, components, hooks, pages, styles
```

## Setup Instructions

### Option 1 — Docker (easiest, everything in containers)

```text
docker compose up --build
```

Then open <http://localhost:8080>. This builds the app image (Go server with the
built frontend baked in) and starts it alongside Postgres; migrations run
automatically at boot. Set `APP_PORT` to change the published port.

### Option 2 — Run locally

```text
# 1. Start Postgres (docker compose)
docker compose -f compose.dev.yaml up -d

# 2. Run the server — migrations are applied automatically at boot
export JWT_SECRET=$(openssl rand -hex 32)   # required, at least 32 bytes
go run ./cmd/server            # listens on :8080

# 3. Run the frontend (separate terminal)
cd frontend
npm install
npm run dev                    # Vite dev server; proxies /auth /items /teams /workflows to :8080
```

Open the Vite URL, sign up — the first account of a new organisation becomes its
admin.

To run migrations without starting the server (for example as a deploy job):

```text
JWT_SECRET=... go run ./cmd/server -migrate-only
```

### Configuration

Every setting is validated at startup; the server refuses to boot on bad values.

| Variable | Default | Notes |
|---|---|---|
| `JWT_SECRET` | — | **Required**, at least 32 bytes |
| `JWT_TTL` | `1h` | Token lifetime |
| `DATABASE_URL` | — | Wins over the `DB_*` variables when set |
| `DB_HOST` / `DB_PORT` | `localhost` / `5432` | Matches `compose.dev.yaml` |
| `DB_USER` / `DB_PASSWORD` / `DB_NAME` | `postgres` / `postgres` / `scaleco` | |
| `DB_SSLMODE` | `disable` | |
| `DB_MAX_OPEN_CONNS` / `DB_MAX_IDLE_CONNS` | `20` / `20` | Connection pool limits |
| `DB_CONN_MAX_LIFETIME` / `DB_CONN_MAX_IDLE_TIME` | `30m` / `5m` | Pool recycling |
| `MIGRATE_ON_BOOT` | `true` | Set `false` when migrations run as a separate job |
| `PORT` | `8080` | HTTP listen port |
| `SPA_DIR` | — | Directory of the built frontend to serve (`/app/web` in the image; unset in local dev) |

See `.env.example` for a copy-paste template.

### Retrying requests safely

Item mutations (`POST`/`PUT`/`PATCH`/`DELETE` under `/items`, including
comments and activity transitions) accept an `Idempotency-Key` header:

```text
curl -X POST http://localhost:8080/items \
  -H "Authorization: Bearer $TOKEN" \
  -H "Idempotency-Key: $(uuidgen)" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Retry-safe item"}'
```

- Retrying with the same key and body replays the original response
  (`Idempotency-Replayed: true`), so a lost response never becomes a
  duplicate item.
- The same key with a different body → `422`; a retry while the first request
  is still running → `409`.
- Failed server-side requests (5xx) release the key so the retry can proceed.
  Keys are kept for 24h and pruned hourly.

## Checks

```text
go build ./... && go vet ./...
go test ./...                  # repository integration tests need Postgres; they skip if it is unreachable

cd frontend
npx tsc -b
npx eslint src
npm run build
```

The integration tests create and drop a `scaleco_test` database on the
configured Postgres (override with `TEST_DATABASE_URL`).

## My Thoughts

The data model, flows, structure are mostly handwritten; to develop the frontend design,
the baseline was designed, similarly service layer, DTOs, repository layer has been paid most attention,
to maintain data consistency, reduce redundancy, and ensure reliability for a first version, that
is supposed to be shipped to a team realistically, there are a lot of know flaws in the current 
system, which are intentional due to the time restrictions. My aim with this was able to 
implement a realistic first version to a client given the requirements, not a polished demo, 
hence the clearly basic UI, which I feel is easier to use and a backend that is reliable with a data model 
that avoids redundancy and tries to be as flexible for scaling as possible while also having the necessary constraints and
structure which probably took half the time to make. 

Scalability technicalities will be in [Scalability.md](Scalability.md)

There's no async in the application for now, since I could not find a fitting use case for the current features;
the default go server with postgres can handle a couple of hundred to a thousand users with 
ease. For bulk uploading or notifications, async would become necessary. However,  that has not been implemented, also the current frontend is static pages,
for which I had planned to implement SSE to call outdated versions for items, but for a first version that would be 
too broad of a scope, hence that has been skipped.

Interesting things are the Version control for items, that track every change to the item, handling concurrency. 
The rest is deliberately boring stuff: plain `database/sql`, a hand-rolled router and
fetch client, authorization enforced in the domain services/repositories rather
than in UI code. See [DEPENDENCY_MAP.md](DEPENDENCY_MAP.md) for how it fits
together and [ENGINEERING_DECISIONS.md](ENGINEERING_DECISIONS.md) for the why and tradeoffs.
