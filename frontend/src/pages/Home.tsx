import { useEffect, useState } from 'react'
import { request, userId, usertype, isAdmin, canManageTeam, canWrite } from '../api/client'
import CrudTable from '../components/CrudTable'
import TeamMembers from '../components/TeamMembers'
import WorkflowStages from '../components/WorkflowStages'
import Layout, { type Item } from '../components/Layout'
import Users from './Users'
import Items from './Items'

// ── Nav items by role ─────────────────────────────────────────────────────────

function navItems(): Item[] {
  const role = usertype()
  switch (role) {
    case 'admin':
      return [
        { key: 'dashboard', label: 'Dashboard' },
        { key: 'items', label: 'Items' },
        { key: 'teams', label: 'Teams' },
        { key: 'workflows', label: 'Workflows' },
        { key: 'users', label: 'Users' },
      ]
    case 'leader':
      return [
        { key: 'dashboard', label: 'Dashboard' },
        { key: 'items', label: 'Items' },
        { key: 'teams', label: 'Teams' },
        { key: 'workflows', label: 'Workflows' },
      ]
    default:
      // editor / viewer
      return [
        { key: 'dashboard', label: 'Dashboard' },
        { key: 'items', label: 'Items' },
      ]
  }
}

const bottomItems: Item[] = [
  { key: 'settings', label: 'Settings' },
  { key: 'profile', label: 'Profile' },
]

// ── Types ─────────────────────────────────────────────────────────────────────

type Stats = { items: number; teams: number; workflows: number; stages: number }

// ── Component ─────────────────────────────────────────────────────────────────

