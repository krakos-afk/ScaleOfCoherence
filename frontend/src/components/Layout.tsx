import type { ReactNode } from 'react'

export type Item = { key: string; label: string }

type Props = {
  items: Item[]
  bottom: Item[]
  page: string
  onNavigate: (key: string) => void
  children: ReactNode
}

export default function Layout({ items, bottom, page, onNavigate, children }: Props) {
  const link = (i: Item) => (
    <button key={i.key} className={page === i.key ? 'active' : ''} onClick={() => onNavigate(i.key)}>
      {i.label}
    </button>
  )

  return (
    <div className="layout">
      <main className="content">{children}</main>
      <nav className="sidebar">
        {items.map(link)}
        <div className="spacer" />
        {bottom.map(link)}
      </nav>
    </div>
  )
}
