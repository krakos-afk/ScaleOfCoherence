import { useState } from 'react'
import { ApiError, request, setToken } from '../api/client'

const password = 'demo-password-123'

const accounts = [
  {
    email: 'demo-admin@demo.local',
    role: 'Admin',
    scope: 'Organisation',
    note: 'Manages users, teams and workflows, and approves stage work.',
  },
  {
    email: 'demo-leader@demo.local',
    role: 'Team leader',
    scope: 'Demo Team',
    note: 'Claims, completes, reassigns and advances the team’s stages.',
  },
  {
    email: 'demo-editor@demo.local',
    role: 'Editor',
    scope: 'Demo Team',
    note: 'Claims and completes stage work, and comments.',
  },
  {
    email: 'demo-commentator@demo.local',
    role: 'Commentator',
    scope: 'Demo Team',
    note: 'Comments on stage work but cannot claim it.',
  },
  {
    email: 'demo-viewer@demo.local',
    role: 'Viewer',
    scope: 'Demo Team',
    note: 'Read-only access to the team’s items.',
  },
  {
    email: 'demo-member@demo.local',
    role: 'Member',
    scope: 'No team',
    note: 'Org member without a team role: no stage work or comments.',
  },
]

const notSeeded = 'Demo accounts are not seeded on this server yet. Restart the server (or run `go run ./demo` once), then try again.'

export default function DemoLogin({ onDone }: { onDone: () => void }) {
  const [pending, setPending] = useState('')
  const [error, setError] = useState('')

  const signIn = async (email: string) => {
    setPending(email)
    setError('')
    try {
      const { token } = await request<{ token: string }>('POST', '/auth/login', { email, password })
      setToken(token)
      onDone()
    } catch (err) {
      setError(err instanceof ApiError && err.status === 401 ? notSeeded : (err as Error).message)
      setPending('')
    }
  }

  return (
    <div className="demo-card">
      <h2>Demo accounts</h2>
      <p className="auth-sub">
        One click signs you in. Every account uses the password <code>{password}</code>.
      </p>
      {error && <div className="alert alert-error">{error}</div>}
      <div className="demo-accounts">
        {accounts.map((a) => (
          <button
            key={a.email}
            className="demo-account"
            onClick={() => signIn(a.email)}
            disabled={pending !== ''}
          >
            <span className="demo-account-head">
              <span className="demo-role">{a.role}</span>
              <span className="demo-scope">{a.scope}</span>
            </span>
            <span className="demo-email">{a.email}</span>
            <span className="demo-note">{pending === a.email ? 'Signing in…' : a.note}</span>
          </button>
        ))}
      </div>
      <div className="demo-hint">
        Seeded by <code>go run ./demo</code>, which also walks an item through the
        workflow and prints its id.
      </div>
    </div>
  )
}
