import { useEffect, useState, useCallback } from 'react'
import { request, userId, canManageTeam, isAdmin, ApiError } from '../api/client'

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

type TimelineUserRef = { id: string; username: string }

type TimelineActivityState = {
  id: number
  stage_id: string
  team_id: string
  team_name: string
  assigned_user_id: string | null
  assigned_user?: TimelineUserRef
  completion_note: string | null
  approval_status: string | null
  approval_note: string | null
}

type TimelineEntry = {
  version: number
  created_at: string
  actor: TimelineUserRef
  type: string
  changes: Record<string, unknown>
  activity?: TimelineActivityState
}

type TimelinePage = { entries: TimelineEntry[]; next_before_version: number | null }

type Comment = {
  id: number
  item_id: string
  author_id: string
  author: string
  version: number
  body: string
  created_at: string
}

type CommentsPage = { data: Comment[]; total: number }

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

type Stage = { id: string; name: string; workflowId: string; workflowName: string; approver_user_id?: string | null; position?: number; assigned_team_id?: string | null }
type User = { id: string; username: string; role?: string }

type Props = {
  itemId: string
  stages: Stage[]
  onClose: () => void
  onItemUpdated: () => void
}

const TIMELINE_LIMIT = 50
const COMMENT_PAGE_SIZE = 50

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

function titleCase(s: string) {
  return s.charAt(0).toUpperCase() + s.slice(1)
}

