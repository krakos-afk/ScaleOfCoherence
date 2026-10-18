import { useState } from 'react'

// ─── Types ────────────────────────────────────────────────────────────────────

export type Workflow = { id: string; name: string }
export type Stage = { id: string; name: string; workflowId: string; workflowName: string; approver_user_id?: string | null; position?: number; assigned_team_id?: string | null }

export type ItemData = {
  id: string
  title: string
  description: string
  priority: string
  workflow_id: string | null
  current_stage_id: string | null
  custom_fields: Record<string, string>
  closed_at: string | null
  version: number
  created_at: string
  created_by: string
}

export type ItemFormValues = {
  title: string
  description: string
  priority: string
  workflow_id: string
  current_stage_id: string
  custom_fields: Record<string, string>
  close: boolean | null  // null = leave unchanged
}

type KVPair = { key: string; value: string }

type Props = {
  /** Null means "create new" */
  item: ItemData | null
  workflows: Workflow[]
  stages: Stage[]
  /** Only admins may change the closed/reopen state from this form. */
  canClose: boolean
  saving: boolean
  error: string
  onSubmit: (values: ItemFormValues) => void
  onCancel: () => void
}

const PRIORITIES = [
  { value: 'medium', label: 'Medium' },
  { value: 'urgent', label: 'Urgent' },
  { value: 'high', label: 'High' },
  { value: 'low', label: 'Low' },
]

function kvFromRecord(r: Record<string, string>): KVPair[] {
  return Object.entries(r).map(([key, value]) => ({ key, value }))
}

function kvToRecord(pairs: KVPair[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const { key, value } of pairs) {
    const k = key.trim()
    if (k) out[k] = value
  }
  return out
}

// ─── Component ────────────────────────────────────────────────────────────────

