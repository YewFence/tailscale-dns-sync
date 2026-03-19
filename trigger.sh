#!/usr/bin/env bash
set -euo pipefail

WORKER_URL="${WORKER_URL:-}"
TRIGGER_TOKEN="${TRIGGER_TOKEN:-}"

if [[ -z "$WORKER_URL" ]]; then
  read -rp "Worker URL (e.g. https://auto-cf-dns.xxx.workers.dev): " WORKER_URL
fi

if [[ -z "$TRIGGER_TOKEN" ]]; then
  read -rsp "Trigger Token: " TRIGGER_TOKEN
  echo
fi

echo "Triggering sync..."
curl -sf -X POST "${WORKER_URL}/trigger" \
  -H "Authorization: Bearer ${TRIGGER_TOKEN}" \
  -w "\nHTTP %{http_code}\n"
