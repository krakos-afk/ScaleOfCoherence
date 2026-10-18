import { useState, type FormEvent } from 'react'
import { request, setToken } from '../api/client'

const fields = {
  login: [['email', 'Email', 'email'], ['password', 'Password', 'password']],
  signup: [['org_name', 'Organization', 'text'], ['username', 'Username', 'text'], ['email', 'Email', 'email'], ['password', 'Password (min 8)', 'password']],
}

export default function AuthForm({ mode, onDone }: { mode: 'login' | 'signup'; onDone: () => void }) {
  const [values, setValues] = useState<Record<string, string>>({})
  const [error, setError] = useState('')

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    try {
      const { token } = await request<{ token: string }>('POST', `/auth/${mode}`, values)
      setToken(token)
      onDone()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  return (
    <form onSubmit={submit}>
      <h2>{mode === 'login' ? 'Log in' : 'Sign up'}</h2>
      {fields[mode].map(([name, label, type]) => (
        <input key={name} name={name} type={type} placeholder={label} required
          onChange={(e) => setValues({ ...values, [name]: e.target.value })} />
      ))}
      <button type="submit">{mode === 'login' ? 'Log in' : 'Sign up'}</button>
      {error && <div>{error}</div>}
    </form>
  )
}
