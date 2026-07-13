# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

Go 服务，将 Tailscale 设备列表同步到 Technitium DNS Server 的 Conditional Forwarder Zone，实现内网自定义域名和泛域名解析，并允许 TXT 等未覆盖记录继续递归解析。

## 常用命令

```bash
go run .
go test ./...
go build -o tailscale-dns-sync .
```

本地运行需要：

```bash
export TAILSCALE_API_KEY=... TAILSCALE_TAILNET=... DOMAIN_SUFFIX=... \
       TECHNITIUM_URL=... TECHNITIUM_TOKEN=...
go run .
```

## 架构

- `main.go`：读取环境变量，启动 cron 和 HTTP server（`/health`、`/trigger`、`/purge`），串行化 DNS 操作。
- `tailscale.go`：调用 Tailscale API v2，返回 `map[shortHostname]tailscaleIP`，只取 `100.x.x.x`。
- `technitium.go`：Technitium Token API 客户端，负责 Forwarder Zone 和 A 记录管理。
- `sync.go`：差量同步和 purge，只管理备注为 `Managed by tailscale-dns-sync` 的记录。

`runSync` 并发拉取 Tailscale 设备与 Technitium Zone 数据。每台设备维护 `hostname.suffix` 和 `*.hostname.suffix` 两条 A 记录。

`DOMAIN_SUFFIX` 对应的 Zone 必须是启用的 `Forwarder` Zone，并包含指向 `this-server` 的 FWD 记录。Zone 不存在时会自动创建。

## CI / Docker

GitHub Actions 在 push main 或 semver tag 时构建多架构镜像（amd64/arm64）并推送到 GHCR。
