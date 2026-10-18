import { useEffect, useState, type FormEvent } from 'react'
import { request, userId as getMyId } from '../api/client'
import SortableTh from '../components/SortableTh'
import { useDebouncedValue } from '../hooks/useDebouncedValue'
import { useSort } from '../hooks/useSort'

type OrgUser = { id: string; username: string; usertype: string }

const USERTYPES = ['admin', 'user'] as const

export default function Users() {
  const [users, setUsers] = useState<OrgUser[]>([])
  const [error, setError] = useState('')
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ email: '', username: '', password: '', usertype: 'user' })
  const [saving, setSaving] = useState(false)
  const [tick, setTick] = useState(0)
  const [q, setQ] = useState('')
  const search = useDebouncedValue(q).trim()
  const { sort, order, toggle } = useSort('username', 'asc')
  const myId = getMyId()

  const reload = () => setTick((t) => t + 1)

  useEffect(() => {
    const params = new URLSearchParams({ sort, order })
    if (search) params.set('q', search)
    request<OrgUser[]>('GET', `/auth/users?${params}`)
      .then((u) => { setUsers(u); setError('') })
      .catch((err: Error) => setError(err.message))
  }, [tick, search, sort, order])

  const createUser = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    setError('')
    try {
      await request('POST', '/auth/users', form)
      setForm({ email: '', username: '', password: '', usertype: 'user' })
      setShowForm(false)
      reload()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const deleteUser = async (id: string, username: string) => {
    if (!confirm(`Delete user "${username}"? This cannot be undone.`)) return
    try {
      await request('DELETE', `/auth/users/${id}`)
      reload()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const badgeClass = (t: string) => (t === 'admin' ? 'badge-urgent' : 'badge-low')

  return (
    <>
      <div className="page-header">
        <div>
          <h2>Users</h2>
          <div className="page-meta">{users.length} member{users.length !== 1 ? 's' : ''} in this organisation</div>
        </div>
        <button className="btn btn-primary" onClick={() => setShowForm(true)}>
          + Add user
        </button>
      </div>

      {error && <div className="alert alert-error">{error}</div>}

      {/* Create user modal */}
      {showForm && (
        <div className="modal-backdrop" onClick={(e) => e.target === e.currentTarget && setShowForm(false)}>
          <div className="modal">
            <div className="modal-header">
              <h3>New user</h3>
            </div>
            <form onSubmit={createUser}>
              <div className="modal-body">
                <div className="form-group">
                  <label>Email</label>
                  <input
                    type="email"
                    placeholder="user@example.com"
                    required
                    value={form.email}
                    onChange={(e) => setForm({ ...form, email: e.target.value })}
                  />
                </div>
                <div className="form-group">
                  <label>Username</label>
                  <input
                    placeholder="username"
                    required
                    value={form.username}
                    onChange={(e) => setForm({ ...form, username: e.target.value })}
                  />
                </div>
                <div className="form-group">
                  <label>Password (min 8 chars)</label>
                  <input
                    type="password"
                    placeholder="password"
                    required
                    minLength={8}
                    value={form.password}
                    onChange={(e) => setForm({ ...form, password: e.target.value })}
                  />
                </div>
                <div className="form-group">
                  <label>Role</label>
                  <select
                    value={form.usertype}
                    onChange={(e) => setForm({ ...form, usertype: e.target.value })}
                  >
                    {USERTYPES.map((t) => (
                      <option key={t} value={t} style={{ textTransform: 'capitalize' }}>{t}</option>
                    ))}
                  </select>
                </div>
              </div>
              <div className="modal-footer">
                <button type="button" className="btn btn-ghost" onClick={() => setShowForm(false)}>Cancel</button>
                <button type="submit" className="btn btn-primary" disabled={saving}>
                  {saving ? 'Creating…' : 'Create user'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      <div className="list-toolbar">
        <input
          type="search"
          className="search-input"
          placeholder="Search users…"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
      </div>

      <div className="card">
        <table>
          <thead>
            <tr>
              <SortableTh label="Username" field="username" sort={sort} order={order} onSort={toggle} />
              <SortableTh label="Role" field="usertype" sort={sort} order={order} onSort={toggle} />
              <th style={{ width: 1 }} />
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <tr key={u.id}>
                <td>
                  {u.username}
                  {u.id === myId && (
                    <span style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 6 }}>(you)</span>
                  )}
                </td>
                <td>
                  <span className={`badge ${badgeClass(u.usertype)}`} style={{ textTransform: 'capitalize' }}>
                    {u.usertype}
                  </span>
                </td>
                <td>
                  {u.id !== myId && (
                    <div className="td-actions">
                      <button
                        className="btn btn-danger"
                        style={{ fontSize: 12, padding: '4px 10px' }}
                        onClick={() => deleteUser(u.id, u.username)}
                      >
                        Delete
                      </button>
                    </div>
                  )}
                </td>
              </tr>
            ))}
            {users.length === 0 && (
              <tr>
                <td colSpan={3} style={{ textAlign: 'center', color: 'var(--text-muted)', padding: '32px' }}>
                  {search ? 'No matches for your search.' : 'No users yet'}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  )
}
