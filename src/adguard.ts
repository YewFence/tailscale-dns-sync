interface RewriteEntry {
  domain: string
  answer: string
}

export class AdGuardClient {
  private baseUrl: string
  private authHeader: string

  constructor(url: string, username: string, password: string) {
    this.baseUrl = url.replace(/\/$/, '')
    this.authHeader = `Basic ${Buffer.from(`${username}:${password}`).toString('base64')}`
  }

  async fetchRewrites(): Promise<RewriteEntry[]> {
    const res = await fetch(`${this.baseUrl}/control/rewrite/list`, {
      headers: { Authorization: this.authHeader },
    })
    if (!res.ok) {
      throw new Error(`AdGuard list rewrites error: ${res.status} ${await res.text()}`)
    }
    return (await res.json()) as RewriteEntry[]
  }

  async addRewrite(domain: string, answer: string): Promise<void> {
    const res = await fetch(`${this.baseUrl}/control/rewrite/add`, {
      method: 'POST',
      headers: {
        Authorization: this.authHeader,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ domain, answer }),
    })
    if (!res.ok) {
      throw new Error(`AdGuard add rewrite error: ${res.status} ${await res.text()}`)
    }
  }

  async updateRewrite(domain: string, oldIp: string, newIp: string): Promise<void> {
    const res = await fetch(`${this.baseUrl}/control/rewrite/update`, {
      method: 'PUT',
      headers: {
        Authorization: this.authHeader,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        target: { domain, answer: oldIp },
        update: { domain, answer: newIp },
      }),
    })
    if (!res.ok) {
      throw new Error(`AdGuard update rewrite error: ${res.status} ${await res.text()}`)
    }
  }

  async deleteRewrite(domain: string, answer: string): Promise<void> {
    const res = await fetch(`${this.baseUrl}/control/rewrite/delete`, {
      method: 'POST',
      headers: {
        Authorization: this.authHeader,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ domain, answer }),
    })
    if (!res.ok) {
      throw new Error(`AdGuard delete rewrite error: ${res.status} ${await res.text()}`)
    }
  }
}
