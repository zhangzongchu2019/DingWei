#!/bin/bash
# S-R62-CLI-VIEW-B 版式校准探针：mock provider 直启 TUI（不经 helper、无真凭据）。
# 用法: layout_probe.sh <tag> <codex-binary> <wire_api>
set -u
E=/tmp/cli-layout-evidence
TAG=$1; BIN=$2; WIRE=${3:-responses}
R=$E/$TAG
mkdir -p "$R/home" "$R/work" "$R/codex-home"
python3 "$E/mock_provider.py" 0 > "$R/mock.port" 2>"$R/mock.err" &
MOCKPID=$!
sleep 0.5
PORT=$(head -1 "$R/mock.port")
cat > "$R/codex-home/config.toml" <<CFG
model = "mock-model"
model_provider = "mock"
approval_policy = "never"
sandbox_mode = "read-only"

[model_providers.mock]
name = "Local mock (layout calibration only)"
base_url = "http://127.0.0.1:$PORT/v1"
env_key = "MOCK_API_KEY"
wire_api = "$WIRE"

[projects."$R/work"]
trust_level = "trusted"
CFG
SES="clilayout-$TAG"
tmux -L clilayout kill-session -t "$SES" 2>/dev/null
env -i HOME="$R/home" PATH=/usr/local/bin:/usr/bin:/bin LANG=C.UTF-8 LC_ALL=C.UTF-8 TERM=xterm-256color \
  CODEX_HOME="$R/codex-home" MOCK_API_KEY=dummy-layout-only \
  tmux -L clilayout new-session -d -s "$SES" -x 120 -y 36 -c "$R/work" "$BIN"
tmux -L clilayout pipe-pane -t "$SES" -o "cat >> $R/pane-raw.log"
date -u +%FT%TZ > "$R/start"
for i in $(seq 1 40); do
  tmux -L clilayout capture-pane -t "$SES" -p > "$R/screen-$i.txt" 2>/dev/null
  if grep -q "›" "$R/screen-$i.txt" 2>/dev/null; then break; fi
  sleep 0.5
done
tmux -L clilayout send-keys -t "$SES" "请只回复两个字：收到。不要调用任何工具，不要读写任何文件。"
sleep 0.3
tmux -L clilayout send-keys -t "$SES" Enter
date -u +%FT%TZ > "$R/injected"
for i in $(seq 1 60); do
  sleep 1
  tmux -L clilayout capture-pane -t "$SES" -p > "$R/after-$i.txt" 2>/dev/null
done
tmux -L clilayout capture-pane -t "$SES" -p > "$R/final.txt"
date -u +%FT%TZ > "$R/end"
tmux -L clilayout kill-session -t "$SES" 2>/dev/null
kill "$MOCKPID" 2>/dev/null
find "$R/codex-home/sessions" -name "rollout-*.jsonl" -exec cp {} "$R/" \; 2>/dev/null
echo "done $TAG"
