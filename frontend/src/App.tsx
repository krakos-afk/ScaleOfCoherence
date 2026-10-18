import { useState } from 'react'
import { getToken, setToken } from './api/client'
import AuthForm from './components/AuthForm'
import Home from './pages/Home'

export default function App() {
  const [authed, setAuthed] = useState(!!getToken())
  const [mode, setMode] = useState<'login' | 'signup'>('login')

  if (authed) return <Home onLogout={() => { setToken(null); setAuthed(false) }} />

  return (
    <div className="auth">
      <AuthForm key={mode} mode={mode} onDone={() => setAuthed(true)} />
      <p>
        <button onClick={() => setMode(mode === 'login' ? 'signup' : 'login')}>
          {mode === 'login' ? 'Need an account? Sign up' : 'Have an account? Log in'}
        </button>
      </p>
    </div>
  )
}
