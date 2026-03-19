interface Env {
  TAILSCALE_API_KEY: string
  TAILSCALE_TAILNET: string
  CF_API_TOKEN: string
  CF_ZONE_ID: string
  DOMAIN_SUFFIX: string
  TRIGGER_TOKEN: string
  TAILSCALE_WEBHOOK_SECRET: string
}

interface TailscaleDevice {
  hostname: string
  addresses: string[]
}

interface TailscaleDevicesResponse {
  devices: TailscaleDevice[]
}

interface CFDNSRecord {
  id: string
  name: string
  content: string
  comment?: string
}

interface CFDNSListResponse {
  result: CFDNSRecord[]
  success: boolean
}

interface BatchPost {
  type: 'A'
  name: string
  content: string
  ttl: number
  proxied: boolean
  comment: string
}

interface BatchPatch {
  id: string
  content: string
  ttl: number
  comment: string
}

interface BatchDelete {
  id: string
}

// Tailscale webhook 事件类型（只关心节点相关的）
const NODE_EVENTS = new Set([
  'nodeCreated',
  'nodeDeleted',
  'nodeApproved',
  'nodeNeedsApproval',
  'nodeKeyExpiringInOneDay',
  'nodeKeyExpired',
])

interface TailscaleWebhookPayload {
  timestamp: string
  version: number
  type: string
  tailnet: string
  data: unknown
}

// 验证 Tailscale webhook 签名
// Header 格式：t=<unix-timestamp>,v1=<hmac-sha256-hex>
// 签名内容：<unix-timestamp>.<raw-body>
// 参考：https://github.com/tailscale/tailscale/blob/main/docs/webhooks/example.go
async function verifyWebhookSignature(secret: string, header: string, body: string): Promise<boolean> {
  const parts = Object.fromEntries(header.split(',').map((p) => p.split('=')))
  const timestamp = parts['t']
  const signature = parts['v1']
  if (!timestamp || !signature) return false

  // 验证时间戳新鲜度，拒绝 5 分钟前的请求（防 replay attack）
  const ts = parseInt(timestamp, 10)
  if (isNaN(ts) || Date.now() / 1000 - ts > 300) return false

  const key = await crypto.subtle.importKey(
    'raw',
    new TextEncoder().encode(secret),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign'],
  )
  const payload = new TextEncoder().encode(`${timestamp}.${body}`)
  const signed = await crypto.subtle.sign('HMAC', key, payload)
  const expected = Array.from(new Uint8Array(signed))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')

  return expected === signature
}

async function fetchTailscaleDevices(env: Env): Promise<Map<string, string>> {
  const url = `https://api.tailscale.com/api/v2/tailnet/${encodeURIComponent(env.TAILSCALE_TAILNET)}/devices`
  const res = await fetch(url, {
    headers: {
      Authorization: `Bearer ${env.TAILSCALE_API_KEY}`,
    },
  })
  if (!res.ok) {
    throw new Error(`Tailscale API error: ${res.status} ${await res.text()}`)
  }
  const data = (await res.json()) as TailscaleDevicesResponse
  const deviceMap = new Map<string, string>()
  for (const device of data.devices) {
    // 取 Tailscale IP（100.x.x.x）
    const tsIP = device.addresses.find((a) => a.startsWith('100.'))
    if (!tsIP) continue
    // hostname 已经是短名，直接用，转小写
    const name = device.hostname.toLowerCase()
    deviceMap.set(name, tsIP)
  }
  return deviceMap
}

async function fetchExistingDNSRecords(env: Env): Promise<Map<string, { id: string; ip: string }>> {
  const url = `https://api.cloudflare.com/client/v4/zones/${env.CF_ZONE_ID}/dns_records?type=A&comment=tailscale-sync&per_page=500`
  const res = await fetch(url, {
    headers: {
      Authorization: `Bearer ${env.CF_API_TOKEN}`,
      'Content-Type': 'application/json',
    },
  })
  if (!res.ok) {
    throw new Error(`Cloudflare list DNS error: ${res.status} ${await res.text()}`)
  }
  const data = (await res.json()) as CFDNSListResponse
  const recordMap = new Map<string, { id: string; ip: string }>()
  for (const record of data.result) {
    recordMap.set(record.name, { id: record.id, ip: record.content })
  }
  return recordMap
}

