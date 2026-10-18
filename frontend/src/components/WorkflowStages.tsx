import { useEffect, useState } from 'react'
import { request } from '../api/client'

type Stage = {
  id: string
  name: string
  description: string
  assigned_team_id: string
  position: number
  completion_requirements: unknown[]
  approval_required: boolean
  approver_user_id: string | null
  created_at: string
  updated_at: string
}
type Named = { id: string; name?: string; username?: string }
type Form = {
  id?: string
  name: string
  description: string
  assigned_team_id: string
  position: string
  requirements: string[]
  approval_required: boolean
  approver_user_id: string
}

const text = (r: unknown) => (typeof r === 'string' ? r : JSON.stringify(r))

export default function WorkflowStages({ workflow, onBack }: { workflow: { id: string; name: string }; onBack: () => void }) {
  const [stages, setStages] = useState<Stage[]>([])
  const [teams, setTeams] = useState<Named[]>([])
  const [users, setUsers] = useState<Named[]>([])
  const [form, setForm] = useState<Form | null>(null)
  const [tick, setTick] = useState(0)
  const [error, setError] = useState('')
  const base = `/workflows/${workflow.id}/stages`

  useEffect(() => {
    Promise.all([
      request<Stage[]>('GET', base),
      request<{ data: Named[] }>('GET', '/teams?page=1&limit=100'),
      request<Named[]>('GET', '/auth/users'),
    ])
      .then(([s, t, u]) => {
        setStages(s ?? [])
        setTeams(t.data ?? [])
        setUsers(u ?? [])
        setError('')
      })
      .catch((err: Error) => setError(err.message))
  }, [base, tick])

  const teamName = (id: string) => teams.find((t) => t.id === id)?.name ?? id
  const userName = (id: string | null) => (id ? users.find((u) => u.id === id)?.username ?? id : '-')

  const open = (s?: Stage) =>
    setForm(
      s
        ? {
            id: s.id,
            name: s.name,
            description: s.description,
            assigned_team_id: s.assigned_team_id,
            position: String(s.position),
            requirements: (s.completion_requirements ?? []).map(text),
            approval_required: s.approval_required,
            approver_user_id: s.approver_user_id ?? '',
          }
        : { name: '', description: '', assigned_team_id: teams[0]?.id ?? '', position: '', requirements: [], approval_required: false, approver_user_id: '' },
    )

  const send = async (id: string | undefined, body: object) => {
    try {
      await (id ? request('PUT', `${base}/${id}`, body) : request('POST', base, body))
      setForm(null)
      setTick((t) => t + 1)
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const bodyOf = (s: Stage, position: number) => ({
    name: s.name,
    description: s.description,
    assigned_team_id: s.assigned_team_id,
    position,
    completion_requirements: s.completion_requirements ?? [],
    approval_required: s.approval_required,
    approver_user_id: s.approver_user_id,
  })

  const save = (e: React.FormEvent) => {
    e.preventDefault()
    if (!form) return
    send(form.id, {
      name: form.name,
      description: form.description,
      assigned_team_id: form.assigned_team_id,
      position: Number(form.position) || 0,
      completion_requirements: form.requirements.map((r) => r.trim()).filter(Boolean),
      approval_required: form.approval_required,
      approver_user_id: form.approval_required ? form.approver_user_id || users[0]?.id : null,
    })
  }

  const remove = async (id: string) => {
    if (!confirm('Delete this stage?')) return
    try {
      await request('DELETE', `${base}/${id}`)
      setTick((t) => t + 1)
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const setReq = (i: number, v: string) => form && setForm({ ...form, requirements: form.requirements.map((r, j) => (j === i ? v : r)) })

  return (
    <>
      <button onClick={onBack}>Back</button>
      <h2>{workflow.name} stages</h2>
      <button onClick={() => open()}>Add stage</button>
      {error && <div>{error}</div>}
      {form && (
        <form onSubmit={save}>
          <h3>{form.id ? 'Edit stage' : 'New stage'}</h3>
          <label>Name <input required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} /></label>
          <label>Description <input value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} /></label>
          <label>
            Assigned team
            <select required value={form.assigned_team_id} onChange={(e) => setForm({ ...form, assigned_team_id: e.target.value })}>
              {teams.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
            </select>
          </label>
          <label>
            Position
            <input type="number" min={1} max={stages.length + 1} placeholder="Last" value={form.position} onChange={(e) => setForm({ ...form, position: e.target.value })} />
          </label>
          <div>
            Completion requirements
            {form.requirements.map((r, i) => (
              <div key={i}>
                <input value={r} onChange={(e) => setReq(i, e.target.value)} />
                <button type="button" onClick={() => setForm({ ...form, requirements: form.requirements.filter((_, j) => j !== i) })}>Remove</button>
              </div>
            ))}
            <button type="button" onClick={() => setForm({ ...form, requirements: [...form.requirements, ''] })}>Add requirement</button>
          </div>
          <label>
            <input type="checkbox" checked={form.approval_required} onChange={(e) => setForm({ ...form, approval_required: e.target.checked })} /> Approval required
          </label>
          {form.approval_required && (
            <label>
              Approver
              <select value={form.approver_user_id || users[0]?.id || ''} onChange={(e) => setForm({ ...form, approver_user_id: e.target.value })}>
                {users.map((u) => <option key={u.id} value={u.id}>{u.username}</option>)}
              </select>
            </label>
          )}
          <button type="submit">{form.id ? 'Update' : 'Create'}</button>
          <button type="button" onClick={() => setForm(null)}>Cancel</button>
        </form>
      )}
      <table>
        <thead>
          <tr>
            <th>#</th><th>Name</th><th>Description</th><th>Team</th><th>Requirements</th><th>Approval</th><th>Approver</th><th>Created</th><th>Updated</th><th />
          </tr>
        </thead>
        <tbody>
          {stages.map((s, i) => (
            <tr key={s.id}>
              <td>{s.position}</td>
              <td>{s.name}</td>
              <td>{s.description}</td>
              <td>{teamName(s.assigned_team_id)}</td>
              <td>{(s.completion_requirements ?? []).map(text).join(', ') || '-'}</td>
              <td>{s.approval_required ? 'Yes' : 'No'}</td>
              <td>{userName(s.approver_user_id)}</td>
              <td>{new Date(s.created_at).toLocaleDateString()}</td>
              <td>{new Date(s.updated_at).toLocaleDateString()}</td>
              <td>
                <button disabled={i === 0} onClick={() => send(s.id, bodyOf(s, s.position - 1))}>Up</button>
                <button disabled={i === stages.length - 1} onClick={() => send(s.id, bodyOf(s, s.position + 1))}>Down</button>
                <button onClick={() => open(s)}>Edit</button>
                <button onClick={() => remove(s.id)}>Delete</button>
              </td>
            </tr>
          ))}
          {stages.length === 0 && <tr><td colSpan={10}>No stages</td></tr>}
        </tbody>
      </table>
    </>
  )
}
