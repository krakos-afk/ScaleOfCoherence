import { useEffect, useState, useCallback } from 'react'
import { request, userId, canManageTeam, isAdmin, canWrite } from '../api/client'

// ─── Types ────────────────────────────────────────────────────────────────────

type Activity = {
  id: number
  item_id: string
  stage_id: string
  team_id: string
  assigned_user_id: string | null
  assigned_by: string
  assigned_at: string
  item_version: number
  completed_by: string | null
  completion_note: string | null
  completed_at: string | null
  released_at: string | null
  release_reason: string | null
  approval_status: string | null
  approved_by: string | null
  approved_at: string | null
  approval_note: string | null
}

type Change = {
  id: number
  item_id: string
  version: number
  actor_id: string
  changes: Record<string, unknown>
  created_at: string
}

type ItemFull = {
  id: string
  title: string
  description: string
  priority: string
  workflow_id: string | null
  current_stage_id: string | null
  custom_fields: Record<string, string>
  closed_at: string | null
  version: number
  created_by: string
  created_at: string
  updated_at: string
}

type Stage = { id: string; name: string; workflowId: string; workflowName: string }
type User = { id: string; username: string }

type Props = {
  itemId: string
  stages: Stage[]
  onClose: () => void
  onItemUpdated: () => void
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function priorityClass(p: string) {
  if (p === 'urgent') return 'badge-urgent'
  if (p === 'high') return 'badge-high'
  if (p === 'medium') return 'badge-medium'
  return 'badge-low'
}

function fmt(ts: string | null | undefined) {
  if (!ts) return '—'
  return new Date(ts).toLocaleString()
}

function activityStatus(a: Activity): { label: string; cls: string } {
  if (a.released_at && a.approval_status === 'approved') return { label: 'Approved & done', cls: 'badge-medium' }
  if (a.released_at) return { label: 'Released', cls: 'badge-low' }
  if (a.approval_status === 'pending') return { label: 'Pending approval', cls: 'badge-high' }
  if (a.approval_status === 'rejected') return { label: 'Rejected — redo needed', cls: 'badge-urgent' }
  if (!a.assigned_user_id) return { label: 'Unclaimed', cls: 'badge-low' }
  return { label: 'In progress', cls: 'badge-medium' }
}

// ─── Section wrapper ──────────────────────────────────────────────────────────

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div style={{ marginBottom: 24 }}>
      <div style={{
        fontSize: 11, fontWeight: 700, textTransform: 'uppercase',
        letterSpacing: '.08em', color: 'var(--text-muted)', marginBottom: 10,
        borderBottom: '1px solid var(--border)', paddingBottom: 6,
      }}>
        {title}
      </div>
      {children}
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ marginBottom: 10 }}>
      <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 2 }}>{label}</div>
      <div style={{ fontSize: 13 }}>{children}</div>
    </div>
  )
}

// ─── Main component ───────────────────────────────────────────────────────────

