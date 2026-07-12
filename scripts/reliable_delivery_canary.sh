#!/usr/bin/env bash
set -euo pipefail

# Dry-run by default. Set APPLY_IPTABLES=1 only on the dedicated canary helper.
# HUB_IP is mandatory in apply mode. The rule drops helper -> Hub ACK traffic,
# exercising both TCP half-close/write-buffer ambiguity and duplicate delivery ACK.
hub_ip="${HUB_IP:-}"
hub_port="${HUB_PORT:-8791}"
apply="${APPLY_IPTABLES:-0}"

if [[ "$apply" != "1" ]]; then
  echo "dry-run: sudo iptables -I OUTPUT -p tcp -d <HUB_IP> --dport $hub_port -j DROP"
  echo "restore: sudo iptables -D OUTPUT -p tcp -d <HUB_IP> --dport $hub_port -j DROP"
  echo "verify: Hub resends the same delivery_id; helper injects once and repeats its ACK"
  exit 0
fi

if [[ -z "$hub_ip" ]]; then
  echo "HUB_IP is required when APPLY_IPTABLES=1" >&2
  exit 2
fi

cleanup() {
  sudo iptables -D OUTPUT -p tcp -d "$hub_ip" --dport "$hub_port" -j DROP 2>/dev/null || true
}
trap cleanup EXIT INT TERM

sudo iptables -I OUTPUT -p tcp -d "$hub_ip" --dport "$hub_port" -j DROP
echo "ACK traffic dropped for ${DROP_SECONDS:-15}s; send one canary delivery now"
sleep "${DROP_SECONDS:-15}"
cleanup
trap - EXIT INT TERM
echo "traffic restored; inspect Hub session_delivery and helper inbox for one injection"
