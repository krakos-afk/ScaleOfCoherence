import { useEffect, useState } from 'react'
import { request } from '../api/client'
import SortableTh from './SortableTh'
import { useDebouncedValue } from '../hooks/useDebouncedValue'
import { useSort } from '../hooks/useSort'

type Row = { id: string; version?: number } & Record<string, unknown>
export type Field = {
  key: string
  label: string
  options?: { value: string; label: string }[]
  required?: boolean
  textarea?: boolean
}

const LIMIT = 10

// Split _version out of the index signature to avoid number-vs-string conflict
type FormState = {
  id?: string
  _version?: number
  fields: Record<string, string | undefined>
}

type Props = {
  title: string
  path: string
  fields: Field[]
  manageLabel?: string
  onManage?: (row: { id: string; name: string }) => void
  /** If true, PUT requests include a `version` field from the row for optimistic locking */
  versioned?: boolean
  /** Hide all create/edit/delete actions — render as read-only list */
  readOnly?: boolean
  /** Field keys that can be sorted by clicking their column header */
  sortableFields?: string[]
}

export default function CrudTable({
  title, path, fields, manageLabel = 'Members', onManage, versioned, readOnly,
  sortableFields = ['name', 'created_at'],
}: Props) {
  const [rows, setRows] = useState<Row[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [form, setForm] = useState<FormState | null>(null)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [q, setQ] = useState('')
  const search = useDebouncedValue(q).trim()
  const { sort, order, toggle } = useSort('name', 'asc')

  const [tick, setTick] = useState(0)
  const reload = () => setTick((t) => t + 1)

  useEffect(() => {
    const params = new URLSearchParams({ page: String(page), limit: String(LIMIT), sort, order })
    if (search) params.set('q', search)
    request<{ data: Row[]; total: number }>('GET', `${path}?${params}`)
      .then((res) => {
        setRows(res.data)
        setTotal(res.total)
        setError('')
      })
      .catch((err: Error) => setError(err.message))
  }, [path, page, tick, search, sort, order])

  const changeSort = (field: string) => {
    toggle(field)
    setPage(1)
  }

  const pages = Math.max(1, Math.ceil(total / LIMIT))

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    const body: Record<string, unknown> = Object.fromEntries(
      fields.map((f) => [f.key, form?.fields[f.key] ?? f.options?.[0]?.value ?? '']),
    )
    // Include version for optimistic locking on update
    if (versioned && form?._version !== undefined) {
      body.version = form._version
    }
    setSaving(true)
    setError('')
    try {
      await (form?.id ? request('PUT', `${path}/${form.id}`, body) : request('POST', path, body))
      setForm(null)
      reload()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const remove = async (id: string) => {
    if (!confirm('Are you sure you want to delete this?')) return
    try {
      await request('DELETE', `${path}/${id}`)
      if (rows.length === 1 && page > 1) setPage(page - 1)
      else reload()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const editRow = (row: Row): FormState => ({
    id: row.id,
    _version: row.version,
    fields: Object.fromEntries(
      fields.map((f) => [f.key, row[f.key] == null ? '' : String(row[f.key])]),
    ),
  })

  const display = (f: Field, row: Row) => {
    const val = row[f.key] == null ? '' : String(row[f.key])
    if (!f.options) return val
    return f.options.find((o) => o.value === val)?.label ?? val
  }

  const isNew = !form?.id

  const fieldVal = (key: string) => form?.fields[key] ?? ''
  const setField = (key: string, val: string) =>
    setForm((f) => f ? { ...f, fields: { ...f.fields, [key]: val } } : f)

  return (
    <>
      <div className="page-header">
        <div>
          <h2>{title}</h2>
          <div className="page-meta">{total} record{total !== 1 ? 's' : ''}</div>
        </div>
        {!readOnly && (
          <button className="btn btn-primary" onClick={() => setForm({ fields: {} })}>
            + Add {title.replace(/s$/, '')}
          </button>
        )}
      </div>

      {error && <div className="alert alert-error">{error}</div>}

      {/* Modal */}
      {form && !readOnly && (
        <div className="modal-backdrop" onClick={(e) => e.target === e.currentTarget && setForm(null)}>
          <div className="modal">
            <div className="modal-header">
              <h3>{isNew ? `New ${title.replace(/s$/, '')}` : `Edit ${title.replace(/s$/, '')}`}</h3>
              {versioned && !isNew && form._version !== undefined && (
                <span className="badge badge-version">v{form._version}</span>
              )}
            </div>
            <form onSubmit={save}>
              <div className="modal-body">
                {fields.map((f) =>
                  f.options ? (
                    <div className="form-group" key={f.key}>
                      <label>{f.label}</label>
                      <select
                        value={fieldVal(f.key) || f.options[0].value}
                        onChange={(e) => setField(f.key, e.target.value)}
                      >
                        {f.options.map((o) => (
                          <option key={o.value} value={o.value}>{o.label}</option>
                        ))}
                      </select>
                    </div>
                  ) : f.textarea ? (
                    <div className="form-group" key={f.key}>
                      <label>{f.label}</label>
                      <textarea
                        rows={3}
                        placeholder={f.label}
                        required={f.required}
                        value={fieldVal(f.key)}
                        onChange={(e) => setField(f.key, e.target.value)}
                        style={{ resize: 'vertical' }}
                      />
                    </div>
                  ) : (
                    <div className="form-group" key={f.key}>
                      <label>{f.label}</label>
                      <input
                        placeholder={f.label}
                        required={f.required}
                        value={fieldVal(f.key)}
                        onChange={(e) => setField(f.key, e.target.value)}
                      />
                    </div>
                  ),
                )}
              </div>
              <div className="modal-footer">
                <button type="button" className="btn btn-ghost" onClick={() => setForm(null)}>
                  Cancel
                </button>
                <button type="submit" className="btn btn-primary" disabled={saving}>
                  {saving ? 'Saving…' : isNew ? 'Create' : 'Save changes'}
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
          placeholder={`Search ${title.toLowerCase()}…`}
          value={q}
          onChange={(e) => { setQ(e.target.value); setPage(1) }}
        />
      </div>

      <div className="card">
        <table>
          <thead>
            <tr>
              {fields.map((f) => sortableFields.includes(f.key) ? (
                <SortableTh key={f.key} label={f.label} field={f.key} sort={sort} order={order} onSort={changeSort} />
              ) : (
                <th key={f.key}>{f.label}</th>
              ))}
              {sortableFields.includes('created_at') ? (
                <SortableTh label="Created" field="created_at" sort={sort} order={order} onSort={changeSort} />
              ) : (
                <th>Created</th>
              )}
              <th style={{ width: 1 }} />
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.id}>
                {fields.map((f) => (
                  <td key={f.key}>{display(f, r)}</td>
                ))}
                <td className="td-muted">{new Date(r.created_at as string).toLocaleDateString()}</td>
                <td>
                  <div className="td-actions">
                    {onManage && (
                      <button
                        className="btn btn-ghost"
                        style={{ fontSize: 12, padding: '4px 10px' }}
                        onClick={() => onManage({ id: r.id, name: r.name as string })}
                      >
                        {manageLabel}
                      </button>
                    )}
                    {!readOnly && (
                      <>
                        <button
                          className="btn btn-ghost"
                          style={{ fontSize: 12, padding: '4px 10px' }}
                          onClick={() => setForm(editRow(r))}
                        >
                          Edit
                        </button>
                        <button
                          className="btn btn-danger"
                          style={{ fontSize: 12, padding: '4px 10px' }}
                          onClick={() => remove(r.id)}
                        >
                          Delete
                        </button>
                      </>
                    )}
                  </div>
                </td>
              </tr>
            ))}
            {rows.length === 0 && (
              <tr>
                <td colSpan={fields.length + 2} style={{ textAlign: 'center', color: 'var(--text-muted)', padding: '32px' }}>
                  {search ? 'No matches for your search.' : 'No records yet'}
                </td>
              </tr>
            )}
          </tbody>
        </table>
        <div className="pagination">
          <button className="btn btn-ghost" style={{ padding: '4px 10px', fontSize: 12 }} disabled={page <= 1} onClick={() => setPage(page - 1)}>
            ← Prev
          </button>
          <span>Page {page} of {pages}</span>
          <button className="btn btn-ghost" style={{ padding: '4px 10px', fontSize: 12 }} disabled={page >= pages} onClick={() => setPage(page + 1)}>
            Next →
          </button>
        </div>
      </div>
    </>
  )
}
