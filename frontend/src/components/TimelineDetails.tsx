import type { ReactNode } from 'react'

export type TimelineUserRef = { id: string; username: string }

export type TimelineActivityState = {
  id: number
  type?: string
  stage_id: string
  team_id: string
  team_name: string
  assigned_user_id: string | null
  assigned_user?: TimelineUserRef
  completion_note: string | null
  approval_status: string | null
  approval_note: string | null
}

export type TimelineEntry = {
  version: number
  created_at: string
  actor: TimelineUserRef
  type: string
  changes: Record<string, unknown>
  activity?: TimelineActivityState
}

type StageRef = {
  id: string
  name: string
  workflowId: string
  workflowName: string
}

type Props = {
  entry: TimelineEntry
  stages: StageRef[]
  resolveUser: (id: string | null | undefined) => string
  stageName: (id: string | null, full?: boolean) => string
}

type ChangeRow = {
  key: string
  label: string
  from?: ReactNode
  to: ReactNode
}

const META_KEYS = new Set([
  'type',
  'activity',
  'activity_id',
  'closed_activity_id',
  'from',
  'to',
  'closed',
  'custom_fields',
  'stage_id',
])

const FIELD_ORDER = ['title', 'description', 'priority', 'workflow_id', 'closed_at']

const FIELD_LABELS: Record<string, string> = {
  title: 'Title',
  description: 'Description',
  priority: 'Priority',
  workflow_id: 'Workflow',
  closed_at: 'Status',
  stage_id: 'Stage',
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function scalar(value: unknown): string {
  if (value === null || value === undefined || value === '') return '—'
  if (typeof value === 'boolean') return value ? 'Yes' : 'No'
  if (Array.isArray(value)) {
    return value.length > 0 ? value.map(scalar).join(', ') : '—'
  }
  if (isRecord(value)) {
    const parts = Object.entries(value).map(([key, nested]) => `${key}: ${scalar(nested)}`)
    return parts.length > 0 ? parts.join(', ') : '—'
  }
  return String(value)
}

function fieldLabel(key: string): string {
  if (FIELD_LABELS[key]) return FIELD_LABELS[key]
  const label = key.replaceAll('_', ' ')
  return label.charAt(0).toUpperCase() + label.slice(1)
}

function buildRows(entry: TimelineEntry, stages: StageRef[], resolveUser: Props['resolveUser'], stageName: Props['stageName']): ChangeRow[] {
  const changes = entry.changes ?? {}
  const rows: ChangeRow[] = []

  const workflowName = (value: unknown): string => {
    if (typeof value !== 'string' || !value) return '—'
    const stage = stages.find((s) => s.workflowId === value)
    return stage?.workflowName ?? value.slice(0, 8) + '…'
  }

  const valueFor = (key: string, value: unknown): ReactNode => {
    switch (key) {
      case 'priority':
        return typeof value === 'string' ? value.charAt(0).toUpperCase() + value.slice(1) : scalar(value)
      case 'workflow_id':
        return workflowName(value)
      case 'closed_at':
        return value ? 'Closed' : 'Open'
      case 'stage_id':
        return stageName(typeof value === 'string' ? value : null, false)
      default:
        return scalar(value)
    }
  }

  const addDiff = (key: string, value: unknown) => {
    if (META_KEYS.has(key) || key === 'custom_fields') return
    const label = fieldLabel(key)
    if (Array.isArray(value) && value.length === 2) {
      rows.push({ key, label, from: valueFor(key, value[0]), to: valueFor(key, value[1]) })
    }
  }

  if (entry.type === 'updated' || entry.type === 'stage_moved') {
    for (const key of FIELD_ORDER) {
      if (key in changes) addDiff(key, changes[key])
    }
    for (const [key, value] of Object.entries(changes)) {
      if (!FIELD_ORDER.includes(key)) addDiff(key, value)
    }
  }

  if (Array.isArray(changes.custom_fields) && changes.custom_fields.length === 2) {
    const [before, after] = changes.custom_fields
    const beforeFields = isRecord(before) ? before : {}
    const afterFields = isRecord(after) ? after : {}
    const keys = Array.from(new Set([...Object.keys(beforeFields), ...Object.keys(afterFields)])).sort()
    for (const key of keys) {
      const from = scalar(beforeFields[key])
      const to = scalar(afterFields[key])
      if (from !== to) {
        rows.push({ key: `custom_fields.${key}`, label: `Custom field · ${key}`, from, to })
      }
    }
  }

  if (entry.type === 'stage_moved') {
    const from = typeof changes.from === 'string' ? changes.from : null
    const to = typeof changes.to === 'string' ? changes.to : null
    rows.push({ key: 'stage', label: 'Stage', from: stageName(from, false), to: stageName(to, false) })
  }

  if (entry.type === 'assigned') {
    const from = typeof changes.from === 'string' ? changes.from : null
    const to = typeof changes.to === 'string' ? changes.to : entry.activity?.assigned_user_id ?? null
    rows.push({ key: 'assignee', label: 'Assignee', from: resolveUser(from), to: resolveUser(to) })
  }

  if (entry.type === 'created' && entry.activity) {
    if (entry.activity.stage_id) {
      rows.push({ key: 'created-stage', label: 'Stage', to: stageName(entry.activity.stage_id, false) })
    }
    if (entry.activity.team_name) {
      rows.push({ key: 'created-team', label: 'Team', to: entry.activity.team_name })
    }
  }

  if (entry.type === 'completed' && entry.activity?.completion_note) {
    rows.push({ key: 'completion-note', label: 'Note', to: entry.activity.completion_note })
  }

  if (entry.type === 'approved' || entry.type === 'rejected') {
    if (entry.activity?.approval_note) {
      rows.push({ key: 'approval-note', label: 'Review', to: entry.activity.approval_note })
    }
    if (entry.type === 'rejected' && entry.activity?.completion_note) {
      rows.push({ key: 'rejected-note', label: 'Completion note', to: entry.activity.completion_note })
    }
  }

  if (entry.type === 'closed') {
    rows.push({
      key: 'status',
      label: 'Status',
      to: changes.closed === true ? 'Closed' : 'Reopened',
    })
  }

  return rows
}

function ChangeRowView({ row }: { row: ChangeRow }) {
  return (
    <div style={{ display: 'flex', gap: 8 }}>
      <span style={{ color: 'var(--text-muted)', minWidth: 96, flexShrink: 0 }}>{row.label}</span>
      <span style={{ wordBreak: 'break-word', whiteSpace: 'pre-wrap' }}>
        {row.from !== undefined && (
          <>
            {row.from} <span style={{ color: 'var(--text-muted)' }}>→</span>{' '}
          </>
        )}
        {row.to}
      </span>
    </div>
  )
}

export function TimelineDetails({ entry, stages, resolveUser, stageName }: Props) {
  const rows = buildRows(entry, stages, resolveUser, stageName)
  if (rows.length === 0) {
    return <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>No additional detail.</div>
  }
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 3, fontSize: 12 }}>
      {rows.map((row) => <ChangeRowView key={row.key} row={row} />)}
    </div>
  )
}
