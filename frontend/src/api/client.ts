export const getToken = () => localStorage.getItem('token')
export const setToken = (t: string | null) =>
  t ? localStorage.setItem('token', t) : localStorage.removeItem('token')

export async function request<T = unknown>(method: string, path: string, body?: unknown): Promise<T> {
  const token = getToken()
  const res = await fetch(path, {
    method,
    headers: {
      ...(body ? { 'Content-Type': 'application/json' } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) throw new Error((await res.text()).trim() || res.statusText)
  return res.status === 204 ? (undefined as T) : res.json()
}

const claims = (): Record<string, string> => {
  try {
    return JSON.parse(atob(getToken()!.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')))
  } catch {
    return {}
  }
}

export const userId = () => claims().sub ?? ''
export const usertype = () => claims().typ ?? ''
