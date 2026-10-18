import { useEffect, useState } from 'react'
import { request, canManageTeam, userId as getMyId } from '../api/client'
import SortableTh from './SortableTh'
import { useDebouncedValue } from '../hooks/useDebouncedValue'
import { useSort } from '../hooks/useSort'

const LIMIT = 10

type Member = { user_id: string; username: string; role: string }
type OrgUser = { id: string; username: string; usertype: string }

export default function TeamMembers({
  team,
  onBack,
  canEdit,
}: {
  team: { id: string; name: string }
  onBack: () => void
  /** If true, show add/remove controls. Caller derives this from role checks. */
  canEdit?: boolean
}) {
  const [members, setMembers] = useState<Member[]>([])
  const [users, setUsers] = useState<OrgUser[]>([])
  const [userId, setUserId] = useState('')
  const [role, setRole] = useState('viewer')
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [tick, setTick] = useState(0)
  const [error, setError] = useState('')
  const [q, setQ] = useState('')
  const search = useDebouncedValue(q).trim()
  const { sort, order, toggle } = useSort('username', 'asc')
  const base = `/teams/${team.id}/members`
  const reload = () => setTick((t) => t + 1)
  const changeSort = (field: string) => {
    toggle(field)
    setPage(1)
  }

  // Whether the current user is a leader of THIS team (checked after members load).
  const myId = getMyId()
  const isTeamLeader = members.some((m) => m.user_id === myId && m.role === 'leader')
  // Show edit controls if caller passed canEdit=true OR current user is a leader of this team.
  const showControls = canEdit || isTeamLeader

  useEffect(() => {
    const params = new URLSearchParams({ page: String(page), limit: String(LIMIT), sort, order })
    if (search) params.set('q', search)
    const requests: [Promise<{ data: Member[]; total: number }>, Promise<OrgUser[]>] = [
      request<{ data: Member[]; total: number }>('GET', `${base}?${params}`),
      // Only admins/leaders can list org users — gracefully degrade to empty if forbidden.
      canManageTeam()
        ? request<OrgUser[]>('GET', '/auth/users')
        : Promise.resolve([]),
    ]
    Promise.all(requests)
      .then(([m, u]) => {
        setMembers(m.data)
        setTotal(m.total)
        setUsers(u)
        setError('')
      })
      .catch((err: Error) => setError(err.message))
  }, [base, page, tick, search, sort, order])

  const pages = Math.max(1, Math.ceil(total / LIMIT))
  const selected = userId || users[0]?.id || ''

  const run = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
      if (members.length === 1 && page > 1) setPage(page - 1)
      setUserId('')
      reload()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  return (
    <>
      <button className="btn-back" onClick={onBack}>← Back to Teams</button>
      <div className="page-header">
        <div>
          <h2>{team.name}</h2>
          <div className="page-meta">Members · {total} total</div>
        </div>
      </div>

      {error && <div className="alert alert-error">{error}</div>}

      <div className="list-toolbar">
        <input
          type="search"
          className="search-input"
          placeholder="Search members…"
          value={q}
          onChange={(e) => { setQ(e.target.value); setPage(1) }}
        />
      </div>

      <div className="card">
        {showControls && users.length > 0 && (
          <form
            className="members-form"
            onSubmit={(e) => {
              e.preventDefault()
              void run(() => request('POST', base, { user_id: selected, role }))
            }}
          >
            <select value={selected} onChange={(e) => setUserId(e.target.value)} style={{ maxWidth: 200 }}>
              {users.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.username} ({u.usertype})
                </option>
              ))}
            </select>
            <select value={role} onChange={(e) => setRole(e.target.value)} style={{ maxWidth: 140 }}>
              <option value="viewer">viewer</option>
              <option value="editor">editor</option>
              <option value="commentator">commentator</option>
              <option value="leader">leader</option>
            </select>
            <button type="submit" className="btn btn-primary" disabled={!selected} style={{ whiteSpace: 'nowrap' }}>
              Add member
            </button>
          </form>
        )}

        <table>
          <thead>
            <tr>
              <SortableTh label="Username" field="username" sort={sort} order={order} onSort={changeSort} />
              <SortableTh label="Role" field="role" sort={sort} order={order} onSort={changeSort} />
              {showControls && <th style={{ width: 1 }} />}
            </tr>
          </thead>
          <tbody>
            {members.map((m) => (
              <tr key={m.user_id}>
                <td>{m.username}</td>
                <td>
                  <span className="badge badge-medium" style={{ textTransform: 'capitalize' }}>{m.role}</span>
                </td>
                {showControls && (
                  <td>
                    <div className="td-actions">
                      <button
                        className="btn btn-danger"
                        style={{ fontSize: 12, padding: '4px 10px' }}
                        onClick={() => run(() => request('DELETE', `${base}/${m.user_id}`))}
                      >
                        Remove
                      </button>
                    </div>
                  </td>
                )}
              </tr>
            ))}
            {members.length === 0 && (
              <tr>
                <td colSpan={showControls ? 3 : 2} style={{ textAlign: 'center', color: 'var(--text-muted)', padding: '32px' }}>
                  {search ? 'No matches for your search.' : 'No members yet'}
                </td>
              </tr>
            )}
          </tbody>
        </table>
        <div className="pagination">
          <button className="btn btn-ghost" style={{ padding: '4px 10px', fontSize: 12 }} disabled={page <= 1} onClick={() => setPage(page - 1)}>← Prev</button>
          <span>Page {page} of {pages}</span>
          <button className="btn btn-ghost" style={{ padding: '4px 10px', fontSize: 12 }} disabled={page >= pages} onClick={() => setPage(page + 1)}>Next →</button>
        </div>
      </div>
    </>
  )
}