export default function ItemDetail({ itemId, stages, onClose, onItemUpdated }: Props) {
  const me = userId()
  const isAdminUser = isAdmin()
  const canManage = canManageTeam()
  const canDoWork = canWrite()

  const [item, setItem] = useState<ItemFull | null>(null)
  const [openActivity, setOpenActivity] = useState<Activity | null | 'none'>('none')
  const [activityLog, setActivityLog] = useState<Activity[]>([])
  const [changes, setChanges] = useState<Change[]>([])
  const [teamMembers, setTeamMembers] = useState<User[]>([])
  const [allUsers, setAllUsers] = useState<User[]>([])
  const [error, setError] = useState('')
  const [actError, setActError] = useState('')
  const [loading, setLoading] = useState(true)

  // Form state
  const [completeNote, setCompleteNote] = useState('')
  const [approveNote, setApproveNote] = useState('')
  const [reassignUser, setReassignUser] = useState('')
  const [busy, setBusy] = useState(false)

  const [tab, setTab] = useState<'detail' | 'activity' | 'history'>('detail')

  const load = useCallback(async () => {
    setError('')
    try {
      const [itemData, changesData, actLogData] = await Promise.all([
        request<ItemFull>('GET', `/items/${itemId}`),
        request<{ data: Change[] }>('GET', `/items/${itemId}/changes?page=1&limit=20`),
        request<{ data: Activity[] }>('GET', `/items/${itemId}/activity?page=1&limit=50`),
      ])
      const parsed = {
        ...itemData,
        custom_fields: (typeof itemData.custom_fields === 'string'
          ? JSON.parse(itemData.custom_fields as string)
          : itemData.custom_fields) ?? {},
      }
      setItem(parsed)
      setChanges(changesData.data)
      setActivityLog(actLogData.data)

      // Load open activity (404 = none)
      let openAct: Activity | null = null
      try {
        openAct = await request<Activity>('GET', `/items/${itemId}/activity/open`)
        setOpenActivity(openAct)

        // Load team members for this activity's team (for reassign)
        if (openAct.team_id) {
          try {
            const members = await request<{ data: { user_id: string; username: string }[] }>(
              'GET', `/teams/${openAct.team_id}/members?page=1&limit=100`
            )
            setTeamMembers(members.data.map((m) => ({ id: m.user_id, username: m.username })))
          } catch {/* non-fatal */}
        }
      } catch {
        setOpenActivity(null)
      }

      // Load all users (for username resolution + admin reassign picker)
      try {
        const users = await request<{ id: string; username: string }[]>('GET', '/auth/users')
        setAllUsers(users)
      } catch {/* non-fatal */}

    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }, [itemId])

  useEffect(() => { load() }, [load])

  const act = openActivity === 'none' ? null : openActivity

  // Resolve a user ID to a display name
  const resolveUser = (uid: string | null | undefined): string => {
    if (!uid) return '—'
    if (uid === me) return 'You'
    const found = allUsers.find(u => u.id === uid) || teamMembers.find(u => u.id === uid)
    return found ? found.username : uid.slice(0, 8) + '…'
  }

  const doAction = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    setActError('')
    try {
      await fn()
      await load()
      onItemUpdated()
    } catch (err) {
      setActError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const claim = () => doAction(() => request('POST', `/items/${itemId}/activity/claim`))

  const complete = () => {
    const note = completeNote.trim()
    if (!note) { setActError('Completion note is required'); return }
    doAction(async () => {
      await request('POST', `/items/${itemId}/activity/complete`, { note })
      setCompleteNote('')
    })
  }

  const approve = (approved: boolean) => {
    doAction(async () => {
      await request('POST', `/items/${itemId}/activity/approve`, { approved, note: approveNote.trim() })
      setApproveNote('')
    })
  }

  const reassign = () => {
    if (!reassignUser) { setActError('Select a user to reassign to'); return }
    doAction(async () => {
      await request('POST', `/items/${itemId}/activity/reassign`, { user_id: reassignUser })
      setReassignUser('')
    })
  }

  const stageName = (id: string | null) => {
    if (!id) return '—'
    const s = stages.find((s) => s.id === id)
    return s ? `${s.workflowName} › ${s.name}` : id.slice(0, 8) + '…'
  }

  const reassignOptions = (isAdminUser ? allUsers : teamMembers).filter((u) => u.id !== me)

  if (loading) {
    return (
      <div className="drawer-backdrop" onClick={(e) => e.target === e.currentTarget && onClose()}>
        <div className="drawer">
          <div style={{ padding: 32, color: 'var(--text-muted)', textAlign: 'center' }}>Loading…</div>
        </div>
      </div>
    )
  }

  if (!item) {
    return (
      <div className="drawer-backdrop" onClick={(e) => e.target === e.currentTarget && onClose()}>
        <div className="drawer">
          <div className="alert alert-error">{error || 'Item not found'}</div>
        </div>
      </div>
    )
  }

  return (
    <div className="drawer-backdrop" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div className="drawer">

        {/* Header */}
        <div style={{
          padding: '20px 24px 0',
          borderBottom: '1px solid var(--border)',
          display: 'flex', flexDirection: 'column', gap: 8,
        }}>
          <div style={{ display: 'flex', alignItems: 'flex-start', gap: 12 }}>
            <div style={{ flex: 1, minWidth: 0 }}>
              <h2 style={{ margin: 0, fontSize: 18, lineHeight: 1.3, wordBreak: 'break-word' }}>{item.title}</h2>
              <div style={{ display: 'flex', gap: 6, marginTop: 6, flexWrap: 'wrap', alignItems: 'center' }}>
                <span className={`badge ${priorityClass(item.priority)}`} style={{ textTransform: 'capitalize' }}>
                  {item.priority}
                </span>
                <span className={`badge ${item.closed_at ? 'badge-low' : 'badge-medium'}`}>
                  {item.closed_at ? 'Closed' : 'Open'}
                </span>
                <span className="badge badge-version">v{item.version}</span>
                {act && !act.released_at && (
                  <span className={`badge ${activityStatus(act).cls}`}>{activityStatus(act).label}</span>
                )}
              </div>
            </div>
            <button className="btn btn-ghost" style={{ padding: '4px 8px', fontSize: 18, lineHeight: 1 }} onClick={onClose}>✕</button>
          </div>

          {/* Tabs */}
          <div style={{ display: 'flex', gap: 0, marginTop: 8 }}>
            {(['detail', 'activity', 'history'] as const).map((t) => (
              <button
                key={t}
                onClick={() => setTab(t)}
                style={{
                  background: 'none', border: 'none', cursor: 'pointer',
                  padding: '8px 16px', fontSize: 13, fontWeight: tab === t ? 600 : 400,
                  color: tab === t ? 'var(--accent)' : 'var(--text-muted)',
                  borderBottom: tab === t ? '2px solid var(--accent)' : '2px solid transparent',
                  marginBottom: -1, textTransform: 'capitalize',
                }}
              >
                {t}
              </button>
            ))}
          </div>
        </div>

        {/* Body */}
        <div style={{ padding: '20px 24px', overflowY: 'auto', flex: 1 }}>
          {error && <div className="alert alert-error" style={{ marginBottom: 16 }}>{error}</div>}

          {/* ── Detail tab ── */}
          {tab === 'detail' && (
            <>
              {item.description && (
                <Section title="Description">
                  <p style={{ margin: 0, fontSize: 14, lineHeight: 1.6, color: 'var(--text)' }}>{item.description}</p>
                </Section>
              )}

              <Section title="Properties">
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0 24px' }}>
                  <Field label="Stage">{stageName(item.current_stage_id)}</Field>
                  <Field label="Priority">
                    <span className={`badge ${priorityClass(item.priority)}`} style={{ textTransform: 'capitalize' }}>
                      {item.priority}
                    </span>
                  </Field>
                  <Field label="Status">
                    <span className={`badge ${item.closed_at ? 'badge-low' : 'badge-medium'}`}>
                      {item.closed_at ? 'Closed' : 'Open'}
                    </span>
                  </Field>
                  <Field label="Version">v{item.version}</Field>
                  <Field label="Created by">{resolveUser(item.created_by)}</Field>
                  <Field label="Created">{fmt(item.created_at)}</Field>
                  <Field label="Updated">{fmt(item.updated_at)}</Field>
                  {item.closed_at && <Field label="Closed">{fmt(item.closed_at)}</Field>}
                </div>
              </Section>

              {Object.keys(item.custom_fields).length > 0 && (
                <Section title="Custom Fields">
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0 24px' }}>
                    {Object.entries(item.custom_fields).map(([k, v]) => (
                      <Field key={k} label={k}>{v || <span style={{ color: 'var(--text-muted)' }}>—</span>}</Field>
                    ))}
                  </div>
                </Section>
              )}
            </>
          )}

          {/* ── Activity tab ── */}
          {tab === 'activity' && (
            <>
              {actError && <div className="alert alert-error" style={{ marginBottom: 16 }}>{actError}</div>}

              {/* Current open activity actions */}
              {act === null && (
                <div style={{ color: 'var(--text-muted)', fontSize: 14, marginBottom: 24 }}>
                  No active stage assignment for this item.
                </div>
              )}

              {act !== null && act !== undefined && (
                <>
                  {/* Status card */}
                  <Section title="Current Assignment">
                    <div className="card" style={{ padding: 16, marginBottom: 0 }}>
                      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0 24px' }}>
                        <Field label="Stage">{stageName(act.stage_id)}</Field>
                        <Field label="Status">
                          {(() => { const s = activityStatus(act); return <span className={`badge ${s.cls}`}>{s.label}</span> })()}
                        </Field>
                        <Field label="Assigned to">
                          {act.assigned_user_id
                            ? <strong>{resolveUser(act.assigned_user_id)}</strong>
                            : <span style={{ color: 'var(--text-muted)' }}>Unclaimed</span>
                          }
                        </Field>
                        <Field label="Since">{fmt(act.assigned_at)}</Field>
                        {act.completed_by && (
                          <Field label="Submitted by">{resolveUser(act.completed_by)}</Field>
                        )}
                        {act.completion_note && <Field label="Completion note">{act.completion_note}</Field>}
                        {act.approval_note && <Field label="Approval note">{act.approval_note}</Field>}
                        {act.approved_by && (
                          <Field label="Reviewed by">{resolveUser(act.approved_by)} · {fmt(act.approved_at)}</Field>
                        )}
                      </div>
                    </div>
                  </Section>

                  {/* Actions */}
                  <Section title="Actions">
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>

                      {/* Claim — unclaimed and I'm a writer */}
                      {!act.assigned_user_id && canDoWork && !act.released_at && (
                        <div>
                          <button className="btn btn-primary" disabled={busy} onClick={claim}>
                            {busy ? 'Claiming…' : 'Claim this item'}
                          </button>
                          <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>
                            Self-assign to start working on it.
                          </div>
                        </div>
                      )}

                      {/* Complete — I'm the assigned user, not yet submitted, not pending approval */}
                      {act.assigned_user_id === me && !act.completed_by && act.approval_status !== 'pending' && !act.released_at && (
                        <div>
                          <div className="form-group" style={{ marginBottom: 8 }}>
                            <label>Completion note <span style={{ color: 'var(--danger)' }}>*</span></label>
                            <textarea
                              rows={2}
                              placeholder="Describe what was done…"
                              value={completeNote}
                              onChange={(e) => setCompleteNote(e.target.value)}
                              style={{ resize: 'vertical' }}
                            />
                          </div>
                          <button className="btn btn-primary" disabled={busy || !completeNote.trim()} onClick={complete}>
                            {busy ? 'Submitting…' : 'Mark as complete'}
                          </button>
                        </div>
                      )}

                      {/* Approve/Reject — pending approval, and I'm a manager */}
                      {act.approval_status === 'pending' && canManage && (
                        <div>
                          <div className="form-group" style={{ marginBottom: 8 }}>
                            <label>Approval note (optional)</label>
                            <textarea
                              rows={2}
                              placeholder="Optional note…"
                              value={approveNote}
                              onChange={(e) => setApproveNote(e.target.value)}
                              style={{ resize: 'vertical' }}
                            />
                          </div>
                          <div style={{ display: 'flex', gap: 8 }}>
                            <button className="btn btn-primary" disabled={busy} onClick={() => approve(true)}>
                              {busy ? '…' : '✓ Approve'}
                            </button>
                            <button className="btn btn-danger" disabled={busy} onClick={() => approve(false)}>
                              {busy ? '…' : '✕ Reject'}
                            </button>
                          </div>
                        </div>
                      )}

                      {/* Reassign — admin or leader, only when not already released */}
                      {canManage && !act.released_at && (
                        <div>
                          <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', marginBottom: 6 }}>
                            Reassign to
                          </div>
                          {reassignOptions.length === 0 ? (
                            <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                              No other team members available.
                            </div>
                          ) : (
                            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                              <select
                                value={reassignUser}
                                onChange={(e) => setReassignUser(e.target.value)}
                                style={{ flex: 1 }}
                              >
                                <option value="">— select user —</option>
                                {reassignOptions.map((u) => (
                                  <option key={u.id} value={u.id}>{u.username}</option>
                                ))}
                              </select>
                              <button className="btn btn-ghost" disabled={busy || !reassignUser} onClick={reassign}>
                                Reassign
                              </button>
                            </div>
                          )}
                        </div>
                      )}

                      {/* Passive states */}
                      {act.assigned_user_id && act.assigned_user_id !== me && act.approval_status !== 'pending' && !canManage && !act.released_at && (
                        <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                          This item is currently assigned to <strong>{resolveUser(act.assigned_user_id)}</strong>.
                        </div>
                      )}
                      {act.approval_status === 'pending' && !canManage && (
                        <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                          Waiting for approval from a team leader.
                        </div>
                      )}
                      {act.approval_status === 'rejected' && act.assigned_user_id === me && (
                        <div className="alert alert-error" style={{ marginBottom: 0 }}>
                          Your submission was rejected. Please review the approval note above and submit again.
                        </div>
                      )}
                    </div>
                  </Section>
                </>
              )}

              {/* Full activity log */}
              {activityLog.length > 0 && (
                <Section title="Activity Log">
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                    {activityLog.map((a) => {
                      const s = activityStatus(a)
                      return (
                        <div key={a.id} className="card" style={{ padding: '10px 14px' }}>
                          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 4 }}>
                            <span className={`badge ${s.cls}`}>{s.label}</span>
                            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{fmt(a.assigned_at)}</span>
                          </div>
                          <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                            Stage: {stageName(a.stage_id)}
                            {a.assigned_user_id && <> · Assignee: {resolveUser(a.assigned_user_id)}</>}
                            {a.completed_by && <> · Completed by: {resolveUser(a.completed_by)}</>}
                            {a.approved_by && <> · Reviewed by: {resolveUser(a.approved_by)}</>}
                          </div>
                          {a.completion_note && (
                            <div style={{ fontSize: 12, marginTop: 4, color: 'var(--text)' }}>
                              Note: {a.completion_note}
                            </div>
                          )}
                          {a.approval_note && (
                            <div style={{ fontSize: 12, marginTop: 2, color: 'var(--text-muted)', fontStyle: 'italic' }}>
                              Review: {a.approval_note}
                            </div>
                          )}
                        </div>
                      )
                    })}
                  </div>
                </Section>
              )}
            </>
          )}

          {/* ── History tab ── */}
          {tab === 'history' && (
            <Section title="Change History">
              {changes.length === 0 ? (
                <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>No changes recorded.</div>
              ) : (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                  {changes.map((c) => (
                    <div key={c.id} className="card" style={{ padding: '12px 16px' }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4 }}>
                        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                          <span className="badge badge-version">v{c.version}</span>
                          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                            by {resolveUser(c.actor_id)}
                          </span>
                        </div>
                        <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{fmt(c.created_at)}</span>
                      </div>
                      <div style={{ fontSize: 12, fontFamily: 'monospace', color: 'var(--text-muted)', wordBreak: 'break-all' }}>
                        {Object.entries(c.changes).map(([k, v]) => (
                          <div key={k}>
                            <span style={{ color: 'var(--text)', fontWeight: 600 }}>{k}</span>: {JSON.stringify(v)}
                          </div>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </Section>
          )}
        </div>
      </div>
    </div>
  )
}
