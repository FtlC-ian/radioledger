const API_BASE = process.env.RADIOLEDGER_API_URL ?? 'http://localhost:9091'

export class ApiClient {
  constructor(private token: string, private userId: string) {}

  private headers(): Record<string,string> {
    return { 'Content-Type':'application/json', Authorization:`Bearer ${this.token}`, 'X-User-ID':this.userId }
  }

  async get<T = unknown>(path: string): Promise<T> {
    const r = await fetch(`${API_BASE}${path}`, { headers: this.headers() })
    const j = await r.json()
    if (!j.success) throw new Error(`GET ${path}: ${j.error}`)
    return j.data as T
  }

  async post<T = unknown>(path: string, body: unknown): Promise<T> {
    const r = await fetch(`${API_BASE}${path}`, {
      method: 'POST', headers: this.headers(), body: JSON.stringify(body)
    })
    const j = await r.json()
    if (!j.success) throw new Error(`POST ${path}: ${j.error}`)
    return j.data as T
  }

  async delete<T = unknown>(path: string): Promise<T> {
    const r = await fetch(`${API_BASE}${path}`, { method:'DELETE', headers: this.headers() })
    const j = await r.json()
    if (!j.success) throw new Error(`DELETE ${path}: ${j.error}`)
    return j.data as T
  }

  async getDefaultLogbook(): Promise<{uuid:string;name:string;is_default:boolean}> {
    const d = await this.get<{items:Array<{uuid:string;name:string;is_default:boolean}>}>('/v1/logbooks')
    const lb = d.items.find(l => l.is_default) ?? d.items[0]
    if (!lb) throw new Error('No logbooks')
    return lb
  }
}