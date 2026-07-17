import { type Page } from '@playwright/test'
import { registerTestUser, type AuthUser } from './auth'

export const API = 'http://localhost:9091'
export const WEB = 'http://localhost:3000'

export async function setupAuth(page: Page, suffix?: string): Promise<AuthUser> {
  const user = await registerTestUser(suffix)

  // Set both the token (for auth store's isAuthenticated check) and the user profile.
  // The auth store reads 'radioledger.token' to determine authentication state.
  const authData = {
    token: user.token,
    userId: user.userId,
    email: user.email,
    uuid: user.uuid,
  }
  await page.addInitScript((data: typeof authData) => {
    localStorage.setItem('radioledger.token', data.token)
    localStorage.setItem('radioledger.user_id', data.userId)
    localStorage.setItem('radioledger.user', JSON.stringify({
      uuid: data.uuid,
      email: data.email,
      callsign: 'W1TST',
      display_name: 'E2E User',
      onboarding_complete: true,
      timezone: 'UTC',
    }))
  }, authData)

  await page.route(`${API}/**`, async route => {
    const headers = {
      ...route.request().headers(),
      Authorization: `Bearer ${user.token}`,
      'X-User-ID': user.userId,
    }
    await route.continue({ headers })
  })

  let lbUuid: string | null = null
  const getLb = async (): Promise<string | null> => {
    if (lbUuid) return lbUuid
    try {
      const r = await fetch(`${API}/v1/logbooks`, {
        headers: { Authorization:`Bearer ${user.token}`, 'X-User-ID':user.userId }
      })
      const j = await r.json()
      const items: {uuid:string;is_default:boolean}[] = j?.data?.items ?? []
      lbUuid = (items.find(l=>l.is_default) ?? items[0])?.uuid ?? null
    } catch { /* ignore */ }
    return lbUuid
  }

  await page.route(`${API}/v1/qsos**`, async route => {
    const uuid = await getLb()
    if (!uuid) { await route.abort(); return }
    const newUrl = route.request().url().replace(`${API}/v1/qsos`, `${API}/v1/logbooks/${uuid}/qsos`)
    const response = await route.fetch({ url: newUrl })
    await route.fulfill({ response })
  })

  return user
}

export async function getDefaultLogbook(token: string, userId: string): Promise<{uuid:string;name:string}> {
  const r = await fetch(`${API}/v1/logbooks`, {
    headers: { Authorization:`Bearer ${token}`, 'X-User-ID':userId }
  })
  const j = await r.json()
  const items: {uuid:string;name:string;is_default:boolean}[] = j?.data?.items ?? []
  const lb = items.find(l=>l.is_default) ?? items[0]
  if (!lb) throw new Error('No logbooks found')
  return lb
}

export async function createQso(
  logbookUuid: string, token: string, userId: string,
  callsign: string, band: string, mode: string
) {
  await fetch(`${API}/v1/logbooks/${logbookUuid}/qsos`, {
    method: 'POST',
    headers: { 'Content-Type':'application/json', Authorization:`Bearer ${token}`, 'X-User-ID':userId },
    body: JSON.stringify({ callsign, band, mode, datetime_on: new Date().toISOString() }),
  })
}