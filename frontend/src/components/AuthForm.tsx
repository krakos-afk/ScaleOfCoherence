import { useState, type FormEvent } from 'react'
import { request, setToken } from '../api/client'

const fields = {
  login: [
    ['email', 'Email', 'email'],
    ['password', 'Password', 'password'],
  ],
  signup: [
    ['org_name', 'Organization name', 'text'],
    ['username', 'Username', 'text'],
    ['email', 'Email', 'email'],
    ['password', 'Password (min 8 chars)', 'password'],
  ],
}

export default function AuthForm({ mode, onDone }: { mode: 'login' | 'signup'; onDone: () => void }) {
  const [values, setValues] = useState<Record<string, string>>({})
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setLoading(true)
    setError('')
    try {
      const { token } = await request<{ token: string }>('POST', `/auth/${mode}`, values)
      setToken(token)
      onDone()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <form onSubmit={submit} style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div>
        <h2>{mode === 'login' ? 'Welcome back' : 'Create your account'}</h2>
        <p className="auth-sub">{mode === 'login' ? 'Sign in to continue' : 'Get started for free'}</p>
      </div>
      {error && <div className="alert alert-error">{error}</div>}
      {fields[mode].map(([name, label, type]) => (
        <div className="form-group" key={name}>
          <label>{label}</label>
          <input
            name={name}
            type={type}
            placeholder={label}
            required
            onChange={(e) => setValues({ ...values, [name]: e.target.value })}
          />
        </div>
      ))}
      <button className="btn btn-primary" type="submit" disabled={loading} style={{ marginTop: 4 }}>
        {loading ? 'Please wait…' : mode === 'login' ? 'Sign in' : 'Create account'}
      </button>
    </form>
  )
}
