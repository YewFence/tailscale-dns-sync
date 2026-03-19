# auto-cf-dns

Cloudflare Worker，每小时自动拉取 Tailscale 设备列表，在 Cloudflare DNS 里维护 `device-name.ts.yourdomain.com` → Tailscale IP 的 A 记录。

## 工作原理

定时触发（cron `0 * * * *`），每次执行分三步：

1. **拉取 Tailscale 设备列表** — 调用 Tailscale API，取每台设备的 `hostname`（短名）和 `addresses`（取 `100.x.x.x` 段的 Tailscale IP）
2. **拉取现有 DNS 记录** — 从 Cloudflare 查询 `comment=tailscale-sync` 的 A 记录，只操作本 Worker 管理的记录，不碰手动创建的
3. **对比同步**：
   - 新设备 → 创建 DNS 记录
   - IP 有变化 → 更新 DNS 记录
   - 设备已从 Tailscale 移除 → 删除 DNS 记录

DNS 记录格式：`{hostname}.{DOMAIN_SUFFIX}`，例如 `my-macbook.ts.yew.im`，TTL 60 秒，不开 Cloudflare 代理。

## 环境变量

部署前需要通过 `wrangler secret put` 设置以下变量：

| 变量名 | 说明 |
|---|---|
| `TAILSCALE_API_KEY` | Tailscale API Key，在 [admin console](https://login.tailscale.com/admin/settings/keys) 生成 |
| `TAILSCALE_TAILNET` | tailnet 名称，通常是你的邮箱或组织名（如 `example.com`） |
| `CF_API_TOKEN` | Cloudflare API Token，需要 **DNS:Edit** 权限 |
| `CF_ZONE_ID` | 域名的 Zone ID，在 Cloudflare 域名概览页右侧可以找到 |
| `DOMAIN_SUFFIX` | 子域前缀，如 `ts.yew.im`（不含前导点） |

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
```

## 验证

部署后可以在 Cloudflare Dashboard → Workers → auto-cf-dns → Triggers 手动触发一次 cron，然后：

```bash
dig my-macbook.ts.yew.im
```

看是否返回对应的 Tailscale IP。