async function syncDNS(env: Env): Promise<void> {
  console.log('Starting Tailscale → Cloudflare DNS sync...')

  const [tailscaleDevices, existingRecords] = await Promise.all([
    fetchTailscaleDevices(env),
    fetchExistingDNSRecords(env),
  ])

  console.log(`Tailscale devices: ${tailscaleDevices.size}, existing DNS records: ${existingRecords.size}`)

  const posts: BatchPost[] = []
  const patches: BatchPatch[] = []
  const deletes: BatchDelete[] = []

  // 新增或更新（每台设备对应两条记录：A + wildcard）
  for (const [deviceName, ip] of tailscaleDevices) {
    const fqdn = `${deviceName}.${env.DOMAIN_SUFFIX}`
    const wildcard = `*.${deviceName}.${env.DOMAIN_SUFFIX}`
    for (const name of [fqdn, wildcard]) {
      const existing = existingRecords.get(name)
      if (!existing) {
        posts.push({ type: 'A', name, content: ip, ttl: 60, proxied: false, comment: 'tailscale-sync' })
      } else if (existing.ip !== ip) {
        patches.push({ id: existing.id, content: ip, ttl: 60, comment: 'tailscale-sync' })
      } else {
        console.log(`Unchanged: ${name} → ${ip}`)
      }
    }
  }

  // 删除已下线设备的记录（A + wildcard 都删）
  const suffix = `.${env.DOMAIN_SUFFIX}`
  for (const [fqdn, record] of existingRecords) {
    const base = fqdn.startsWith('*.') ? fqdn.slice(2) : fqdn
    if (!base.endsWith(suffix)) continue
    const deviceName = base.slice(0, -suffix.length)
    if (!tailscaleDevices.has(deviceName)) {
      deletes.push({ id: record.id })
      console.log(`Queued delete: ${fqdn}`)
    }
  }

  if (posts.length === 0 && patches.length === 0 && deletes.length === 0) {
    console.log('Nothing to sync.')
    return
  }

  const url = `https://api.cloudflare.com/client/v4/zones/${env.CF_ZONE_ID}/dns_records/batch`
  const res = await fetch(url, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${env.CF_API_TOKEN}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ posts, patches, deletes }),
  })
  if (!res.ok) {
    throw new Error(`Cloudflare batch DNS error: ${res.status} ${await res.text()}`)
  }
  console.log(`Sync complete. created=${posts.length} updated=${patches.length} deleted=${deletes.length}`)
}

async function purgeAllRecords(env: Env): Promise<void> {
  console.log('Purging all tailscale-sync DNS records...')
  const existingRecords = await fetchExistingDNSRecords(env)
  if (existingRecords.size === 0) {
    console.log('Nothing to purge.')
    return
  }
  const deletes: BatchDelete[] = Array.from(existingRecords.values()).map(({ id }) => ({ id }))
  const url = `https://api.cloudflare.com/client/v4/zones/${env.CF_ZONE_ID}/dns_records/batch`
  const res = await fetch(url, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${env.CF_API_TOKEN}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ deletes }),
  })
  if (!res.ok) {
    throw new Error(`Cloudflare batch DNS error: ${res.status} ${await res.text()}`)
  }
  console.log(`Purge complete. deleted=${deletes.length}`)
}

export default {
  async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
    const { pathname } = new URL(request.url)

    if (pathname === '/trigger') {
      const token = request.headers.get('Authorization')?.replace('Bearer ', '')
      if (!token || token !== env.TRIGGER_TOKEN) {
        return new Response('Unauthorized', { status: 401 })
      }
      ctx.waitUntil(syncDNS(env))
      return new Response('Sync triggered', { status: 202 })
    }

    if (pathname === '/purge') {
      const token = request.headers.get('Authorization')?.replace('Bearer ', '')
      if (!token || token !== env.TRIGGER_TOKEN) {
        return new Response('Unauthorized', { status: 401 })
      }
      ctx.waitUntil(purgeAllRecords(env))
      return new Response('Purge triggered', { status: 202 })
    }

    if (pathname === '/webhook') {
      if (request.method !== 'POST') {
        return new Response('Method Not Allowed', { status: 405 })
      }
      const sigHeader = request.headers.get('Tailscale-Webhook-Signature')
      if (!sigHeader) {
        return new Response('Unauthorized', { status: 401 })
      }
      const body = await request.text()
      const valid = await verifyWebhookSignature(env.TAILSCALE_WEBHOOK_SECRET, sigHeader, body)
      if (!valid) {
        return new Response('Unauthorized', { status: 401 })
      }
      const events = JSON.parse(body) as TailscaleWebhookPayload[]
      const shouldSync = events.some((e) => NODE_EVENTS.has(e.type))
      if (shouldSync) {
        console.log(`Webhook triggered sync by event: ${events.map((e) => e.type).join(', ')}`)
        ctx.waitUntil(syncDNS(env))
      }
      return new Response('OK', { status: 200 })
    }

    return new Response('Not Found', { status: 404 })
  },

  async scheduled(_event: ScheduledEvent, env: Env, _ctx: ExecutionContext): Promise<void> {
    await syncDNS(env)
  },
}
