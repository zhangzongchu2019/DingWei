#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
WORKDIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
SERVICE_NAME=${SERVICE_NAME:-workpulse}
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
ENV_FILE="$WORKDIR/.env"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "缺少依赖: $1" >&2
    case "$1" in
      go) echo "安装提示: apt install golang-go，或先放置预编译 bin/workpulse" >&2 ;;
      tmux) echo "安装提示: apt install tmux。调度器/sessionHelper Mode B 需要 tmux。" >&2 ;;
      sqlite3) echo "安装提示: apt install sqlite3" >&2 ;;
      openssl) echo "安装提示: apt install openssl" >&2 ;;
      curl) echo "安装提示: apt install curl" >&2 ;;
    esac
    exit 1
  fi
}

replace_or_append() {
  key="$1"
  value="$2"
  file="$3"
  tmp="${file}.tmp"
  if grep -q "^${key}=" "$file"; then
    awk -v k="$key" -v v="$value" 'BEGIN{FS=OFS="="} $1==k {$0=k"="v} {print}' "$file" > "$tmp"
    mv "$tmp" "$file"
  else
    printf '%s=%s\n' "$key" "$value" >> "$file"
  fi
}

shell_quote() {
  printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

prompt_value() {
  key="$1"
  label="$2"
  secret="${3:-false}"
  current=$(grep -E "^${key}=" "$ENV_FILE" | tail -1 | cut -d= -f2- || true)
  if [ ! -t 0 ]; then
    return 0
  fi
  if [ -n "$current" ]; then
    return 0
  fi
  if [ "$secret" = "true" ]; then
    printf '%s: ' "$label"
    stty -echo
    IFS= read -r value || value=""
    stty echo
    printf '\n'
  else
    printf '%s: ' "$label"
    IFS= read -r value || value=""
  fi
  if [ -n "$value" ]; then
    replace_or_append "$key" "$(shell_quote "$value")" "$ENV_FILE"
  fi
}

echo "== WorkPulse deploy =="
echo "WORKDIR=$WORKDIR"

if [ "$(uname -s)" != "Linux" ]; then
  echo "deploy/install.sh 仅支持 Linux systemd 裸机部署。" >&2
  exit 1
fi

need_cmd openssl
need_cmd curl
need_cmd sqlite3
need_cmd tmux
if [ ! -x "$WORKDIR/bin/workpulse" ]; then
  need_cmd go
fi

mkdir -p "$WORKDIR/bin" "$WORKDIR/data"

if [ ! -x "$WORKDIR/bin/workpulse" ]; then
  echo "== build bin/workpulse =="
  (cd "$WORKDIR" && go build -o bin/workpulse ./cmd/workpulse)
else
  echo "== reuse existing bin/workpulse =="
fi

if [ ! -f "$ENV_FILE" ]; then
  echo "== create .env =="
  cp "$WORKDIR/.env.example" "$ENV_FILE"
  replace_or_append "WP_SECRET_KEY" "$(openssl rand -hex 32)" "$ENV_FILE"
  replace_or_append "WP_DB_PATH" "$WORKDIR/data/workpulse.db" "$ENV_FILE"
  replace_or_append "WP_DATA_DIR" "$WORKDIR/data" "$ENV_FILE"
  replace_or_append "WP_ADDR" "127.0.0.1:8791" "$ENV_FILE"
  replace_or_append "WP_SCHEDULE_BACKUP_DIR" "$WORKDIR/data/schedule-backup" "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  prompt_value "FEISHU_APP_ID" "飞书 APP_ID（可留空后续后台录入）"
  prompt_value "FEISHU_APP_SECRET" "飞书 APP_SECRET（可留空后续后台录入）" true
  prompt_value "WP_ADMIN_INIT_PASSWORD" "初始管理员密码（首次启动 seed 用）" true
  chmod 600 "$ENV_FILE"
else
  echo "== keep existing .env =="
  chmod 600 "$ENV_FILE"
  if ! grep -q '^WP_SECRET_KEY=.' "$ENV_FILE"; then
    echo "WARN: .env 缺少 WP_SECRET_KEY。请执行: openssl rand -hex 32 并写入；已有 DB 迁移时必须使用旧值。" >&2
  fi
fi

echo "== install systemd unit =="
if ! command -v systemctl >/dev/null 2>&1; then
  echo "缺少 systemctl，无法安装 systemd unit。" >&2
  exit 1
fi
tmp_unit=$(mktemp)
sed "s#{{WORKDIR}}#$WORKDIR#g; s#{{USER}}#$(id -un)#g" "$WORKDIR/deploy/workpulse.service" > "$tmp_unit"
sudo install -m 0644 "$tmp_unit" "$SERVICE_FILE"
rm -f "$tmp_unit"
sudo systemctl daemon-reload
sudo systemctl enable --now "$SERVICE_NAME"

echo "== smoke =="
"$WORKDIR/deploy/smoke.sh"

cat <<EOF

PASS: WorkPulse installed.

Next steps:
1. Put deploy/nginx-dingwei.conf into your nginx HTTPS server block,
   then adjust proxy_pass port if WP_ADDR is not 127.0.0.1:8791.
2. Open /dingwei/admin/login and log in with WP_ADMIN_USER.
3. Record members, issue key_id/secret, bind Feishu accounts, then distribute
   tools/sessionhelper/run.sh to members.
4. Scheduler needs deepseek-cli/tmux on this host and credentials under an
   absolute WP_SCHEDULER_CONFIG_DIR, for example /home/$(id -un)/.deepseek-cli.
EOF
