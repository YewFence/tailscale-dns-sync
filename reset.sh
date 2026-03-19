#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEV_VARS="$SCRIPT_DIR/.dev.vars"

# URL
_default_url="http://localhost:8787"
read -rp "Worker URL [${_default_url}]: " _input_url
WORKER_URL="${_input_url:-${WORKER_URL:-${_default_url}}}"

# Token
read -rsp "Trigger Token (leave empty to read from .dev.vars): " _input_token
echo
if [[ -n "$_input_token" ]]; then
  TRIGGER_TOKEN="$_input_token"
else
  TRIGGER_TOKEN="${TRIGGER_TOKEN:-}"
  if [[ -z "$TRIGGER_TOKEN" ]] && [[ -f "$DEV_VARS" ]]; then
    TRIGGER_TOKEN="$(grep -E '^TRIGGER_TOKEN=' "$DEV_VARS" | cut -d= -f2- | tr -d '[:space:]')"
  fi
  if [[ -z "$TRIGGER_TOKEN" ]]; then
    echo "Error: TRIGGER_TOKEN not set and not found in .dev.vars" >&2
    exit 1
  fi
fi

read -rp "This will delete ALL tailscale-sync DNS records. Continue? [y/N] " _confirm
if [[ "${_confirm,,}" != "y" ]]; then
  echo "Aborted."
  exit 0
fi

echo "Triggering purge at ${WORKER_URL}..."
curl -sf -X POST "${WORKER_URL}/purge" \
  -H "Authorization: Bearer ${TRIGGER_TOKEN}" \
  -w "\nHTTP %{http_code}\n"
