interface Env {
  TAILSCALE_API_KEY: string
  TAILSCALE_TAILNET: string
  CF_API_TOKEN: string
  CF_ZONE_ID: string
  DOMAIN_SUFFIX: string
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
  const suffix = `.${env.DOMAIN_SUFFIX}`
  for (const record of data.result) {
    if (!record.name.endsWith(suffix)) continue
    const deviceName = record.name.slice(0, -suffix.length)
    recordMap.set(deviceName, { id: record.id, ip: record.content })
  }
  return recordMap
}

async function createRecord(env: Env, deviceName: string, ip: string): Promise<void> {
  const fqdn = `${deviceName}.${env.DOMAIN_SUFFIX}`
  const url = `https://api.cloudflare.com/client/v4/zones/${env.CF_ZONE_ID}/dns_records`
  const res = await fetch(url, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${env.CF_API_TOKEN}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      type: 'A',
      name: fqdn,
      content: ip,
      ttl: 60,
      proxied: false,
      comment: 'tailscale-sync',
    }),
  })
  if (!res.ok) {
    throw new Error(`Cloudflare create DNS error for ${fqdn}: ${res.status} ${await res.text()}`)
  }
  console.log(`Created: ${fqdn} → ${ip}`)
}

async function updateRecord(env: Env, recordId: string, deviceName: string, ip: string): Promise<void> {
  const fqdn = `${deviceName}.${env.DOMAIN_SUFFIX}`
  const url = `https://api.cloudflare.com/client/v4/zones/${env.CF_ZONE_ID}/dns_records/${recordId}`
  const res = await fetch(url, {
    method: 'PATCH',
    headers: {
      Authorization: `Bearer ${env.CF_API_TOKEN}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      content: ip,
      ttl: 60,
      comment: 'tailscale-sync',
    }),
  })
  if (!res.ok) {
    throw new Error(`Cloudflare update DNS error for ${fqdn}: ${res.status} ${await res.text()}`)
  }
  console.log(`Updated: ${fqdn} → ${ip}`)
}

async function deleteRecord(env: Env, recordId: string, deviceName: string): Promise<void> {
  const fqdn = `${deviceName}.${env.DOMAIN_SUFFIX}`
  const url = `https://api.cloudflare.com/client/v4/zones/${env.CF_ZONE_ID}/dns_records/${recordId}`
  const res = await fetch(url, {
    method: 'DELETE',
    headers: {
      Authorization: `Bearer ${env.CF_API_TOKEN}`,
    },
  })
  if (!res.ok) {
    throw new Error(`Cloudflare delete DNS error for ${fqdn}: ${res.status} ${await res.text()}`)
  }
  console.log(`Deleted: ${fqdn}`)
}

async function syncDNS(env: Env): Promise<void> {
  console.log('Starting Tailscale → Cloudflare DNS sync...')

  const [tailscaleDevices, existingRecords] = await Promise.all([
    fetchTailscaleDevices(env),
    fetchExistingDNSRecords(env),
  ])

  console.log(`Tailscale devices: ${tailscaleDevices.size}, existing DNS records: ${existingRecords.size}`)

  const ops: Promise<void>[] = []

  // 新增或更新
  for (const [deviceName, ip] of tailscaleDevices) {
    const existing = existingRecords.get(deviceName)
    if (!existing) {
      ops.push(createRecord(env, deviceName, ip))
    } else if (existing.ip !== ip) {
      ops.push(updateRecord(env, existing.id, deviceName, ip))
    } else {
      console.log(`Unchanged: ${deviceName}.${env.DOMAIN_SUFFIX} → ${ip}`)
    }
  }

  // 删除已下线设备
  for (const [deviceName, record] of existingRecords) {
    if (!tailscaleDevices.has(deviceName)) {
      ops.push(deleteRecord(env, record.id, deviceName))
    }
  }

  await Promise.all(ops)
  console.log('Sync complete.')
}

export default {
  async scheduled(_event: ScheduledEvent, env: Env, _ctx: ExecutionContext): Promise<void> {
    await syncDNS(env)
  },
}
