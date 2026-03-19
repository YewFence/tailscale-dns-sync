interface Env {
  TAILSCALE_API_KEY: string
  TAILSCALE_TAILNET: string
  CF_API_TOKEN: string
  CF_ZONE_ID: string
  DOMAIN_SUFFIX: string
  TRIGGER_TOKEN: string
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

export default {
  async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
    const { pathname } = new URL(request.url)
    if (pathname !== '/trigger') {
      return new Response('Not Found', { status: 404 })
    }
    const token = request.headers.get('Authorization')?.replace('Bearer ', '')
    if (!token || token !== env.TRIGGER_TOKEN) {
      return new Response('Unauthorized', { status: 401 })
    }
    ctx.waitUntil(syncDNS(env))
    return new Response('Sync triggered', { status: 202 })
  },

  async scheduled(_event: ScheduledEvent, env: Env, _ctx: ExecutionContext): Promise<void> {
    await syncDNS(env)
  },
}
