type Props = {
  label: string
  field: string
  sort: string
  order: string
  onSort: (field: string) => void
}

export default function SortableTh({ label, field, sort, order, onSort }: Props) {
  const active = sort === field
  return (
    <th
      className={active ? 'sortable active' : 'sortable'}
      onClick={() => onSort(field)}
      title={`Sort by ${label.toLowerCase()}`}
    >
      {label}
      <span className="sort-arrow">{active && order === 'desc' ? '▼' : '▲'}</span>
    </th>
  )
}
