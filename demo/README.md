# Demo

Seeds a demo workspace and drives the main flows through the public HTTP API:
signup and login, admin-only endpoints, team roles, team-owned workflow stages,
item versioning with a stale-write `409`, claim/complete, reject/approve, stage
advance, close, comments, and the merged timeline.

The server seeds this workspace through its own API at startup when
`SEED_DEMO=true` (the default). Startup seeding reuses the workspace and its
demo item, so restarts do not create duplicate items. The standalone command
below remains useful for walking the flow again against a running server; each
manual invocation adds one item. The demo never touches the database.

    go run ./demo -base http://localhost:8080

The workspace (`Scale Demo`) is created on the first run and reused afterwards,
so the command is safe to repeat. The login page lists the same accounts and
signs in with one click.

| Account | Email | Role |
|---|---|---|
| `demo-admin` | `demo-admin@demo.local` | org admin |
| `demo-leader` | `demo-leader@demo.local` | user, team leader |
| `demo-editor` | `demo-editor@demo.local` | user, editor |
| `demo-commentator` | `demo-commentator@demo.local` | user, commentator |
| `demo-viewer` | `demo-viewer@demo.local` | user, viewer |
| `demo-member` | `demo-member@demo.local` | user, no team role |

All accounts use the password `demo-password-123`. The auth endpoints are
rate-limited; several runs in quick succession may return `429`.
