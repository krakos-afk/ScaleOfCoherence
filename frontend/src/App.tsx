import { useState } from 'react'
import { getToken, setToken } from './api/client'
import AuthForm from './components/AuthForm'
import DemoLogin from './components/DemoLogin'
import Home from './pages/Home'

export default function App() {
  const [authed, setAuthed] = useState(!!getToken())
  const [mode, setMode] = useState<'login' | 'signup'>('login')

  if (authed) return <Home onLogout={() => { setToken(null); setAuthed(false) }} />

  return (
    <div className="auth-page">
      <div className="auth-shell">
        <div className="auth-card">
          <AuthForm key={mode} mode={mode} onDone={() => setAuthed(true)} />
          <div className="auth-toggle">
            {mode === 'login' ? (
              <>
                New organisation?{' '}
                <button onClick={() => setMode('signup')}>Create account</button>
              </>
            ) : (
              <>
                Already have an account?{' '}
                <button onClick={() => setMode('login')}>Sign in</button>
              </>
            )}
          </div>
          {mode === 'login' && (
            <div style={{ marginTop: 8, fontSize: 12, color: 'var(--text-muted)', textAlign: 'center' }}>
              Team members are added by their organisation admin.
            </div>
          )}
        </div>
        {mode === 'login' && <DemoLogin onDone={() => setAuthed(true)} />}
      </div>
    </div>
  )
}