function activityStatus(a: Activity): { label: string; cls: string } {
  if (a.released_at && a.approval_status === 'approved') return { label: 'Approved & done', cls: 'badge-medium' }
  if (a.released_at) return { label: 'Released', cls: 'badge-low' }
  if (a.approval_status === 'pending') return { label: 'Pending approval', cls: 'badge-high' }
  if (a.approval_status === 'rejected') return { label: 'Rejected — redo needed', cls: 'badge-urgent' }
  if (!a.assigned_user_id) return { label: 'Unclaimed', cls: 'badge-low' }
  return { label: 'In progress', cls: 'badge-medium' }
}

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

  const [item, setItem] = useState<ItemFull | null>(null)
  const [openActivity, setOpenActivity] = useState<Activity | null | 'none'>('none')
  const [timeline, setTimeline] = useState<TimelineEntry[]>([])
  const [nextBefore, setNextBefore] = useState<number | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [comments, setComments] = useState<Comment[]>([])
  const [commentTotal, setCommentTotal] = useState(0)
  const [commentPages, setCommentPages] = useState(1)
  const [commentDraft, setCommentDraft] = useState('')
  const [commentBusy, setCommentBusy] = useState(false)
  const [commentError, setCommentError] = useState('')
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

  const [tab, setTab] = useState<'detail' | 'timeline' | 'comments'>('detail')

  const load = useCallback(async () => {
    try {
      const [itemData, timelineData, commentData] = await Promise.all([
        request<ItemFull>('GET', `/items/${itemId}`),
        request<TimelinePage>('GET', `/items/${itemId}/timeline?limit=${TIMELINE_LIMIT}`),
        request<CommentsPage>('GET', `/items/${itemId}/comments?page=1&limit=${COMMENT_PAGE_SIZE}`),
      ])
      setError('')
      const parsed = {
        ...itemData,
        custom_fields: (typeof itemData.custom_fields === 'string'
          ? JSON.parse(itemData.custom_fields as string)
          : itemData.custom_fields) ?? {},
      }
      setItem(parsed)
      setTimeline(timelineData.entries)
      setNextBefore(timelineData.next_before_version)
      setComments(commentData.data)
      setCommentTotal(commentData.total)
      setCommentPages(1)

      // Load open activity (404 = none)
      let openAct: Activity | null = null
      try {
        openAct = await request<Activity>('GET', `/items/${itemId}/activity/open`)
        setOpenActivity(openAct)
      } catch {
        setOpenActivity(null)
      }

      // Determine the team responsible for this item's current stage so leader
      // checks work even when there is no open activity.
      const currentStage = itemData.current_stage_id
        ? stages.find((s) => s.id === itemData.current_stage_id)
        : undefined
      const teamID = openAct?.team_id ?? currentStage?.assigned_team_id ?? null
      if (teamID) {
        try {
          const members = await request<{ data: { user_id: string; username: string; role: string }[] }>(
            'GET', `/teams/${teamID}/members?page=1&limit=100`
          )
          setTeamMembers(members.data.map((m) => ({ id: m.user_id, username: m.username, role: m.role })))
        } catch {
          setTeamMembers([])
        }
      } else {
        setTeamMembers([])
      }

      // Org-scoped name directory readable by every member, so ids resolve to
      // usernames for non-admins too.
      try {
        const users = await request<{ id: string; username: string }[]>('GET', '/auth/user-names')
        setAllUsers(users)
      } catch {/* non-fatal */}

    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }, [itemId, stages])

  // fetch-on-mount/prop-change: every setState inside load() runs after an await
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => { load() }, [load, itemId, stages])

  const loadMore = async () => {
    if (nextBefore == null || loadingMore) return
    setLoadingMore(true)
    try {
      const page = await request<TimelinePage>(
        'GET', `/items/${itemId}/timeline?limit=${TIMELINE_LIMIT}&before_version=${nextBefore}`
      )
      setTimeline((prev) => [...prev, ...page.entries])
      setNextBefore(page.next_before_version)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoadingMore(false)
    }
  }

  const act = openActivity === 'none' ? null : openActivity

  const myTeamRole = teamMembers.find((u) => u.id === me)?.role
  const isTeamLeader = myTeamRole === 'leader'
  const isTeamEditor = myTeamRole === 'editor'
  const isTeamCommentator = myTeamRole === 'commentator'
  const canClaim = isAdminUser || isTeamEditor || isTeamLeader
  // Org admins and the item team's editors/commentators/leaders may comment;
  // the server enforces the same rule.
  const canComment = isAdminUser || isTeamEditor || isTeamCommentator || isTeamLeader
  const canManageActivity = canManage || isTeamLeader
  const isApprover = !!act?.stage_id && stages.find((s) => s.id === act.stage_id)?.approver_user_id === me
  const canApprove = canManageActivity || isApprover
  const canManageItem = isAdminUser || isTeamLeader

  const currentStage = item?.current_stage_id
    ? stages.find((s) => s.id === item.current_stage_id)
    : undefined
  const nextStage = currentStage
    ? stages
        .filter((s) => s.workflowId === currentStage.workflowId && (s.position ?? 0) > (currentStage.position ?? 0))
        .sort((a, b) => (a.position ?? 0) - (b.position ?? 0))[0]
    : undefined
  const canAdvance = canManageItem && !!nextStage

  const canDeleteComment = (c: Comment) => c.author_id === me || isAdminUser || isTeamLeader

  const loadMoreComments = async () => {
    try {
      const res = await request<CommentsPage>(
        'GET', `/items/${itemId}/comments?page=${commentPages + 1}&limit=${COMMENT_PAGE_SIZE}`
      )
      setComments((prev) => [...prev, ...res.data])
      setCommentTotal(res.total)
      setCommentPages((p) => p + 1)
    } catch (err) {
      setCommentError((err as Error).message)
    }
  }

  const postComment = async () => {
    const body = commentDraft.trim()
    if (!body) return
    setCommentBusy(true)
    setCommentError('')
    try {
      const created = await request<Comment>('POST', `/items/${itemId}/comments`, { body })
      setComments((prev) => [created, ...prev])
      setCommentTotal((t) => t + 1)
      setCommentDraft('')
    } catch (err) {
      setCommentError((err as Error).message)
    } finally {
      setCommentBusy(false)
    }
  }

  const removeComment = async (commentID: number) => {
    setCommentError('')
    setCommentBusy(true)
    try {
      await request('DELETE', `/items/${itemId}/comments/${commentID}`)
      setComments((prev) => prev.filter((c) => c.id !== commentID))
      setCommentTotal((t) => Math.max(0, t - 1))
    } catch (err) {
      setCommentError((err as Error).message)
    } finally {
      setCommentBusy(false)
    }
  }

  const resolveUser = (uid: string | null | undefined): string => {
    if (!uid) return '—'
    if (uid === me) return 'You'
    const found = allUsers.find(u => u.id === uid) || teamMembers.find(u => u.id === uid)
    return found ? found.username : uid.slice(0, 8) + '…'
  }

  const stageName = (id: string | null, full = true) => {
    if (!id) return '—'
    const s = stages.find((s) => s.id === id)
    if (!s) return id.slice(0, 8) + '…'
    return full ? `${s.workflowName} › ${s.name}` : s.name
  }

  // Every action sends the item's last known version. On 409 we discard any
  // local state, refetch, and ask the user to review.
  const doAction = async (fn: (version: number) => Promise<unknown>) => {
    if (!item) return
    setBusy(true)
    setActError('')
    try {
      await fn(item.version)
      await load()
      onItemUpdated()
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setActError('This item changed, please review.')
        await load()
        onItemUpdated()
      } else {
        setActError((err as Error).message)
      }
    } finally {
      setBusy(false)
    }
  }

  const claim = () => doAction((version) =>
    request('POST', `/items/${itemId}/activity/claim`, { version }))

  const complete = () => {
    const note = completeNote.trim()
    if (!note) { setActError('Completion note is required'); return }
    doAction(async (version) => {
      await request('POST', `/items/${itemId}/activity/complete`, { note, version })
      setCompleteNote('')
    })
  }

  const approve = (approved: boolean) => {
    doAction(async (version) => {
      await request('POST', `/items/${itemId}/activity/approve`, { approved, note: approveNote.trim(), version })
      setApproveNote('')
    })
  }

  const reassign = () => {
    if (!reassignUser) { setActError('Select a user to reassign to'); return }
    doAction(async (version) => {
      await request('POST', `/items/${itemId}/activity/reassign`, { user_id: reassignUser, version })
      setReassignUser('')
    })
  }

  const advance = () => doAction((version) =>
    request('POST', `/items/${itemId}/advance`, { version }))
  const setClosed = (closed: boolean) => doAction((version) =>
    request('POST', `/items/${itemId}/status`, { closed, version }))

  const reassignOptions = (isAdminUser ? allUsers : teamMembers).filter((u) => u.id !== me)

  const TYPE_LABELS: Record<string, string> = {
    created: 'Created item',
    updated: 'Updated item',
    assigned: 'Assignment changed',
    completed: 'Completed',
    approved: 'Approved',
    rejected: 'Rejected',
    stage_moved: 'Stage moved',
    closed: 'Status changed',
  }

  const TYPE_CLASS: Record<string, string> = {
    created: 'badge-medium',
    updated: 'badge-low',
    assigned: 'badge-medium',
    completed: 'badge-medium',
    approved: 'badge-medium',
    rejected: 'badge-urgent',
    stage_moved: 'badge-high',
    closed: 'badge-low',
  }

  const HIDDEN_CHANGE_KEYS = new Set(['type', 'activity_id', 'closed_activity_id', 'from', 'to'])
  const CHANGE_LABELS: Record<string, string> = {
    title: 'Title', description: 'Description', priority: 'Priority',
    workflow_id: 'Workflow', custom_fields: 'Custom fields', closed_at: 'Status',
    stage_id: 'Stage',
  }

  function renderValue(value: unknown): string {
    if (value === null || value === undefined || value === '') return '—'
    if (typeof value === 'boolean') return value ? 'yes' : 'no'
    if (typeof value === 'object') return JSON.stringify(value)
    return String(value)
  }

  function changeValue(k: string, value: unknown): string {
    switch (k) {
      case 'priority':
        return typeof value === 'string' ? titleCase(value) : renderValue(value)
      case 'workflow_id': {
        if (typeof value !== 'string' || !value) return '—'
        const s = stages.find((s) => s.workflowId === value)
        return s ? s.workflowName : value.slice(0, 8) + '…'
      }
      case 'closed_at':
        return value ? 'Closed' : 'Reopened'
      case 'stage_id':
        return stageName(typeof value === 'string' ? value : null, false)
      default:
        return renderValue(value)
    }
  }

  function TimelineRowContent({ e }: { e: TimelineEntry }) {
    const c = e.changes || {}
    const rows: React.ReactNode[] = []

    if (e.type === 'updated' || e.type === 'stage_moved') {
      for (const [k, v] of Object.entries(c)) {
        if (HIDDEN_CHANGE_KEYS.has(k)) continue
        const label = CHANGE_LABELS[k] ?? k
        if (Array.isArray(v) && v.length === 2) {
          rows.push(
            <div key={k} style={{ display: 'flex', gap: 8 }}>
              <span style={{ color: 'var(--text-muted)', minWidth: 84 }}>{label}</span>
              <span style={{ wordBreak: 'break-word' }}>
                {changeValue(k, v[0])} <span style={{ color: 'var(--text-muted)' }}>→</span> {changeValue(k, v[1])}
              </span>
            </div>,
          )
        } else {
          rows.push(
            <div key={k} style={{ display: 'flex', gap: 8 }}>
              <span style={{ color: 'var(--text-muted)', minWidth: 84 }}>{label}</span>
              <span>{changeValue(k, v)}</span>
            </div>,
          )
        }
      }
    }

    if (e.type === 'stage_moved') {
      rows.push(
        <div key="move" style={{ display: 'flex', gap: 8 }}>
          <span style={{ color: 'var(--text-muted)', minWidth: 84 }}>Stage</span>
          <span>{stageName((c.from as string) ?? null, false)} <span style={{ color: 'var(--text-muted)' }}>→</span> {stageName((c.to as string) ?? null, false)}</span>
        </div>,
      )
    }

    if (e.type === 'assigned') {
      rows.push(
        <div key="assign" style={{ display: 'flex', gap: 8 }}>
          <span style={{ color: 'var(--text-muted)', minWidth: 84 }}>Assignee</span>
          <span>{resolveUser(c.from as string | null)} <span style={{ color: 'var(--text-muted)' }}>→</span> {resolveUser(c.to as string)}</span>
        </div>,
      )
    }

    if (e.type === 'completed' && e.activity?.completion_note) {
      rows.push(
        <div key="note" style={{ display: 'flex', gap: 8 }}>
          <span style={{ color: 'var(--text-muted)', minWidth: 84 }}>Note</span>
          <span>{e.activity.completion_note}</span>
        </div>,
      )
    }

    if ((e.type === 'approved' || e.type === 'rejected') && e.activity?.approval_note) {
      rows.push(
        <div key="review" style={{ display: 'flex', gap: 8 }}>
          <span style={{ color: 'var(--text-muted)', minWidth: 84 }}>Review</span>
          <span>{e.activity.approval_note}</span>
        </div>,
      )
    }

    if (e.type === 'closed') {
      rows.push(
        <div key="closed" style={{ display: 'flex', gap: 8 }}>
          <span style={{ color: 'var(--text-muted)', minWidth: 84 }}>Status</span>
          <span>{(c.closed as boolean) ? 'Closed' : 'Reopened'}</span>
        </div>,
      )
    }

    if (rows.length === 0) {
      return <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>No additional detail.</div>
    }
    return <div style={{ display: 'flex', flexDirection: 'column', gap: 2, fontSize: 12 }}>{rows}</div>
  }

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
            {(['detail', 'timeline', 'comments'] as const).map((t) => (
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
                {t}{t === 'comments' && commentTotal > 0 ? ` (${commentTotal})` : ''}
              </button>
            ))}
          </div>
        </div>

        {/* Body */}
        <div style={{ padding: '20px 24px', overflowY: 'auto', flex: 1 }}>
          {error && <div className="alert alert-error" style={{ marginBottom: 16 }}>{error}</div>}
          {actError && <div className="alert alert-error" style={{ marginBottom: 16 }}>{actError}</div>}

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

              {canManageItem && (
                <Section title="Pipeline">
                  <div className="card" style={{ padding: 16, marginBottom: 0 }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap', alignItems: 'center' }}>
                      <div>
                        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 2 }}>Current stage</div>
                        <div style={{ fontSize: 13 }}>{stageName(item.current_stage_id)}</div>
                        <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>
                          {nextStage ? <>Next: {nextStage.name}</> : 'On the last stage'}
                        </div>
                      </div>
                      <div style={{ display: 'flex', gap: 8 }}>
                        <button
                          className="btn btn-primary"
                          disabled={busy || !canAdvance}
                          onClick={advance}
                          title={!nextStage ? 'Already on the last stage' : undefined}
                        >
                          {busy ? '…' : 'Advance to next stage →'}
                        </button>
                        {item.closed_at ? (
                          <button className="btn btn-ghost" disabled={busy} onClick={() => setClosed(false)}>
                            Reopen item
                          </button>
                        ) : (
                          <button className="btn btn-danger" disabled={busy} onClick={() => setClosed(true)}>
                            Close item
                          </button>
                        )}
                      </div>
                    </div>
                  </div>
                </Section>
              )}

              {/* Current assignment + actions */}
              {act === null && (
                <Section title="Current Assignment">
                  <div style={{ color: 'var(--text-muted)', fontSize: 14 }}>
                    No active stage assignment for this item.
                  </div>
                </Section>
              )}

              {act !== null && act !== undefined && (
                <>
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

                  <Section title="Actions">
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>

                      {!act.assigned_user_id && canClaim && !act.released_at && (
                        <div>
                          <button className="btn btn-primary" disabled={busy} onClick={claim}>
                            {busy ? 'Claiming…' : 'Claim this item'}
                          </button>
                          <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>
                            Self-assign to start working on it.
                          </div>
                        </div>
                      )}

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

                      {act.approval_status === 'pending' && canApprove && (
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

                      {canManageActivity && !act.released_at && (
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

                      {act.assigned_user_id && act.assigned_user_id !== me && act.approval_status !== 'pending' && !canManageActivity && !act.released_at && (
                        <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                          This item is currently assigned to <strong>{resolveUser(act.assigned_user_id)}</strong>.
                        </div>
                      )}
                      {act.approval_status === 'pending' && !canApprove && (
                        <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                          Waiting for approval.
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

          {/* ── Timeline tab ── */}
          {tab === 'timeline' && (
            <Section title="Timeline">
              {timeline.length === 0 ? (
                <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>No changes recorded.</div>
              ) : (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                  {timeline.map((e) => (
                    <div key={e.version} className="card" style={{ padding: '12px 16px' }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 6, gap: 8 }}>
                        <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
                          <span className="badge badge-version">v{e.version}</span>
                          <span className={`badge ${TYPE_CLASS[e.type] ?? 'badge-low'}`}>{TYPE_LABELS[e.type] ?? e.type}</span>
                          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                            by {e.actor?.username || resolveUser(e.actor?.id)}
                          </span>
                        </div>
                        <span style={{ fontSize: 11, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>{fmt(e.created_at)}</span>
                      </div>
                      <TimelineRowContent e={e} />
                    </div>
                  ))}
                  {nextBefore != null && (
                    <div style={{ textAlign: 'center', marginTop: 4 }}>
                      <button className="btn btn-ghost" disabled={loadingMore} onClick={loadMore}>
                        {loadingMore ? 'Loading…' : 'Load older'}
                      </button>
                    </div>
                  )}
                </div>
              )}
            </Section>
          )}

          {/* ── Comments tab ── */}
          {tab === 'comments' && (
            <Section title="Comments">
              {canComment ? (
                <div className="form-group" style={{ marginBottom: 16 }}>
                  <textarea
                    rows={3}
                    placeholder="Write a comment…"
                    value={commentDraft}
                    onChange={(e) => setCommentDraft(e.target.value)}
                    style={{ resize: 'vertical' }}
                  />
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8, marginTop: 8 }}>
                    <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                      Attached to v{item.version}
                    </span>
                    <button
                      className="btn btn-primary"
                      disabled={commentBusy || !commentDraft.trim()}
                      onClick={postComment}
                    >
                      {commentBusy ? 'Posting…' : 'Post comment'}
                    </button>
                  </div>
                </div>
              ) : (
                <div style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 16 }}>
                  Only editors, commentators, and leaders of this item's team can comment.
                </div>
              )}

              {commentError && <div className="alert alert-error" style={{ marginBottom: 12 }}>{commentError}</div>}

              {comments.length === 0 ? (
                <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>No comments yet.</div>
              ) : (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                  {comments.map((c) => (
                    <div key={c.id} className="card" style={{ padding: '12px 16px' }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, marginBottom: 6 }}>
                        <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
                          <strong style={{ fontSize: 13 }}>{c.author || resolveUser(c.author_id)}</strong>
                          <span className="badge badge-version">v{c.version}</span>
                          {canDeleteComment(c) && (
                            <button
                              className="btn btn-ghost"
                              style={{ padding: '2px 8px', fontSize: 11 }}
                              onClick={() => removeComment(c.id)}
                              disabled={commentBusy}
                              title="Delete comment"
                            >
                              Delete
                            </button>
                          )}
                        </div>
                        <span style={{ fontSize: 11, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                          {fmt(c.created_at)}
                        </span>
                      </div>
                      <div style={{ fontSize: 13, whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{c.body}</div>
                    </div>
                  ))}
                  {comments.length < commentTotal && (
                    <div style={{ textAlign: 'center', marginTop: 4 }}>
                      <button className="btn btn-ghost" onClick={loadMoreComments}>
                        Load older comments ({commentTotal - comments.length} more)
                      </button>
                    </div>
                  )}
                </div>
              )}
            </Section>
          )}
        </div>
      </div>
    </div>
  )
}
