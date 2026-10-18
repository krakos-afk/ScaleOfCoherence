import { useEffect, useState, useCallback } from 'react'
import { request, canWrite } from '../api/client'
import ItemForm, { type ItemData, type ItemFormValues, type Workflow, type Stage } from '../components/ItemForm'
import ItemDetail from '../components/ItemDetail'

const LIMIT = 10

// ─── Priority badge ───────────────────────────────────────────────────────────

function PriorityBadge({ priority }: { priority: string }) {
  const cls =
    priority === 'urgent' ? 'badge-urgent'
    : priority === 'high' ? 'badge-high'
    : priority === 'medium' ? 'badge-medium'
    : 'badge-low'
  return (
    <span className={`badge ${cls}`} style={{ textTransform: 'capitalize' }}>
      {priority}
    </span>
  )
}

function parseItem(item: ItemData): ItemData {
  return {
    ...item,
    custom_fields: (typeof item.custom_fields === 'string'
      ? JSON.parse(item.custom_fields as string)
      : item.custom_fields) ?? {},
  }
}

type FormMode = 'closed' | 'new' | ItemData
type ListTab = 'all' | 'my'

// ─── Items page ───────────────────────────────────────────────────────────────

export default function Items() {
  const write = canWrite()

  // List state
  const [tab, setTab] = useState<ListTab>('all')
  const [rows, setRows] = useState<ItemData[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [listError, setListError] = useState('')

  // Workflows/stages for form + display
  const [workflows, setWorkflows] = useState<Workflow[]>([])
  const [stages, setStages] = useState<Stage[]>([])

  // Detail drawer
  const [detailId, setDetailId] = useState<string | null>(null)

  // Form state
  const [formMode, setFormMode] = useState<FormMode>('closed')
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)

  // tick to force reload
  const [tick, setTick] = useState(0)
  const reload = useCallback(() => setTick((t) => t + 1), [])

  // Load workflows + stages once
  useEffect(() => {
    request<{ data: { id: string; name: string }[] }>('GET', '/workflows?page=1&limit=100')
      .then(async (res) => {
        setWorkflows(res.data)
        const allStages = await Promise.all(
          res.data.map(async (w) => {
            const s = await request<{ id: string; name: string }[]>('GET', `/workflows/${w.id}/stages`)
            return s.map((stage) => ({
              id: stage.id,
              name: stage.name,
              workflowId: w.id,
              workflowName: w.name,
            }))
          }),
        )
        setStages(allStages.flat())
      })
      .catch(() => {/* non-fatal */})
  }, [])

  // Load items
  useEffect(() => {
    const path = tab === 'my'
      ? `/items/my?page=${page}&limit=${LIMIT}`
      : `/items?page=${page}&limit=${LIMIT}`

    request<{ data: ItemData[]; total: number }>('GET', path)
      .then((res) => {
        setRows(res.data.map(parseItem))
        setTotal(res.total)
        setListError('')
      })
      .catch((err: Error) => setListError(err.message))
  }, [tab, page, tick])

  // Reset to page 1 when tab changes
  const switchTab = (t: ListTab) => {
    setTab(t)
    setPage(1)
  }

  const pages = Math.max(1, Math.ceil(total / LIMIT))

  const openCreate = () => {
    setFormError('')
    setFormMode('new')
  }

  const openEdit = (item: ItemData) => {
    setFormError('')
    setFormMode(item)
  }

  const closeForm = () => {
    setFormMode('closed')
    setFormError('')
  }

  const handleSubmit = async (values: ItemFormValues) => {
    setSaving(true)
    setFormError('')

    const isNew = formMode === 'new'

    const body: Record<string, unknown> = {
      title: values.title,
      description: values.description,
      priority: values.priority,
      workflow_id: values.workflow_id || null,
      current_stage_id: values.current_stage_id || null,
      custom_fields: values.custom_fields,
    }

    if (!isNew && typeof formMode === 'object') {
      body.version = formMode.version
      if (values.close !== null) {
        body.close = values.close
      }
    }

    try {
      if (isNew) {
        await request('POST', '/items', body)
      } else if (typeof formMode === 'object') {
        await request('PUT', `/items/${formMode.id}`, body)
      }
      closeForm()
      reload()
    } catch (err) {
      setFormError((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async (id: string) => {
    if (!confirm('Are you sure you want to delete this item?')) return
    try {
      await request('DELETE', `/items/${id}`)
      if (rows.length === 1 && page > 1) setPage(page - 1)
      else reload()
    } catch (err) {
      setListError((err as Error).message)
    }
  }

  // Helper: resolve stage name from id
  const stageName = (id: string | null) => {
    if (!id) return '—'
    const s = stages.find((s) => s.id === id)
    if (!s) return id.slice(0, 8) + '…'
    return `${s.workflowName} › ${s.name}`
  }

  // Custom fields summary for table display
  const cfSummary = (cf: Record<string, string>) => {
    const keys = Object.keys(cf)
    if (keys.length === 0) return '—'
    if (keys.length <= 2) return keys.map((k) => `${k}: ${cf[k]}`).join(', ')
    return `${keys.length} fields`
  }

  const formItem: ItemData | null = typeof formMode === 'object' ? formMode : null

  return (
    <>
      <div className="page-header">
        <div>
          <h2>Items</h2>
          <div className="page-meta">{total} record{total !== 1 ? 's' : ''}</div>
        </div>
        {write && (
          <button className="btn btn-primary" onClick={openCreate}>
            + Add Item
          </button>
        )}
      </div>

      {listError && <div className="alert alert-error">{listError}</div>}

      {/* Tabs */}
      <div style={{ display: 'flex', gap: 0, marginBottom: 16, borderBottom: '1px solid var(--border)' }}>
        {(['all', 'my'] as const).map((t) => (
          <button
            key={t}
            onClick={() => switchTab(t)}
            style={{
              background: 'none', border: 'none', cursor: 'pointer',
              padding: '8px 20px', fontSize: 13, fontWeight: tab === t ? 600 : 400,
              color: tab === t ? 'var(--accent)' : 'var(--text-muted)',
              borderBottom: tab === t ? '2px solid var(--accent)' : '2px solid transparent',
              marginBottom: -1,
            }}
          >
            {t === 'all' ? 'All Items' : 'My Work'}
          </button>
        ))}
      </div>

      {/* Item form modal */}
      {formMode !== 'closed' && (
        <ItemForm
          item={formItem}
          workflows={workflows}
          stages={stages}
          saving={saving}
          error={formError}
          onSubmit={handleSubmit}
          onCancel={closeForm}
        />
      )}

      {/* Item detail drawer */}
      {detailId && (
        <ItemDetail
          itemId={detailId}
          stages={stages}
          onClose={() => setDetailId(null)}
          onItemUpdated={reload}
        />
      )}

      <div className="card">
        <table>
          <thead>
            <tr>
              <th>Title</th>
              <th>Priority</th>
              <th>Stage</th>
              <th>Status</th>
              <th>Custom Fields</th>
              <th>Version</th>
              <th>Created</th>
              <th style={{ width: 1 }} />
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr
                key={r.id}
                style={{ cursor: 'pointer', ...(r.closed_at ? { opacity: 0.6 } : {}) }}
                onClick={() => setDetailId(r.id)}
              >
                <td style={{ fontWeight: 500, maxWidth: 200 }}>
                  <span title={r.title} style={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {r.title}
                  </span>
                  {r.description && (
                    <span style={{ fontSize: 12, color: 'var(--text-muted)', display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {r.description}
                    </span>
                  )}
                </td>
                <td onClick={(e) => e.stopPropagation()}><PriorityBadge priority={r.priority} /></td>
                <td className="td-muted" style={{ fontSize: 12 }}>{stageName(r.current_stage_id)}</td>
                <td>
                  {r.closed_at ? (
                    <span className="badge badge-low">Closed</span>
                  ) : (
                    <span className="badge badge-medium">Open</span>
                  )}
                </td>
                <td className="td-muted" style={{ fontSize: 12 }}>{cfSummary(r.custom_fields)}</td>
                <td className="td-muted" style={{ fontSize: 12 }}>v{r.version}</td>
                <td className="td-muted">{new Date(r.created_at).toLocaleDateString()}</td>
                <td onClick={(e) => e.stopPropagation()}>
                  <div className="td-actions">
                    {write && (
                      <>
                        <button
                          className="btn btn-ghost"
                          style={{ fontSize: 12, padding: '4px 10px' }}
                          onClick={() => openEdit(r)}
                        >
                          Edit
                        </button>
                        <button
                          className="btn btn-danger"
                          style={{ fontSize: 12, padding: '4px 10px' }}
                          onClick={() => handleDelete(r.id)}
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
                <td colSpan={8} style={{ textAlign: 'center', color: 'var(--text-muted)', padding: '32px' }}>
                  {tab === 'my' ? 'No items are currently assigned to you.' : 'No items yet.'}
                </td>
              </tr>
            )}
          </tbody>
        </table>
        <div className="pagination">
          <button
            className="btn btn-ghost"
            style={{ padding: '4px 10px', fontSize: 12 }}
            disabled={page <= 1}
            onClick={() => setPage(page - 1)}
          >
            ← Prev
          </button>
          <span>Page {page} of {pages}</span>
          <button
            className="btn btn-ghost"
            style={{ padding: '4px 10px', fontSize: 12 }}
            disabled={page >= pages}
            onClick={() => setPage(page + 1)}
          >
            Next →
          </button>
        </div>
      </div>
    </>
  )
}
