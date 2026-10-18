import { useState } from 'react'

export type SortOrder = 'asc' | 'desc'

// Fields that read best newest-first when clicked for the first time.
const DEFAULT_DESC = new Set(['created_at', 'updated_at', 'version'])

export function useSort(defaultField: string, defaultOrder: SortOrder = 'asc') {
  const [sort, setSort] = useState(defaultField)
  const [order, setOrder] = useState<SortOrder>(defaultOrder)

  // toggle sorts by field; clicking the active column flips the direction.
  const toggle = (field: string) => {
    if (field === sort) {
      setOrder((o) => (o === 'asc' ? 'desc' : 'asc'))
    } else {
      setSort(field)
      setOrder(DEFAULT_DESC.has(field) ? 'desc' : 'asc')
    }
  }

  return { sort, order, toggle }
}
