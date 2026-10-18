import { useEffect, useState } from 'react'
import { request, canManageTeam, isAdmin } from '../api/client'

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
  const [saving, setSaving] = useState(false)
  const base = `/workflows/${workflow.id}/stages`

  // Only admins and leaders may create/edit/delete stages.
  const canEdit = canManageTeam()
  // Only admins can list all org users (for approver picker).
  const showUsers = isAdmin()

  useEffect(() => {
    const promises: [Promise<Stage[]>, Promise<{ data: Named[] }>, Promise<Named[]>] = [
      request<Stage[]>('GET', base),
      request<{ data: Named[] }>('GET', '/teams?page=1&limit=100'),
      showUsers ? request<Named[]>('GET', '/auth/users') : Promise.resolve([]),
    ]
    Promise.all(promises)
      .then(([s, t, u]) => {
        setStages(s ?? [])
        setTeams(t.data ?? [])
        setUsers(u ?? [])
        setError('')
      })
      .catch((err: Error) => setError(err.message))
  }, [base, tick, showUsers])

  const teamName = (id: string) => teams.find((t) => t.id === id)?.name ?? id
  const userName = (id: string | null) => (id ? users.find((u) => u.id === id)?.username ?? id : '—')

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

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!form) return
    setSaving(true)
    setError('')
    try {
      await send(form.id, {
        name: form.name,
        description: form.description,
        assigned_team_id: form.assigned_team_id,
        position: Number(form.position) || 0,
        completion_requirements: form.requirements.map((r) => r.trim()).filter(Boolean),
        approval_required: form.approval_required,
        approver_user_id: form.approval_required ? form.approver_user_id || users[0]?.id : null,
      })
    } finally {
      setSaving(false)
    }
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

  const setReq = (i: number, v: string) =>
    form && setForm({ ...form, requirements: form.requirements.map((r, j) => (j === i ? v : r)) })

  return (
    <>
      <button className="btn-back" onClick={onBack}>← Back to Workflows</button>
      <div className="page-header">
        <div>
          <h2>{workflow.name}</h2>
          <div className="page-meta">Stages · {stages.length} total</div>
        </div>
        {canEdit && (
          <button className="btn btn-primary" onClick={() => open()}>+ Add Stage</button>
        )}
      </div>

      {error && <div className="alert alert-error">{error}</div>}

      {/* Stage form modal */}
      {form && canEdit && (
        <div className="modal-backdrop" onClick={(e) => e.target === e.currentTarget && setForm(null)}>
          <div className="modal">
            <div className="modal-header">
              <h3>{form.id ? 'Edit Stage' : 'New Stage'}</h3>
            </div>
            <form onSubmit={save}>
              <div className="modal-body">
                <div className="form-group">
                  <label>Stage Name</label>
                  <input required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="e.g. Review" />
                </div>
                <div className="form-group">
                  <label>Description</label>
                  <input value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} placeholder="Optional" />
                </div>
                <div className="form-group">
                  <label>Assigned Team</label>
                  <select required value={form.assigned_team_id} onChange={(e) => setForm({ ...form, assigned_team_id: e.target.value })}>
                    {teams.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
                  </select>
                </div>
                <div className="form-group">
                  <label>Position</label>
                  <input
                    type="number"
                    min={1}
                    max={stages.length + 1}
                    placeholder={`Last (${stages.length + 1})`}
                    value={form.position}
                    onChange={(e) => setForm({ ...form, position: e.target.value })}
                  />
                </div>
                <div className="form-group">
                  <label>Completion Requirements</label>
                  {form.requirements.map((r, i) => (
                    <div key={i} style={{ display: 'flex', gap: 6, marginBottom: 6 }}>
                      <input value={r} onChange={(e) => setReq(i, e.target.value)} placeholder={`Requirement ${i + 1}`} />
                      <button
                        type="button"
                        className="btn btn-danger"
                        style={{ whiteSpace: 'nowrap', padding: '6px 10px', fontSize: 12 }}
                        onClick={() => setForm({ ...form, requirements: form.requirements.filter((_, j) => j !== i) })}
                      >
                        Remove
                      </button>
                    </div>
                  ))}
                  <button
                    type="button"
                    className="btn btn-ghost"
                    style={{ fontSize: 12, padding: '4px 10px', alignSelf: 'flex-start' }}
                    onClick={() => setForm({ ...form, requirements: [...form.requirements, ''] })}
                  >
                    + Add requirement
                  </button>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                  <input
                    type="checkbox"
                    id="approval"
                    checked={form.approval_required}
                    style={{ width: 'auto' }}
                    onChange={(e) => setForm({ ...form, approval_required: e.target.checked })}
                  />
                  <label htmlFor="approval" style={{ textTransform: 'none', letterSpacing: 0, fontSize: 13, fontWeight: 500, color: 'var(--text)' }}>
                    Requires approval
                  </label>
                </div>
                {form.approval_required && showUsers && (
                  <div className="form-group">
                    <label>Approver</label>
                    <select value={form.approver_user_id || users[0]?.id || ''} onChange={(e) => setForm({ ...form, approver_user_id: e.target.value })}>
                      {users.map((u) => <option key={u.id} value={u.id}>{u.username}</option>)}
                    </select>
                  </div>
                )}
              </div>
              <div className="modal-footer">
                <button type="button" className="btn btn-ghost" onClick={() => setForm(null)}>Cancel</button>
                <button type="submit" className="btn btn-primary" disabled={saving}>{saving ? 'Saving…' : form.id ? 'Save changes' : 'Create'}</button>
              </div>
            </form>
          </div>
        </div>
      )}

      <div className="card">
        <table>
          <thead>
            <tr>
              <th>#</th>
              <th>Name</th>
              <th>Team</th>
              <th>Requirements</th>
              <th>Approval</th>
              {showUsers && <th>Approver</th>}
              <th>Updated</th>
              {canEdit && <th style={{ width: 1 }} />}
            </tr>
          </thead>
          <tbody>
            {stages.map((s, i) => (
              <tr key={s.id}>
                <td className="td-muted">{s.position}</td>
                <td>
                  <div style={{ fontWeight: 600 }}>{s.name}</div>
                  {s.description && <div className="td-muted" style={{ marginTop: 2 }}>{s.description}</div>}
                </td>
                <td>{teamName(s.assigned_team_id)}</td>
                <td className="td-muted">{(s.completion_requirements ?? []).map(text).join(', ') || '—'}</td>
                <td>
                  <span className={`badge ${s.approval_required ? 'badge-high' : 'badge-low'}`}>
                    {s.approval_required ? 'Required' : 'None'}
                  </span>
                </td>
                {showUsers && <td className="td-muted">{userName(s.approver_user_id)}</td>}
                <td className="td-muted">{new Date(s.updated_at).toLocaleDateString()}</td>
                {canEdit && (
                  <td>
                    <div className="td-actions">
                      <button className="btn btn-ghost" style={{ fontSize: 12, padding: '4px 8px' }} disabled={i === 0} onClick={() => send(s.id, bodyOf(s, s.position - 1))}>↑</button>
                      <button className="btn btn-ghost" style={{ fontSize: 12, padding: '4px 8px' }} disabled={i === stages.length - 1} onClick={() => send(s.id, bodyOf(s, s.position + 1))}>↓</button>
                      <button className="btn btn-ghost" style={{ fontSize: 12, padding: '4px 10px' }} onClick={() => open(s)}>Edit</button>
                      <button className="btn btn-danger" style={{ fontSize: 12, padding: '4px 10px' }} onClick={() => remove(s.id)}>Delete</button>
                    </div>
                  </td>
                )}
              </tr>
            ))}
            {stages.length === 0 && (
              <tr>
                <td colSpan={canEdit ? (showUsers ? 8 : 7) : (showUsers ? 7 : 6)} style={{ textAlign: 'center', color: 'var(--text-muted)', padding: '32px' }}>
                  No stages yet
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  )
}
