#!/usr/bin/env bash
# 推送 GitHub(公开仓)前的敏感信息扫描门禁。
# 用法: scripts/check-sensitive.sh   (在仓库根跑;非0退出=发现疑似敏感项,请核查后再推)
# 原则: 绝不把 密钥/secret、真实人名、个人/生产 key_id、真实内网 IP 发布到公开仓。
#       执行必需的技术标识(SH_CLI=claude、~/.claude、claude-deepseek 等)不算敏感。
set -u
cd "$(git rev-parse --show-toplevel 2>/dev/null || echo .)"
HITS=0
scan() { # 标题, grep正则, 允许的白名单正则(占位符/示例)
  local title="$1" pat="$2" allow="${3:-}"
  local out
  out=$(git grep -nIE "$pat" -- ':!*.example' ':!scripts/check-sensitive.sh' 2>/dev/null)
  [ -n "$allow" ] && out=$(printf '%s\n' "$out" | grep -vE "$allow")
  out=$(printf '%s\n' "$out" | grep -vE '^\s*$')
  if [ -n "$out" ]; then
    printf '  ⚠️ [%s]\n%s\n' "$title" "$out" | sed 's/^/    /'
    HITS=$((HITS+1))
  fi
}

echo "== 敏感信息扫描 =="
# 1) 明文密钥/secret(最危险):真实的长随机串。占位符/说明文字/写配置的代码放行。
scan "密钥/secret" \
  'wp_[a-f0-9]{30,}|sk-[A-Za-z0-9]{20,}|[A-Za-z0-9_]*(SECRET|TOKEN|PASSWORD|API_KEY)[A-Za-z0-9_]*\s*[:=]\s*["'"'"']?[A-Za-z0-9/_+.-]{16,}' \
  "只在|管理员单独发放|shell_quote|os\.environ|getenv|env\.get|<[^>]+>|xxx|placeholder|example|示例|占位"
# 2) 个人/生产 key_id(FB-...)。放行占位符与系统内建 key。
scan "个人/生产 key_id" \
  'FB-[A-Za-z0-9]+-[A-Za-z0-9-]{4,}' \
  'FB-test-key|FB-xxx|FB-system-v-task-internal|<[^>]+>|示例|占位'
# 3) 真实人名(应泛化)。作者邮箱账号名放行。
scan "真实人名" \
  '张宗楚|符磊|谭平|陈玉玲|FB-fulei|FB-tanping' \
  ''
# 4) 真实内网/生产 IP。放行 RFC5737 文档段(203.0.113/198.51.100)、私网测试(10./192.168)、本地。
scan "真实生产/内网 IP" \
  '\b(43\.162\.107\.116|119\.139\.[0-9]+\.[0-9]+)\b' \
  ''

echo "== 提交署名合规(作者 + 消息不含 claude 署名) =="
BAD_AUTHOR=$(git log origin/main..HEAD --format='%an|%ae' 2>/dev/null | grep -iE 'anthropic|noreply@anthropic' )
[ -n "$BAD_AUTHOR" ] && { echo "  ⚠️ 存在非法作者: $BAD_AUTHOR"; HITS=$((HITS+1)); }
BAD_MSG=$(git log origin/main..HEAD --format='%B' 2>/dev/null | grep -icE 'co-authored-by:.*claude|generated with .*claude|🤖 generated')
[ "${BAD_MSG:-0}" != "0" ] && { echo "  ⚠️ 提交消息含 claude 署名 $BAD_MSG 处"; HITS=$((HITS+1)); }

echo "----"
if [ "$HITS" -eq 0 ]; then
  echo "✅ 未发现敏感项，可推送。"
  exit 0
fi
echo "❌ 发现 $HITS 类疑似敏感项，请核查(占位符/示例可加白名单)后再推。"
exit 1
