const API_BASE = process.env.RADIOLEDGER_API_URL ?? 'http://localhost:9091'

export interface AuthUser {
  token: string; userId: string; email: string; uuid: string
}

export async function registerTestUser(suffix?: string): Promise<AuthUser> {
  const tag = suffix ?? `${Date.now()}_${Math.random().toString(36).slice(2,7)}`
  const email = `t_${tag}@e2e.invalid`
  const res = await fetch(`${API_BASE}/v1/auth/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password: 'TestPass123!', display_name: `E2E ${tag}`, callsign: 'W1TST' }),
  })
  if (!res.ok) throw new Error(`Register failed ${res.status}: ${await res.text()}`)
  const json = await res.json()
  if (!json.success) throw new Error(`Register error: ${json.error}`)
  const { token, user } = json.data
  return { token, userId: token.replace('dev-user-', ''), email, uuid: user.uuid }
}

export async function loginTestUser(email: string): Promise<AuthUser> {
  const res = await fetch(`${API_BASE}/v1/auth/login`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password: 'TestPass123!' }),
  })
  if (!res.ok) throw new Error(`Login failed ${res.status}: ${await res.text()}`)
  const json = await res.json()
  if (!json.success) throw new Error(`Login error: ${json.error}`)
  const { token, user } = json.data
  return { token, userId: token.replace('dev-user-', ''), email, uuid: user.uuid }
}