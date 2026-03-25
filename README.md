# tailscale-dns-sync

把 Tailscale 设备列表同步到 **AdGuard Home DNS 覆写**，实现自定义域名 + 泛域名访问 Tailscale 设备。

> **为什么需要这个？** Tailscale 官方的 MagicDNS 子域解析（`*.device.ts.net`）目前尚未对外开放——虽然已有一个[合并进主分支的 PR](https://github.com/tailscale/tailscale/pull/18258)，但官方控制平面还未启用此功能。这个项目用自己的域名实现了等价功能，你可以通过 `https://service.my-macbook.ts.example.com` 这样的地址直接访问 Tailscale 设备上运行的服务。

> 相比之前把 IP 同步到 Cloudflare 公网 DNS 的方案（见 [worker 标签](../../tree/worker)），AGH DNS 覆写只在内网生效，不会把 Tailscale IP 暴露在公网。

## 工作原理

定时拉取 Tailscale API，对比 AdGuard Home 的 DNS rewrite 规则，增删改以保持同步。每台设备自动创建两条记录：

```
<hostname>.<DOMAIN_SUFFIX>    → Tailscale IP
*.<hostname>.<DOMAIN_SUFFIX>  → Tailscale IP（支持子域名）
```

## 关于域名选择

**强烈建议使用你自己持有的真实有效域名**（如 `ts.example.com`），而不是自造的假 TLD（如 `.internal`、`.lan`）。原因：

- 浏览器对无法识别的 TLD 会当作搜索词处理，而不是直接访问
- 使用真实域名可以通过 **DNS-01 Challenge** 正常签发 TLS 证书（Let's Encrypt / ACME），让内网服务也能用 HTTPS

**局限性：** DNS 解析依赖你自己部署的 AdGuard Home，其可用性（SLA）需要自行保障。如果 AGH 宕机，内网域名解析将失效。

## 环境变量

| 变量 | 必填 | 说明 | 示例 |
|------|------|------|------|
| `TAILSCALE_API_KEY` | ✅ | Tailscale API Key | `tskey-api-xxx` |
| `TAILSCALE_TAILNET` | ✅ | Tailnet 名称 | `example.com` |
| `DOMAIN_SUFFIX` | ✅ | 内网域名后缀 | `ts.example.com` |
| `ADGUARD_URL` | ✅ | AdGuard Home 地址 | `http://adguardhome:3000` |
| `ADGUARD_USERNAME` | ✅ | AGH 用户名 | `admin` |
| `ADGUARD_PASSWORD` | ✅ | AGH 密码 | `password` |
| `CRON_SCHEDULE` | - | 同步频率，默认每小时 | `0 * * * *` |
| `TRIGGER_TOKEN` | - | 手动触发鉴权 token | 随机字符串 |
| `PORT` | - | HTTP 服务端口，默认 3001 | `3001` |
| `TAILSCALE_IP` | - | 绑定端口的 IP，默认 127.0.0.1 | `100.1.1.1` |

## 快速开始

### Docker Compose

```bash
cp .env.example .env
# 编辑 .env 填入真实配置
docker compose up -d
```

### 本地开发

需要 Go 1.23+。

```bash
go run .
```

## HTTP 接口

| 接口 | 说明 |
|------|------|
| `GET /health` | 健康检查 |
| `POST /trigger` | 立即触发同步（需 `Authorization: Bearer <TRIGGER_TOKEN>`） |
| `POST /purge` | 删除所有托管的 DNS 记录（需 `Authorization: Bearer <TRIGGER_TOKEN>`） |

```bash
curl http://localhost:3001/health
curl -X POST http://localhost:3001/trigger -H "Authorization: Bearer your-token"
curl -X POST http://localhost:3001/purge  -H "Authorization: Bearer your-token"
```

## 与 AdGuard Home 同网络部署

可以参考[示例文件](./compose.with-agh.yml)
