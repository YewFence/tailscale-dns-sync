# tailscale-dns-sync

把 Tailscale 设备列表同步到 **AdGuard Home DNS 覆写**，在内网通过自定义域名访问 Tailscale 设备。

> 相比之前把 IP 同步到 Cloudflare 公网 DNS 的方案，AGH DNS 覆写只在内网生效，不会把内网 IP 暴露在公网。

## 工作原理

定时拉取 Tailscale API，对比 AdGuard Home 的 DNS rewrite 规则，增删改以保持同步。每台设备自动创建两条记录：

```
<hostname>.<DOMAIN_SUFFIX>    → Tailscale IP
*.<hostname>.<DOMAIN_SUFFIX>  → Tailscale IP（支持子域名）
```

## 环境变量

| 变量 | 必填 | 说明 | 示例 |
|------|------|------|------|
| `TAILSCALE_API_KEY` | ✅ | Tailscale API Key | `tskey-api-xxx` |
| `TAILSCALE_TAILNET` | ✅ | Tailnet 名称 | `example.com` |
| `DOMAIN_SUFFIX` | ✅ | 内网域名后缀 | `ts,internel.net` |
| `ADGUARD_URL` | ✅ | AdGuard Home 地址 | `http://adguardhome:3000` |
| `ADGUARD_USERNAME` | ✅ | AGH 用户名 | `admin` |
| `ADGUARD_PASSWORD` | ✅ | AGH 密码 | `password` |
| `CRON_SCHEDULE` | - | 同步频率，默认每小时 | `0 * * * *` |
| `TRIGGER_TOKEN` | - | 手动触发鉴权 token | 随机字符串 |
| `PORT` | - | HTTP 服务端口，默认 3001 | `3001` |

> 域名后缀建议使用真实有效的 tld，否则浏览器可能会将输入的域名错误的识别为需要搜索的文本，而不是直接访问。

## 快速开始

### Docker Compose

```bash
cp .env.example .env
# 编辑 .env 填入真实配置
docker compose up -d
```

### 本地开发

```bash
cp .env.example .env
# 编辑 .env
pnpm install
pnpm dev
```

## HTTP 接口

| 接口 | 说明 |
|------|------|
| `GET /health` | 健康检查 |
| `POST /trigger` | 立即触发同步（需 `Authorization: Bearer <TRIGGER_TOKEN>`） |

```bash
curl http://localhost:3001/health
curl -X POST http://localhost:3001/trigger -H "Authorization: Bearer your-token"
```

## 与 AdGuard Home 同网络部署

可以参考[示例文件](./compose.with-agh.yml)
