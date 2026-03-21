import { fetchTailscaleDevices } from './tailscale'
import { AdGuardClient } from './adguard'

export interface SyncConfig {
  tailscaleApiKey: string
  tailscaleTailnet: string
  domainSuffix: string
  adguardUrl: string
  adguardUsername: string
  adguardPassword: string
}

export async function sync(config: SyncConfig): Promise<void> {
  console.log('Starting Tailscale → AdGuard Home DNS sync...')

  const adguard = new AdGuardClient(config.adguardUrl, config.adguardUsername, config.adguardPassword)

  const [tailscaleDevices, allRewrites] = await Promise.all([
    fetchTailscaleDevices(config.tailscaleApiKey, config.tailscaleTailnet),
    adguard.fetchRewrites(),
  ])

  const suffix = `.${config.domainSuffix}`
  const managedRewrites = allRewrites.filter((r) => r.domain.endsWith(suffix))

  const rewriteMap = new Map<string, string>()
  for (const r of managedRewrites) {
    rewriteMap.set(r.domain, r.answer)
  }

  console.log(`Tailscale devices: ${tailscaleDevices.size}, managed rewrites: ${rewriteMap.size}`)

  let added = 0, updated = 0, deleted = 0

  // 新增或更新（每台设备对应两条记录：hostname + *.hostname）
  for (const [deviceName, ip] of tailscaleDevices) {
    const fqdn = `${deviceName}${suffix}`
    const wildcard = `*.${deviceName}${suffix}`
    for (const domain of [fqdn, wildcard]) {
      const existing = rewriteMap.get(domain)
      if (!existing) {
        await adguard.addRewrite(domain, ip)
        console.log(`Added: ${domain} → ${ip}`)
        added++
      } else if (existing !== ip) {
        await adguard.updateRewrite(domain, existing, ip)
        console.log(`Updated: ${domain} ${existing} → ${ip}`)
        updated++
      } else {
        console.log(`Unchanged: ${domain} → ${ip}`)
      }
    }
  }

  // 删除已下线设备的记录
  for (const [domain, ip] of rewriteMap) {
    const base = domain.startsWith('*.') ? domain.slice(2) : domain
    const deviceName = base.slice(0, -suffix.length)
    if (!tailscaleDevices.has(deviceName)) {
      await adguard.deleteRewrite(domain, ip)
      console.log(`Deleted: ${domain}`)
      deleted++
    }
  }

  console.log(`Sync complete. added=${added} updated=${updated} deleted=${deleted}`)
}

export async function purge(config: SyncConfig): Promise<void> {
  console.log('Purging all managed DNS rewrites...')

  const adguard = new AdGuardClient(config.adguardUrl, config.adguardUsername, config.adguardPassword)
  const allRewrites = await adguard.fetchRewrites()

  const suffix = `.${config.domainSuffix}`
  const managed = allRewrites.filter((r) => r.domain.endsWith(suffix))

  if (managed.length === 0) {
    console.log('Nothing to purge.')
    return
  }

  for (const r of managed) {
    await adguard.deleteRewrite(r.domain, r.answer)
    console.log(`Deleted: ${r.domain}`)
  }
  console.log(`Purge complete. deleted=${managed.length}`)
}
