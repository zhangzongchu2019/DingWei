#!/usr/bin/env bash
# g4-b12-verify.sh — 逐家验通制（V6 判据 B12）干净容器假凭据验通脚本
#
# 用法（在 G1 托管节点，有 docker；prodops 代跑）：
#   BASE_IMAGE=hub.local/dingwei/base-codex@sha256:<digest> ./g4-b12-verify.sh            # 默认只跑 Codex 两条（首期首家）
#   BASE_IMAGE=<ref> ./g4-b12-verify.sh codex-apikey codex-tokenfile claude-apikey ...    # 指定组合
#
# 判定（B12 口径）：CLI 报【鉴权失败判词】（401／unauthorized／incorrect|invalid api key／
# authentication）＝凭据通路成立 ⇒ PASS；其它失败（网络、参数、找不到凭据、交互阻塞）⇒ FAIL。
# 🔴 凭据纪律：只用假凭据（sk-test-invalid-*）；绝不对在用身份执行 login 类命令——
#    本脚本全部在一次性容器的临时 HOME 里跑（丙1 教训：Codex device-auth 会删在用 auth.json）。
#
# 读数格式（逐行，供 tosonnet55tc2k 判读）：
#   COMBO <组合> <PASS|FAIL> <判词摘录>
# 末尾一行：VERDICT <PASS|FAIL>（全 PASS 才 PASS）

set -u
BASE_IMAGE="${BASE_IMAGE:?BASE_IMAGE 必填（固定仓库＋sha256 digest 的基镜像）}"
FAKE_KEY="sk-test-invalid-b12"
FAKE_TOKEN_JSON='{"access_token":"tok-test-invalid-b12","account_id":"acct_test"}'

pass=0; fail=0
report() { # combo verdict detail
  echo "COMBO $1 $2 $3"
  if [ "$2" = PASS ]; then pass=$((pass+1)); else fail=$((fail+1)); fi
}

# judge <combo> <输出>：鉴权失败判词 ⇒ PASS
judge() {
  local combo="$1" out="$2"
  if printf '%s' "$out" | grep -qiE '401|unauthorized|incorrect api key|invalid api key|authentication_error|invalid x-api-key|not logged in|not authenticated'; then
    report "$combo" PASS "$(printf '%s' "$out" | grep -oiE '401|unauthorized|incorrect api key|invalid api key|authentication_error|invalid x-api-key|not logged in|not authenticated' | head -1)"
  else
    report "$combo" FAIL "$(printf '%s' "$out" | tail -2 | tr '\n' ' ' | cut -c1-120)"
  fi
}

# 在干净容器里跑一段脚本（tmp HOME、无网络假定不成立——要能到 API 才谈得上鉴权失败）
run_case() { # <combo> <script>
  local combo="$1" script="$2"
  local out
  out=$(docker run --rm --network bridge -e HOME=/tmp/b12home "$BASE_IMAGE" \
        sh -c "mkdir -p /tmp/b12home && $script" 2>&1)
  judge "$combo" "$out"
}

for combo in "${@:-codex-apikey codex-tokenfile}"; do
  case "$combo" in
    codex-apikey)
      # Codex API key 路径①：stdin 送 key（SPIKE：with-api-key 从 stdin 读）后非交互调用
      run_case codex-apikey "echo '$FAKE_KEY' | codex login --with-api-key 2>&1; codex exec --skip-git-repo-check 'ping' 2>&1 || true"
      ;;
    codex-tokenfile)
      # Codex 订阅令牌文件路径②：平台写入 auth.json 后直接用（无需任何 login）
      run_case codex-tokenfile "mkdir -p ~/.codex && printf '%s' '$FAKE_TOKEN_JSON' > ~/.codex/auth.json && chmod 600 ~/.codex/auth.json && codex exec --skip-git-repo-check 'ping' 2>&1 || true"
      ;;
    claude-apikey)
      # Claude Code：启动脚本读文件后进程内导出（B7：不经 docker env）——这里等价模拟
      run_case claude-apikey "printf '%s' '$FAKE_KEY' > /tmp/b12home/.k && export ANTHROPIC_API_KEY=\$(cat /tmp/b12home/.k) && claude -p 'ping' 2>&1 || true"
      ;;
    deepseek-settings)
      # DeepSeek：~/.deepcode/settings.json 的 env.API_KEY（不认环境变量——SPIKE 实测）
      run_case deepseek-settings "mkdir -p ~/.deepcode && printf '{\"env\":{\"API_KEY\":\"$FAKE_KEY\"}}' > ~/.deepcode/settings.json && deepcode 'ping' 2>&1 || true"
      ;;
    opencode-env)
      run_case opencode-env "printf '%s' '$FAKE_KEY' > /tmp/b12home/.k && export OPENAI_API_KEY=\$(cat /tmp/b12home/.k) && opencode run 'ping' 2>&1 || true"
      ;;
    *)
      echo "COMBO $combo SKIP 未知组合（验通脚本未收录——逐家验通制：没验过的组合不得上线）"
      fail=$((fail+1))
      ;;
  esac
done

if [ "$fail" -eq 0 ] && [ "$pass" -gt 0 ]; then echo "VERDICT PASS"; exit 0; else echo "VERDICT FAIL"; exit 1; fi
