#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/.env"

# URL
_default_url="http://localhost:3001"
read -rp "Service URL [${_default_url}]: " _input_url
SERVICE_URL="${_input_url:-${SERVICE_URL:-${_default_url}}}"

# Token
read -rsp "Trigger Token (leave empty to read from .env): " _input_token
echo
if [[ -n "$_input_token" ]]; then
  TRIGGER_TOKEN="$_input_token"
else
  TRIGGER_TOKEN="${TRIGGER_TOKEN:-}"
  if [[ -z "$TRIGGER_TOKEN" ]] && [[ -f "$ENV_FILE" ]]; then
    TRIGGER_TOKEN="$(grep -E '^TRIGGER_TOKEN=' "$ENV_FILE" | cut -d= -f2- | tr -d '[:space:]')"
  fi
  if [[ -z "$TRIGGER_TOKEN" ]]; then
    echo "Error: TRIGGER_TOKEN not set and not found in .env" >&2
    exit 1
  fi
fi

read -rp "This will delete ALL managed DNS rewrites. Continue? [y/N] " _confirm
if [[ "${_confirm,,}" != "y" ]]; then
  echo "Aborted."
  exit 0
fi

echo "Triggering purge at ${SERVICE_URL}..."
curl -sf -X POST "${SERVICE_URL}/purge" \
  -H "Authorization: Bearer ${TRIGGER_TOKEN}" \
  -w "\nHTTP %{http_code}\n"
