import { useEffect, useState } from 'react'
import { request } from '../api/client'

type Row = { id: string } & Record<string, string>
export type Field = { key: string; label: string; options?: string[]; required?: boolean }

const LIMIT = 10

type Props = { title: string; path: string; fields: Field[]; manageLabel?: string; onManage?: (row: { id: string; name: string }) => void }

export default function CrudTable({ title, path, fields, manageLabel = 'Members', onManage }: Props) {
  const [rows, setRows] = useState<Row[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [form, setForm] = useState<Partial<Row> | null>(null)
  const [error, setError] = useState('')

  const [tick, setTick] = useState(0)
  const reload = () => setTick((t) => t + 1)

  useEffect(() => {
    request<{ data: Row[]; total: number }>('GET', `${path}?page=${page}&limit=${LIMIT}`)
      .then((res) => {
        setRows(res.data)
        setTotal(res.total)
        setError('')
      })
      .catch((err: Error) => setError(err.message))
  }, [path, page, tick])

  const pages = Math.max(1, Math.ceil(total / LIMIT))

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    const body = Object.fromEntries(fields.map((f) => [f.key, form?.[f.key] ?? f.options?.[0] ?? '']))
    try {
      await (form?.id ? request('PUT', `${path}/${form.id}`, body) : request('POST', path, body))
      setForm(null)
      reload()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const remove = async (id: string) => {
    if (!confirm('Delete?')) return
    try {
      await request('DELETE', `${path}/${id}`)
      if (rows.length === 1 && page > 1) setPage(page - 1)
      else reload()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  return (
    <>
      <h2>{title}</h2>
      <button onClick={() => setForm({})}>Add</button>
      {error && <div>{error}</div>}
      {form && (
        <form onSubmit={save}>
          {fields.map((f) =>
            f.options ? (
              <select key={f.key} value={form[f.key] ?? f.options[0]} onChange={(e) => setForm({ ...form, [f.key]: e.target.value })}>
                {f.options.map((o) => <option key={o}>{o}</option>)}
              </select>
            ) : (
              <input key={f.key} placeholder={f.label} required={f.required} value={form[f.key] ?? ''}
                onChange={(e) => setForm({ ...form, [f.key]: e.target.value })} />
            ),
          )}
          <button type="submit">{form.id ? 'Update' : 'Create'}</button>
          <button type="button" onClick={() => setForm(null)}>Cancel</button>
        </form>
      )}
      <table>
        <thead>
          <tr>
            {fields.map((f) => <th key={f.key}>{f.label}</th>)}
            <th>Created</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.id}>
              {fields.map((f) => <td key={f.key}>{r[f.key]}</td>)}
              <td>{new Date(r.created_at).toLocaleString()}</td>
              <td>
                {onManage && <button onClick={() => onManage({ id: r.id, name: r.name })}>{manageLabel}</button>}
                <button onClick={() => setForm(r)}>Edit</button>
                <button onClick={() => remove(r.id)}>Delete</button>
              </td>
            </tr>
          ))}
          {rows.length === 0 && <tr><td colSpan={fields.length + 2}>No records</td></tr>}
        </tbody>
      </table>
      <p>
        <button disabled={page <= 1} onClick={() => setPage(page - 1)}>Prev</button>
        {' '}Page {page} of {pages}{' '}
        <button disabled={page >= pages} onClick={() => setPage(page + 1)}>Next</button>
      </p>
    </>
  )
}
