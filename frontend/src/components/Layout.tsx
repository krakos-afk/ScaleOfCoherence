import type { ReactNode } from 'react'

export type Item = { key: string; label: string; icon?: string }

type Props = {
  items: Item[]
  bottom: Item[]
  page: string
  onNavigate: (key: string) => void
  children: ReactNode
}

export default function Layout({ items, bottom, page, onNavigate, children }: Props) {
  const link = (i: Item) => (
    <button
      key={i.key}
      className={`sidebar-link ${page === i.key ? 'active' : ''}`}
      onClick={() => onNavigate(i.key)}
    >
      <span className="nav-dot" />
      {i.label}
    </button>
  )

  return (
    <div className="layout">
      <nav className="sidebar">
        <div className="sidebar-brand">
          <h1>Scale</h1>
          <p>of Coherence</p>
        </div>
        <div className="sidebar-nav">
          {items.map(link)}
        </div>
        <div className="sidebar-bottom">
          {bottom.map(link)}
        </div>
      </nav>
      <main className="content">{children}</main>
    </div>
  )
}
