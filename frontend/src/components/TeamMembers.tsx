import { useEffect, useState } from 'react'
import { request } from '../api/client'

const LIMIT = 10

type Member = { user_id: string; username: string; role: string }
type OrgUser = { id: string; username: string }

export default function TeamMembers({ team, onBack }: { team: { id: string; name: string }; onBack: () => void }) {
  const [members, setMembers] = useState<Member[]>([])
  const [users, setUsers] = useState<OrgUser[]>([])
  const [userId, setUserId] = useState('')
  const [role, setRole] = useState('viewer')
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [tick, setTick] = useState(0)
  const [error, setError] = useState('')
  const base = `/teams/${team.id}/members`
  const reload = () => setTick((t) => t + 1)

  useEffect(() => {
    Promise.all([request<{ data: Member[]; total: number }>('GET', `${base}?page=${page}&limit=${LIMIT}`), request<OrgUser[]>('GET', '/auth/users')])
      .then(([m, u]) => {
        setMembers(m.data)
        setTotal(m.total)
        setUsers(u)
        setError('')
      })
      .catch((err: Error) => setError(err.message))
  }, [base, page, tick])

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
      <button onClick={onBack}>Back</button>
      <h2>{team.name} members</h2>
      {error && <div>{error}</div>}
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void run(() => request('POST', base, { user_id: selected, role }))
        }}
      >
        <select value={selected} onChange={(e) => setUserId(e.target.value)}>
          {users.map((u) => <option key={u.id} value={u.id}>{u.username}</option>)}
        </select>
        <select value={role} onChange={(e) => setRole(e.target.value)}>
          <option>viewer</option>
          <option>editor</option>
        </select>
        <button type="submit" disabled={!selected}>Add member</button>
      </form>
      <table>
        <thead>
          <tr><th>Username</th><th>Role</th><th /></tr>
        </thead>
        <tbody>
          {members.map((m) => (
            <tr key={m.user_id}>
              <td>{m.username}</td>
              <td>{m.role}</td>
              <td><button onClick={() => run(() => request('DELETE', `${base}/${m.user_id}`))}>Remove</button></td>
            </tr>
          ))}
          {members.length === 0 && <tr><td colSpan={3}>No members</td></tr>}
        </tbody>
      </table>
      <div>
        <button disabled={page <= 1} onClick={() => setPage(page - 1)}>Prev</button>
        {' '}Page {page} of {pages}{' '}
        <button disabled={page >= pages} onClick={() => setPage(page + 1)}>Next</button>
      </div>
    </>
  )
}
