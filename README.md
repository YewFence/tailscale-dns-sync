# tailscale-dns-sync

把 Tailscale 设备列表同步到 **Technitium DNS Server**，实现自定义域名和泛域名访问 Tailscale 设备。

> Tailscale 官方 MagicDNS 的设备子域名解析尚未在官方控制平面开放。本项目使用你持有的真实域名提供等价能力，例如通过 `https://service.my-macbook.ts.example.com` 访问设备上的服务。

## 工作原理

服务定时拉取 Tailscale API，并在 Technitium 中维护一个以 `DOMAIN_SUFFIX` 命名的 Conditional Forwarder Zone。Zone 使用 `this-server` 作为 FWD 记录，因此只覆盖本地存在的记录，其他查询继续由 Technitium 递归解析。

每台设备维护两条 A 记录：

```text
<hostname>.<DOMAIN_SUFFIX>    -> Tailscale IP
*.<hostname>.<DOMAIN_SUFFIX>  -> Tailscale IP
```

例如：

```text
my-macbook.ts.example.com    -> 100.64.0.1
*.my-macbook.ts.example.com  -> 100.64.0.1
```

这样会得到以下行为：

- `service.my-macbook.ts.example.com A` 返回 Tailscale IP。
- `_acme-challenge.service.my-macbook.ts.example.com TXT` 不受 A 记录覆盖，继续查询公网 DNS。
- ACME 客户端可以正常执行 DNS-01 Challenge 的本地传播检查。

项目创建的记录使用 `Managed by tailscale-dns-sync` 备注标记。同步和 purge 只修改带此标记的 A 记录，不会删除同一 Zone 中手工维护的其他记录。若目标名称已存在不受本项目管理的 A 记录，同步会报错而不是覆盖它。

## 域名选择

建议使用自己持有的真实域名，例如 `ts.example.com`，不要使用自造 TLD，例如 `.internal` 或 `.lan`：

- 浏览器能够正常识别真实域名。
- 公网 DNS 可以托管 `_acme-challenge` TXT 记录。
- 内网 A 记录不会把 Tailscale IP 暴露到公网。

## Technitium 准备

需要创建一个非过期 API Token：

1. 登录 Technitium Web Console。
2. 在用户菜单中创建 API Token。
3. 将 Token 写入 `TECHNITIUM_TOKEN`。

Token 需要拥有 Zones 的查看、修改和删除权限。服务会在 Zone 不存在时自动创建 `Forwarder` Zone，因此首次运行还需要创建 Zone 的权限。生产环境建议为同步服务创建权限受限的独立用户。

如果 `DOMAIN_SUFFIX` 已存在，它必须满足：

- Zone 类型为 `Forwarder`。
- Zone 已启用。
- 至少存在一条启用的、指向 `this-server` 的 FWD 记录。

不满足这些条件时服务会拒绝同步，不会自动改造已有 Zone。

## 环境变量

| 变量 | 必填 | 说明 | 示例 |
|------|------|------|------|
| `TAILSCALE_API_KEY` | 是 | Tailscale API Key | `tskey-api-xxx` |
| `TAILSCALE_TAILNET` | 是 | Tailnet 名称 | `example.com` |
| `DOMAIN_SUFFIX` | 是 | Technitium Forwarder Zone 和内网域名后缀 | `ts.example.com` |
| `TECHNITIUM_URL` | 是 | Technitium Web/API 地址 | `http://technitium:5380` |
| `TECHNITIUM_TOKEN` | 是 | Technitium 非过期 API Token | `932b...` |
| `DNS_TTL` | 否 | 受管 A 记录 TTL，默认 60 秒 | `60` |
| `CRON_SCHEDULE` | 否 | 同步周期，默认每小时 | `0 * * * *` |
| `TRIGGER_TOKEN` | 否 | 手动触发接口的 Bearer Token | 随机字符串 |
| `PORT` | 否 | HTTP 服务端口，默认 3001 | `3001` |
| `TAILSCALE_IP` | 否 | Compose 绑定的宿主机 IP，默认 127.0.0.1 | `100.64.0.10` |

`compose.with-technitium.yml` 另外使用：

| 变量 | 必填 | 说明 |
|------|------|------|
| `TECHNITIUM_ADMIN_PASSWORD` | 是 | Technitium 首次初始化的 admin 密码 |
| `TECHNITIUM_ENABLE_BLOCKING` | 否 | 首次初始化时启用广告过滤，默认 `false` |
| `TECHNITIUM_BLOCK_LIST_URLS` | 否 | 首次初始化使用的逗号分隔 Block List URL |

Technitium 的 Docker 初始化环境变量只在配置卷为空的首次启动时读取。实例初始化后，请通过 Web Console 修改管理员密码、广告过滤和 Block List。

## 快速开始

### 使用已有 Technitium

```bash
cp .env.example .env
# 编辑 .env，填写 Tailscale 和 Technitium 配置
docker compose up -d
```

### 一起部署 Technitium

先启动 Technitium，并在 Web Console 中创建 API Token：

```bash
cp .env.example .env
# 至少填写 TECHNITIUM_ADMIN_PASSWORD 和 TAILSCALE_IP
docker compose -f compose.with-technitium.yml up -d technitium
```

打开 `http://<TAILSCALE_IP>:5380`，使用 `admin` 和 `TECHNITIUM_ADMIN_PASSWORD` 登录，创建非过期 API Token，然后写入 `.env`：

```bash
docker compose -f compose.with-technitium.yml up -d
```

### 本地开发

需要 Go 1.23+：

```bash
go run .
```

## 验证解析

```bash
dig @<TECHNITIUM_DNS_IP> service.my-macbook.ts.example.com A +short
dig @<TECHNITIUM_DNS_IP> _acme-challenge.service.my-macbook.ts.example.com TXT
dig @1.1.1.1 _acme-challenge.service.my-macbook.ts.example.com TXT
```

第一条应返回 Tailscale IP，后两条的状态和 TXT 结果应一致。

## HTTP 接口

| 接口 | 说明 |
|------|------|
| `GET /health` | 健康检查 |
| `POST /trigger` | 立即同步，需要可选的 `TRIGGER_TOKEN` 鉴权 |
| `POST /purge` | 删除所有带项目备注的受管 A 记录，需要可选的 `TRIGGER_TOKEN` 鉴权 |

```bash
curl http://localhost:3001/health
curl -X POST http://localhost:3001/trigger -H "Authorization: Bearer your-token"
curl -X POST http://localhost:3001/purge -H "Authorization: Bearer your-token"
```

同步、手动触发和 purge 使用同一把互斥锁。同一时间只会执行一个 DNS 操作，重复触发会被跳过。

## 广告过滤

Technitium 支持 Block List URL、Allowed/Blocked Zone、查询日志和 DNS Apps。完成首次部署后，可以直接在 Web Console 中启用 Blocking 并添加列表，不影响本项目维护的 Forwarder Zone。
