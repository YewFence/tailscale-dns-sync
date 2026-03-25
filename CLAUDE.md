# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

Go 服务，将 Tailscale 设备列表同步到 AdGuard Home 的 DNS rewrite 规则，实现内网自定义域名 + 泛域名解析。

## 常用命令

```bash
# 运行
go run .

# 构建
go build -o tailscale-dns-sync .

# 本地测试（需先设置环境变量）
export TAILSCALE_API_KEY=... TAILSCALE_TAILNET=... DOMAIN_SUFFIX=... \
       ADGUARD_URL=... ADGUARD_USERNAME=... ADGUARD_PASSWORD=...
go run .
```

## 架构

4 个文件，无框架，只依赖 `robfig/cron`：

- `main.go` — 读取环境变量、启动 cron、HTTP server（`/health` `/trigger` `/purge`）
- `tailscale.go` — 调用 Tailscale API v2，返回 `map[shortHostname]tailscaleIP`（只取 `100.x.x.x`）
- `adguard.go` — AdGuard Home REST 客户端，封装 fetch/add/update/delete rewrite
- `sync.go` — 差量同步逻辑（`runSync`）和清除逻辑（`runPurge`）

`runSync` 并发拉取两端数据，diff 后对每台设备维护两条记录：`hostname.suffix` 和 `*.hostname.suffix`。

## CI / Docker

GitHub Actions 在 push main 或 semver tag 时构建多架构镜像（amd64/arm64）推送到 GHCR。
