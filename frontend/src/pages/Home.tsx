import { useState } from 'react'
import { request, userId, usertype } from '../api/client'
import CrudTable from '../components/CrudTable'
import TeamMembers from '../components/TeamMembers'
import WorkflowStages from '../components/WorkflowStages'
import Layout, { type Item } from '../components/Layout'

const adminItems: Item[] = [
  { key: 'dashboard', label: 'Dashboard' },
  { key: 'teams', label: 'Teams' },
  { key: 'workflows', label: 'Workflows' },
  { key: 'items', label: 'Items' },
]
const bottomItems: Item[] = [
  { key: 'settings', label: 'Settings' },
  { key: 'profile', label: 'Profile' },
]

export default function Home({ onLogout }: { onLogout: () => void }) {
  const [page, setPage] = useState('dashboard')
  const [team, setTeam] = useState<{ id: string; name: string } | null>(null)
  const [workflow, setWorkflow] = useState<{ id: string; name: string } | null>(null)
  const [error, setError] = useState('')
  const admin = usertype() === 'admin'

  const deleteAccount = async () => {
    if (!confirm('Delete your account?')) return
    try {
      await request('DELETE', `/auth/users/${userId()}`)
      onLogout()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const views: Record<string, React.ReactNode> = {
    dashboard: <h2>Dashboard</h2>,
    teams: team ? (
      <TeamMembers team={team} onBack={() => setTeam(null)} />
    ) : (
      <CrudTable title="Teams" path="/teams" fields={[{ key: 'name', label: 'Name', required: true }]} onManage={setTeam} />
    ),
    workflows: workflow ? (
      <WorkflowStages workflow={workflow} onBack={() => setWorkflow(null)} />
    ) : (
      <CrudTable
        title="Workflows"
        path="/workflows"
        fields={[
          { key: 'name', label: 'Name', required: true },
          { key: 'description', label: 'Description' },
        ]}
        manageLabel="Stages"
        onManage={setWorkflow}
      />
    ),
    items: (
      <CrudTable
        title="Items"
        path="/items"
        fields={[
          { key: 'title', label: 'Title', required: true },
          { key: 'description', label: 'Description' },
          { key: 'priority', label: 'Priority', options: ['medium', 'urgent', 'high', 'low'] },
        ]}
      />
    ),
    settings: (
      <>
        <h2>Settings</h2>
        <button onClick={deleteAccount}>Delete account</button>
      </>
    ),
    profile: (
      <>
        <h2>Profile</h2>
        <p>Role: {usertype()}</p>
        <button onClick={onLogout}>Log out</button>
      </>
    ),
  }

  return (
    <Layout items={admin ? adminItems : []} bottom={bottomItems} page={page} onNavigate={setPage}>
      {views[page]}
      {error && <div>{error}</div>}
    </Layout>
  )
}
