import cron from 'node-cron'
import http from 'node:http'
import { sync, purge, SyncConfig } from './sync'

const required = [
  'TAILSCALE_API_KEY',
  'TAILSCALE_TAILNET',
  'DOMAIN_SUFFIX',
  'ADGUARD_URL',
  'ADGUARD_USERNAME',
  'ADGUARD_PASSWORD',
]

for (const key of required) {
  if (!process.env[key]) {
    console.error(`Missing required environment variable: ${key}`)
    process.exit(1)
  }
}

const config: SyncConfig = {
  tailscaleApiKey: process.env.TAILSCALE_API_KEY!,
  tailscaleTailnet: process.env.TAILSCALE_TAILNET!,
  domainSuffix: process.env.DOMAIN_SUFFIX!,
  adguardUrl: process.env.ADGUARD_URL!,
  adguardUsername: process.env.ADGUARD_USERNAME!,
  adguardPassword: process.env.ADGUARD_PASSWORD!,
}

const cronSchedule = process.env.CRON_SCHEDULE || '0 * * * *'
const triggerToken = process.env.TRIGGER_TOKEN
const port = parseInt(process.env.PORT || '3000', 10)

// 启动时立即同步一次
sync(config).catch((err) => console.error('Initial sync failed:', err))

// 定时任务
cron.schedule(cronSchedule, () => {
  sync(config).catch((err) => console.error('Scheduled sync failed:', err))
})
console.log(`Cron scheduled: ${cronSchedule}`)

// HTTP 服务
const server = http.createServer((req, res) => {
  if (req.url === '/health' && req.method === 'GET') {
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify({ status: 'ok' }))
    return
  }

  if (req.url === '/trigger' && req.method === 'POST') {
    if (triggerToken) {
      const auth = req.headers.authorization?.replace('Bearer ', '')
      if (auth !== triggerToken) {
        res.writeHead(401)
        res.end('Unauthorized')
        return
      }
    }
    res.writeHead(202)
    res.end('Sync triggered')
    sync(config).catch((err) => console.error('Manual sync failed:', err))
    return
  }

  if (req.url === '/purge' && req.method === 'POST') {
    if (triggerToken) {
      const auth = req.headers.authorization?.replace('Bearer ', '')
      if (auth !== triggerToken) {
        res.writeHead(401)
        res.end('Unauthorized')
        return
      }
    }
    res.writeHead(202)
    res.end('Purge triggered')
    purge(config).catch((err) => console.error('Purge failed:', err))
    return
  }

  res.writeHead(404)
  res.end('Not Found')
})

server.listen(port, () => {
  console.log(`HTTP server listening on port ${port}`)
})