export default function Home({ onLogout }: { onLogout: () => void }) {
  const [page, setPage] = useState('dashboard')
  const [team, setTeam] = useState<{ id: string; name: string } | null>(null)
  const [workflow, setWorkflow] = useState<{ id: string; name: string } | null>(null)
  const [stats, setStats] = useState<Stats>({ items: 0, teams: 0, workflows: 0, stages: 0 })
  const [error, setError] = useState('')

  const role = usertype()
  const admin = isAdmin()
  const manageTeam = canManageTeam()
  const write = canWrite()

  useEffect(() => {
    const loadWorkflows = request<{ data: { id: string; name: string }[] }>('GET', '/workflows?page=1&limit=100')
      .then(async (res) => {
        const stageCounts = await Promise.all(
          res.data.map((w) =>
            request<{ id: string }[]>('GET', `/workflows/${w.id}/stages`).then((s) => s.length),
          ),
        )
        return { workflows: res.data.length, stages: stageCounts.reduce((a, b) => a + b, 0) }
      })

    Promise.all([
      request<{ total: number }>('GET', '/items?page=1&limit=1').then((r) => r.total),
      request<{ total: number }>('GET', '/teams?page=1&limit=1').then((r) => r.total),
      loadWorkflows,
    ])
      .then(([items, teams, { workflows, stages }]) => {
        setStats({ items, teams, workflows, stages })
      })
      .catch((err: Error) => setError(err.message))
  }, [])

  const deleteAccount = async () => {
    if (!confirm('This will permanently delete your account. Continue?')) return
    try {
      await request('DELETE', `/auth/users/${userId()}`)
      onLogout()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  // ── Role badge helper ──────────────────────────────────────────────────────
  const roleBadgeClass = () => {
    switch (role) {
      case 'admin': return 'badge-urgent'
      case 'leader': return 'badge-high'
      case 'editor': return 'badge-medium'
      default: return 'badge-low'
    }
  }

  // ── Page views ─────────────────────────────────────────────────────────────

  const views: Record<string, React.ReactNode> = {
    dashboard: (
      <>
        <div className="page-header">
          <div>
            <h2>Dashboard</h2>
            <div className="page-meta">Overview of your workspace</div>
          </div>
        </div>
        {error && <div className="alert alert-error">{error}</div>}
        <div className="stat-grid">
          <div className="stat-card">
            <div className="stat-label">Items</div>
            <div className="stat-value">{stats.items}</div>
          </div>
          <div className="stat-card">
            <div className="stat-label">Teams</div>
            <div className="stat-value">{stats.teams}</div>
          </div>
          <div className="stat-card">
            <div className="stat-label">Workflows</div>
            <div className="stat-value">{stats.workflows}</div>
          </div>
          <div className="stat-card">
            <div className="stat-label">Stages</div>
            <div className="stat-value">{stats.stages}</div>
          </div>
        </div>
        <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>
          {admin
            ? 'You have admin access. Use the sidebar to manage items, teams, workflows, and users.'
            : manageTeam
            ? 'You are a team leader. Use the sidebar to manage your teams, items, and workflows.'
            : write
            ? 'You are an editor. Use the sidebar to browse and edit items.'
            : 'You are a viewer. Use the sidebar to browse items.'}
        </div>
      </>
    ),

    // ── Users (admin only) ─────────────────────────────────────────────────
    users: admin ? <Users /> : null,

    // ── Teams ──────────────────────────────────────────────────────────────
    teams: team ? (
      <TeamMembers
        team={team}
        onBack={() => setTeam(null)}
        canEdit={manageTeam}
      />
    ) : (
      <CrudTable
        title="Teams"
        path="/teams"
        fields={[{ key: 'name', label: 'Name', required: true }]}
        manageLabel="Members"
        onManage={setTeam}
        readOnly={!manageTeam}
      />
    ),

    // ── Workflows ──────────────────────────────────────────────────────────
    workflows: workflow ? (
      <WorkflowStages workflow={workflow} onBack={() => setWorkflow(null)} />
    ) : (
      <CrudTable
        title="Workflows"
        path="/workflows"
        fields={[
          { key: 'name', label: 'Name', required: true },
          { key: 'description', label: 'Description', textarea: true },
        ]}
        manageLabel="Stages"
        onManage={setWorkflow}
        readOnly={!admin}
      />
    ),

    // ── Items (dedicated component with full field support) ────────────────
    items: <Items />,

    // ── Settings ───────────────────────────────────────────────────────────
    settings: (
      <>
        <div className="page-header">
          <div>
            <h2>Settings</h2>
            <div className="page-meta">Manage your account</div>
          </div>
        </div>
        {error && <div className="alert alert-error">{error}</div>}
        <div className="card" style={{ padding: '24px', maxWidth: 480 }}>
          <div style={{ fontWeight: 600, marginBottom: 6 }}>Danger Zone</div>
          <div style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 14 }}>
            Deleting your account is permanent and cannot be undone.
          </div>
          <button
            className="btn btn-ghost"
            style={{ color: 'var(--danger)', borderColor: 'var(--danger)' }}
            onClick={deleteAccount}
          >
            Delete my account
          </button>
        </div>
      </>
    ),

    // ── Profile ────────────────────────────────────────────────────────────
    profile: (
      <>
        <div className="page-header">
          <div>
            <h2>Profile</h2>
            <div className="page-meta">Your account details</div>
          </div>
        </div>
        <div className="card" style={{ padding: '24px', maxWidth: 480 }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
            <div>
              <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '.05em', color: 'var(--text-muted)', marginBottom: 6 }}>
                Role
              </div>
              <span className={`badge ${roleBadgeClass()}`} style={{ textTransform: 'capitalize' }}>{role}</span>
            </div>
            <div>
              <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '.05em', color: 'var(--text-muted)', marginBottom: 6 }}>
                User ID
              </div>
              <code style={{ fontSize: 12, color: 'var(--text-muted)', wordBreak: 'break-all' }}>{userId()}</code>
            </div>
            <hr style={{ border: 'none', borderTop: '1px solid var(--border)' }} />
            <button className="btn btn-ghost" onClick={onLogout} style={{ alignSelf: 'flex-start' }}>
              Sign out
            </button>
          </div>
        </div>
      </>
    ),
  }

  const navigate = (key: string) => {
    if (key !== 'teams') setTeam(null)
    if (key !== 'workflows') setWorkflow(null)
    setPage(key)
  }

  return (
    <Layout items={navItems()} bottom={bottomItems} page={page} onNavigate={navigate}>
      {views[page] ?? null}
    </Layout>
  )
}
