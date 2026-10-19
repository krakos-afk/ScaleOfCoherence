export const getToken = () => localStorage.getItem('token')
export const setToken = (t: string | null) =>
  t ? localStorage.setItem('token', t) : localStorage.removeItem('token')

/** API error carrying the HTTP status and parsed body (e.g. current version on 409). */
export class ApiError extends Error {
  status: number
  body: unknown
  constructor(status: number, message: string, body: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.body = body
  }
}

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
  if (!res.ok) {
    const text = (await res.text()).trim()
    let parsed: unknown
    try { parsed = text ? JSON.parse(text) : undefined } catch { parsed = undefined }
    const message =
      parsed && typeof parsed === 'object' && 'error' in parsed
        ? String((parsed as { error: unknown }).error)
        : text || res.statusText
    throw new ApiError(res.status, friendlyMessage(res, message), parsed)
  }
  return res.status === 204 ? (undefined as T) : res.json()
}

/** Replaces terse server messages for retryable conditions with clear ones. */
function friendlyMessage(res: Response, fallback: string): string {
  if (res.status === 429) {
    const retry = Number(res.headers.get('Retry-After') ?? '')
    return Number.isFinite(retry) && retry > 0
      ? `Too many requests — try again in ${retry}s.`
      : 'Too many requests — please slow down and try again.'
  }
  if (res.status === 503) {
    return 'The server is busy right now — please try again in a moment.'
  }
  return fallback
}

const claims = (): Record<string, string> => {
  try {
    return JSON.parse(atob(getToken()!.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')))
  } catch {
    return {}
  }
}

export const userId = () => claims().sub ?? ''
export const username = () => claims().unm ?? ''
export const usertype = () => claims().typ ?? ''
export const isAdmin = () => usertype() === 'admin'
/** A regular org member (non-admin). Team roles are enforced server-side. */
export const isUser = () => usertype() === 'user'
/** Only org-admins manage teams at the org level; team leaders manage their own team. */
export const canManageTeam = () => isAdmin()
/** Any org member may create/edit items; team roles gate stage work server-side. */
export const canWrite = () => isAdmin() || isUser()
