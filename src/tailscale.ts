interface TailscaleDevice {
  hostname: string
  addresses: string[]
}

interface TailscaleDevicesResponse {
  devices: TailscaleDevice[]
}

export async function fetchTailscaleDevices(apiKey: string, tailnet: string): Promise<Map<string, string>> {
  const url = `https://api.tailscale.com/api/v2/tailnet/${encodeURIComponent(tailnet)}/devices`
  const res = await fetch(url, {
    headers: {
      Authorization: `Bearer ${apiKey}`,
    },
  })
  if (!res.ok) {
    throw new Error(`Tailscale API error: ${res.status} ${await res.text()}`)
  }
  const data = (await res.json()) as TailscaleDevicesResponse
  const deviceMap = new Map<string, string>()
  for (const device of data.devices) {
    const tsIP = device.addresses.find((a) => a.startsWith('100.'))
    if (!tsIP) continue
    const name = device.hostname.toLowerCase()
    deviceMap.set(name, tsIP)
  }
  return deviceMap
}