export default function ItemForm({ item, workflows, stages, canClose, saving, error, onSubmit, onCancel }: Props) {
  const isNew = item === null

  // Derive initial workflowId from item's workflow_id or stage's workflow
  const initWorkflowId = item?.workflow_id ?? ''
  const initStageId = item?.current_stage_id ?? ''

  const [title, setTitle] = useState(item?.title ?? '')
  const [description, setDescription] = useState(item?.description ?? '')
  const [priority, setPriority] = useState(item?.priority ?? 'medium')
  const [workflowId, setWorkflowId] = useState(initWorkflowId)
  const [stageId, setStageId] = useState(initStageId)
  const [kvPairs, setKvPairs] = useState<KVPair[]>(
    item?.custom_fields && Object.keys(item.custom_fields).length > 0
      ? kvFromRecord(item.custom_fields)
      : []
  )
  // close toggle: null (for edit = leave unchanged), true = close, false = reopen
  const [closeAction, setCloseAction] = useState<'leave' | 'close' | 'reopen'>('leave')

  const isClosed = !!item?.closed_at

  // Stages filtered to selected workflow
  const filteredStages = workflowId
    ? stages.filter((s) => s.workflowId === workflowId)
    : stages

  const handleWorkflowChange = (wid: string) => {
    setWorkflowId(wid)
    // Reset stage if it doesn't belong to new workflow
    if (wid && stageId) {
      const belongs = stages.some((s) => s.id === stageId && s.workflowId === wid)
      if (!belongs) setStageId('')
    }
  }

  // KV helpers
  const addKv = () => setKvPairs([...kvPairs, { key: '', value: '' }])
  const removeKv = (i: number) => setKvPairs(kvPairs.filter((_, idx) => idx !== i))
  const updateKv = (i: number, field: 'key' | 'value', val: string) =>
    setKvPairs(kvPairs.map((p, idx) => (idx === i ? { ...p, [field]: val } : p)))

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    let close: boolean | null = null
    if (!isNew) {
      if (closeAction === 'close') close = true
      else if (closeAction === 'reopen') close = false
    }
    onSubmit({
      title: title.trim(),
      description: description.trim(),
      priority,
      workflow_id: workflowId,
      current_stage_id: stageId,
      custom_fields: kvToRecord(kvPairs),
      close,
    })
  }

  return (
    <div className="modal-backdrop" onClick={(e) => e.target === e.currentTarget && onCancel()}>
      <div className="modal" style={{ maxWidth: 560, width: '100%' }}>
        <div className="modal-header">
          <h3>{isNew ? 'New Item' : 'Edit Item'}</h3>
          {!isNew && item && (
            <span className="badge badge-version">v{item.version}</span>
          )}
        </div>

        {error && <div className="alert alert-error" style={{ margin: '0 24px 0' }}>{error}</div>}

        <form onSubmit={handleSubmit}>
          <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>

            {/* Title */}
            <div className="form-group">
              <label>Title <span style={{ color: 'var(--danger)' }}>*</span></label>
              <input
                placeholder="Item title"
                required
                value={title}
                onChange={(e) => setTitle(e.target.value)}
              />
            </div>

            {/* Description */}
            <div className="form-group">
              <label>Description</label>
              <textarea
                rows={3}
                placeholder="Describe the item…"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                style={{ resize: 'vertical' }}
              />
            </div>

            {/* Priority */}
            <div className="form-group">
              <label>Priority</label>
              <select value={priority} onChange={(e) => setPriority(e.target.value)}>
                {PRIORITIES.map((p) => (
                  <option key={p.value} value={p.value}>{p.label}</option>
                ))}
              </select>
            </div>

            {/* Workflow → Stage (cascading) */}
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              <div className="form-group" style={{ marginBottom: 0 }}>
                <label>Workflow</label>
                <select value={workflowId} onChange={(e) => handleWorkflowChange(e.target.value)}>
                  <option value="">— none —</option>
                  {workflows.map((w) => (
                    <option key={w.id} value={w.id}>{w.name}</option>
                  ))}
                </select>
              </div>
              <div className="form-group" style={{ marginBottom: 0 }}>
                <label>Stage</label>
                <select
                  value={stageId}
                  onChange={(e) => setStageId(e.target.value)}
                  disabled={!workflowId && filteredStages.length === 0}
                >
                  <option value="">— none —</option>
                  {filteredStages.map((s) => (
                    <option key={s.id} value={s.id}>{s.name}</option>
                  ))}
                </select>
              </div>
            </div>

            {/* Closed status — only on edit, and only for admins */}
            {!isNew && canClose && (
              <div className="form-group">
                <label>Status</label>
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                  {isClosed ? (
                    <>
                      <span className="badge badge-low" style={{ alignSelf: 'center' }}>Closed</span>
                      <button
                        type="button"
                        className={`btn ${closeAction === 'reopen' ? 'btn-primary' : 'btn-ghost'}`}
                        style={{ fontSize: 12, padding: '4px 12px' }}
                        onClick={() => setCloseAction(closeAction === 'reopen' ? 'leave' : 'reopen')}
                      >
                        {closeAction === 'reopen' ? '✓ Will reopen' : 'Reopen'}
                      </button>
                    </>
                  ) : (
                    <>
                      <span className="badge badge-medium" style={{ alignSelf: 'center' }}>Open</span>
                      <button
                        type="button"
                        className={`btn ${closeAction === 'close' ? 'btn-danger' : 'btn-ghost'}`}
                        style={{ fontSize: 12, padding: '4px 12px' }}
                        onClick={() => setCloseAction(closeAction === 'close' ? 'leave' : 'close')}
                      >
                        {closeAction === 'close' ? '✓ Will close' : 'Close item'}
                      </button>
                    </>
                  )}
                </div>
              </div>
            )}

            {/* Custom Fields */}
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label style={{ marginBottom: 6 }}>
                Custom Fields
                <button
                  type="button"
                  className="btn btn-ghost"
                  style={{ fontSize: 11, padding: '2px 8px', marginLeft: 8 }}
                  onClick={addKv}
                >
                  + Add field
                </button>
              </label>
              {kvPairs.length === 0 && (
                <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                  No custom fields yet. Click "+ Add field" to add one.
                </div>
              )}
              {kvPairs.map((pair, i) => (
                <div key={i} style={{ display: 'grid', gridTemplateColumns: '1fr 1fr auto', gap: 8, marginBottom: 6 }}>
                  <input
                    placeholder="Field name"
                    value={pair.key}
                    onChange={(e) => updateKv(i, 'key', e.target.value)}
                    style={{ fontSize: 13 }}
                  />
                  <input
                    placeholder="Value"
                    value={pair.value}
                    onChange={(e) => updateKv(i, 'value', e.target.value)}
                    style={{ fontSize: 13 }}
                  />
                  <button
                    type="button"
                    className="btn btn-ghost"
                    style={{ fontSize: 12, padding: '4px 8px', color: 'var(--danger)' }}
                    onClick={() => removeKv(i)}
                    aria-label="Remove field"
                  >
                    ✕
                  </button>
                </div>
              ))}
            </div>

          </div>

          <div className="modal-footer">
            <button type="button" className="btn btn-ghost" onClick={onCancel} disabled={saving}>
              Cancel
            </button>
            <button type="submit" className="btn btn-primary" disabled={saving}>
              {saving ? 'Saving…' : isNew ? 'Create item' : 'Save changes'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
