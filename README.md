# auto-cf-dns

Cloudflare Worker，自动将 Tailscale 设备同步为 Cloudflare DNS 记录。支持每小时定时同步、手动触发、以及 Tailscale Webhook 实时同步三种方式。

## 工作原理

同步逻辑分三步执行：

1. **拉取 Tailscale 设备列表** — 调用 Tailscale API，取每台设备的 `hostname`（短名）和 `addresses`（取 `100.x.x.x` 段的 Tailscale IP）
2. **拉取现有 DNS 记录** — 从 Cloudflare 查询 `comment=tailscale-sync` 的 A 记录，只操作本 Worker 管理的记录，不碰手动创建的
3. **对比同步**：
   - 新设备 → 创建 DNS 记录
   - IP 有变化 → 更新 DNS 记录
   - 设备已从 Tailscale 移除 → 删除 DNS 记录

每台设备同时维护两条记录，TTL 60 秒，不开 Cloudflare 代理：
```
my-macbook.ts.yew.im    A  100.x.x.x
*.my-macbook.ts.yew.im  A  100.x.x.x
```

**触发方式：**
- **定时**：cron `0 * * * *`，每小时执行一次
- **Webhook**：Tailscale 设备变更时实时推送，Worker 验签后立即触发同步
- **手动**：调用 `/trigger` 端点

## 环境变量

部署前需要通过 `wrangler secret put` 设置以下变量：

| 变量名 | 说明 |
|---|---|
| `TAILSCALE_API_KEY` | Tailscale API Key，在 [admin console](https://login.tailscale.com/admin/settings/keys) 生成 |
| `TAILSCALE_TAILNET` | tailnet 名称，通常是你的邮箱或组织名（如 `example.com`） |
| `CF_API_TOKEN` | Cloudflare API Token，需要 **DNS:Edit** 权限 |
| `CF_ZONE_ID` | 域名的 Zone ID，在 Cloudflare 域名概览页右侧可以找到 |
| `DOMAIN_SUFFIX` | 子域前缀，如 `ts.yew.im`（不含前导点） |
| `TRIGGER_TOKEN` | 手动触发 API 的鉴权 Token，自己随机生成一个即可 |
| `TAILSCALE_WEBHOOK_SECRET` | Tailscale Webhook 的签名密钥，在配置 Webhook 时由 Tailscale 生成 |

## 部署

```bash
# 安装依赖
pnpm install

# 设置 secrets（逐个执行，按提示粘贴值）
pnpm wrangler secret put TAILSCALE_API_KEY
pnpm wrangler secret put TAILSCALE_TAILNET
pnpm wrangler secret put CF_API_TOKEN
pnpm wrangler secret put CF_ZONE_ID
pnpm wrangler secret put DOMAIN_SUFFIX
pnpm wrangler secret put TRIGGER_TOKEN
pnpm wrangler secret put TAILSCALE_WEBHOOK_SECRET

# 部署
pnpm deploy
```

## 本地测试

```bash
# 启动本地开发服务器
pnpm dev

# 另开一个终端，手动触发 scheduled 事件
curl "http://localhost:8787/__scheduled?cron=*+*+*+*+*"
```

注意本地测试时环境变量需要在 `wrangler.toml` 里临时写死，或者用 `.dev.vars` 文件：

```ini
# .dev.vars（不要提交到 git）
TAILSCALE_API_KEY=tskey-api-xxx
TAILSCALE_TAILNET=example.com
CF_API_TOKEN=xxx
CF_ZONE_ID=xxx
DOMAIN_SUFFIX=ts.yew.im
TRIGGER_TOKEN=xxx
TAILSCALE_WEBHOOK_SECRET=xxx
```

## 手动触发

部署后可以通过 `trigger.sh` 手动触发一次同步：

```bash
# 交互式输入
bash trigger.sh

# 或通过环境变量传入
WORKER_URL=https://auto-cf-dns.xxx.workers.dev TRIGGER_TOKEN=your-token bash trigger.sh
```

也可以直接用 curl：

```bash
curl -X POST https://auto-cf-dns.xxx.workers.dev/trigger \
  -H "Authorization: Bearer your-token"
```

成功返回 `202 Accepted`，sync 在后台异步执行。

成功返回 `202 Accepted`，sync 在后台异步执行。

## Webhook 配置

部署完成后，在 Tailscale admin console → [Settings → Webhooks](https://login.tailscale.com/admin/settings/webhooks) 添加 Webhook：

- **Endpoint URL**：`https://auto-cf-dns.xxx.workers.dev/webhook`
- **订阅的事件**：勾选所有 **Node** 相关事件

Tailscale 会在创建时展示签名密钥（只显示一次），将其设置为 `TAILSCALE_WEBHOOK_SECRET`：

```bash
pnpm wrangler secret put TAILSCALE_WEBHOOK_SECRET
```

配置完成后设备上线/下线会实时触发同步，无需等待下一次 cron。

## 验证

部署后可以在 Cloudflare Dashboard → Workers → auto-cf-dns → Triggers 手动触发一次 cron，然后：

```bash
dig my-macbook.ts.yew.im
```

看是否返回对应的 Tailscale IP。
