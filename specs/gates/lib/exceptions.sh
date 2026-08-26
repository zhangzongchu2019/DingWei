#!/bin/sh
# 受控例外通用库 —— 三类抑制通道（style-ok / status-ok / path-ok）共用同一套判据
#
# 🔴 立本件的直接原因（乙域 terra 2026-08-26，RV-乙20/21 全闸契约）：
#   上一版只把 `style-ok` 一类抑制改成了受控例外，而
#     · path-anchor-gate 仍 `grep -v 'path-ok:'` 整行跳过；
#     · status-consistency-check 仍 `grep -v 'status-ok:'` 整行跳过；
#   两者都只校「理由非空」这个**自由文本**条件，没有逐处绑定、没有批准。
#   ⇒ **通则只落到三条通道里的一条 —— 这正是「改了规则未同步引用点」的自指形态**：
#     写下「三类抑制均属特权例外」的那个人，只改了其中一类。
#
# 🔴 第二个直接原因（terra 两个变异实测，判于 fix @ 434b8fe）：
#   变异 1：例外第 5 列「批准方」填字面「terra」（仓内并无 terra 的批准记录）⇒ 旧实现仍 rc=0；
#   变异 2：`AUTHOR_SESSION` 可由**环境变量**覆盖，填成别的名字后执笔方即可自批 ⇒ 旧实现仍 rc=0。
#   ⇒ **「批准方」是一个字符串比对，不是一次批准核验。字符串不是证据。**
#
# 契约（terra 原话，逐字实现）：
#   例外键 = `claim_id ＋ 件 ＋ 行指纹 ＋ 理由码 ＋ approval_ref`；
#   approval_ref 须解析到 **reviewer-owned、受保护的批准记录**，并核其审批人/键与同一元组匹配；
#   **缺失 / pending / 不可解析 / 审批人＝作者 ⇒ 拒（exit 1）**；
#   **授权根不可得 ⇒ UNKNOWN（exit 2），不是 PASS**；
#   🔴 **批准身份不得从可控环境变量读取。**
set -u

# 🔴 执笔方会话名【写死】—— 不读环境变量。
#   旧实现写作 ${AUTHOR_SESSION:-…}，于是「不得自增自批」这条规则的**判据本身**
#   落在被判方可控的面上：改一个环境变量就能把自己从「作者」名单里摘出去。
#   ⚠️ 同族：规则有了，执行者的开关却握在被规则约束的一方手里 ＝ 没有规则。
EXC_AUTHOR='zzc-opus50tchangshark-2642'

# 台账 / 名册 / 授权根的路径可经环境覆盖 —— **仅供反测夹具使用**。
# ⚠️ 能力边界，写明不假装：路径可覆盖意味着本库**挡不住**「改判据文件本身」这类攻击面；
#   真实防护须来自仓侧保护（CODEOWNERS ＋ 保护分支 ＋ 必过状态检查），该项**尚未授权**（业主件）。
#   ⇒ 故本库对「授权根是否成立」的默认答案是 **UNKNOWN，不是 PASS**（见 exc_trust_backends）。
EXC_APPROVALS=${EXC_APPROVALS:-specs/gates/approvals}
EXC_REVIEWERS=${EXC_REVIEWERS:-specs/gates/reviewers.txt}
EXC_TRUSTROOT=${EXC_TRUSTROOT:-specs/gates/approvals/TRUST-ROOT.md}

# 行指纹 = 该行去空白后 sha256 前 16 位。
# 🔴 用**内容哈希**而非行号：行号会随上游插入一行而整体漂移，
#   那会让所有例外在一次无关编辑后集体失效（过度严格）；
#   而内容哈希只在**该行内容真的变了**时失配 —— 这正是 terra 要的语义
#   （「内容变更或新增第二命中即失配，拒绝重审」）。
exc_fp() { sed -n "$2p" "$1" | tr -d '[:space:]' | sha256sum | cut -c1-16; }

# 授权根：打印已启用（ACTIVE）的后端名；文件缺失回 2。
# 🔴 当前仓内 TRUST-ROOT.md **一个 ACTIVE 后端都没有** —— 这是实况，不是疏漏：
#   「reviewer-owned 怎么验证」（甲 git blame / 乙 GPG / 丙 门方独占件）属门方裁量，尚未裁定。
#   ⇒ 在裁定落地前，任何**已登记的例外**都判 UNKNOWN(2)，不判 PASS。键结构先落，机制等门方。
exc_trust_backends() {
  [ -f "$EXC_TRUSTROOT" ] || return 2
  sed -n 's/^ACTIVE:[[:space:]]*\([A-Za-z0-9_-]*\).*/\1/p' "$EXC_TRUSTROOT"
}

# exc_approval_verify <approval_ref> <claim_id> <件> <行指纹> <理由码>
#   rc 0 = 批准有效 ｜ 1 = 拒（记录本身不成立）｜ 2 = UNKNOWN（取不到 / 验不了）
#
# 🔴 **丙机制精确版**（乙域 terra 收尾裁定 2026-08-26，逐条实现）：
#   · 批准记录落 **reviewer-owned 独占件 / 独占受保护分支**；
#   · `approval_ref` 解析到 **不可变 commit ＋ 记录 ID**，形如 `<门方会话>@<40位commit>#<记录ID>`；
#   · 记录逐项绑定 claim_id · 对象 · 件 · 行指纹 · 理由码 · 批准人 · UTC；
#   · **CI 从远端受保护 ref 验其可达**，且批准人 ＝ 该记录 owner / 门方；执笔**无写权、无自批权**；
#   · **ACTIVE 后端缺失 / 不可取 / 验证失败 ＝ UNKNOWN(exit 2)**，例外与依赖它的闸一律不得 PASS。
#   ⚠️ 甲（git blame）只是**元数据、非授权**，不作后端；
#     乙（GPG）可日后**叠加**，但**无密钥信任/撤销根时不可单独作后端**。
#
# 🔴 为什么用「不可变 commit」而不是「读工作区的台账文件」：
#   工作区那份文件在执笔方的写权范围内 —— 上一版正是靠它，于是「批准」这件事的证据
#   仍落在被判方可改的面上。**指到一个 commit，就把证据钉在了历史里，改不动也删不掉。**
exc_approval_verify() {
  _ref=$1; _cid=$2; _af=$3; _afp=$4; _acode=$5
  [ -n "$_ref" ] || return 1                                   # 缺失 ⇒ 拒
  case "$_ref" in *待*|*未*|*pending*|*PENDING*|*Pending*|*TODO*) return 1 ;; esac   # 未决 ⇒ 拒
  case "$_ref" in *@*'#'*) : ;; *) return 1 ;; esac             # 形状：<门方会话>@<commit>#<记录ID>
  _rev=${_ref%%@*}; _rest=${_ref#*@}
  _cmt=${_rest%%#*}; _rid=${_rest#*#}
  [ -n "$_rev" ] && [ -n "$_cmt" ] && [ -n "$_rid" ] || return 1
  [ "$_rev" = "$EXC_AUTHOR" ] && return 1                       # 审批人＝作者 ⇒ 拒（自批）
  # commit 须是 40 位十六进制 —— 🔴 短 sha / 分支名 / tag 都**不是不可变引用**
  case "$_cmt" in
    ????????????????????????????????????????) : ;;
    *) return 1 ;;
  esac
  printf '%s' "$_cmt" | grep -qE '^[0-9a-f]{40}$' || return 1

  # ===== 授权根：须有 ACTIVE 后端 =====
  _bk=$(exc_trust_backends) || return 2
  [ -n "$_bk" ] || return 2                                     # 无 ACTIVE 后端 ⇒ UNKNOWN
  case " $_bk " in
    *" reviewer-owned-protected-ref "*) : ;;
    *) return 2 ;;                                              # 其它后端尚未实现 ⇒ 验不了 ⇒ UNKNOWN
  esac

  # ===== 该门方的受保护 ref 与记录路径（由授权根声明，不由执笔方在例外里指定）=====
  #   TRUST-ROOT 行格式：REF: <门方会话> <远端受保护 ref> <记录在该 ref 内的路径>
  _cfg=$(awk -v r="$_rev" '$1=="REF:" && $2==r {print $3" "$4; exit}' "$EXC_TRUSTROOT" 2>/dev/null)
  [ -n "$_cfg" ] || return 2                                    # 该门方无受保护 ref 声明 ⇒ 取不到 ⇒ UNKNOWN
  _pref=$(printf '%s' "$_cfg" | awk '{print $1}')
  _ppath=$(printf '%s' "$_cfg" | awk '{print $2}')
  [ -n "$_pref" ] && [ -n "$_ppath" ] || return 2

  _G="git -C ${EXC_GITDIR:-.}"
  $_G rev-parse --verify --quiet "$_pref" >/dev/null 2>&1 || return 2   # 受保护 ref 不可取 ⇒ UNKNOWN
  $_G merge-base --is-ancestor "$_cmt" "$_pref" >/dev/null 2>&1 || return 2  # 不可达 ⇒ 验证失败 ⇒ UNKNOWN
  _blob=$($_G show "$_cmt:$_ppath" 2>/dev/null) || return 2             # 记录读不出 ⇒ UNKNOWN
  [ -n "$_blob" ] || return 2

  # 记录列：<记录ID> <claim_id> <被抑制对象> <件> <行指纹> <理由码> <批准人> <批准时刻UTC>
  _row=$(printf '%s\n' "$_blob" | awk -F'\t' -v i="$_rid" '$0 !~ /^#/ && $1==i {print; exit}')
  [ -n "$_row" ] || return 1                                    # 该 commit 内无此记录 ID ⇒ 拒（明确的否定）
  _a_cid=$(printf '%s' "$_row" | cut -f2)
  _a_f=$(printf '%s'   "$_row" | cut -f4)
  _a_fp=$(printf '%s'  "$_row" | cut -f5)
  _a_code=$(printf '%s' "$_row" | cut -f6)
  _a_by=$(printf '%s'  "$_row" | cut -f7)
  _a_ts=$(printf '%s'  "$_row" | cut -f8)
  # 🔴 逐项绑定：批准是对**某一处**的批准，不是对某个人的信任
  [ "$_a_cid"  = "$_cid"   ] || return 1
  [ "$_a_f"    = "$_af"    ] || return 1
  [ "$_a_fp"   = "$_afp"   ] || return 1
  [ "$_a_code" = "$_acode" ] || return 1
  [ "$_a_by"   = "$_rev"   ] || return 1
  [ "$_a_by"   = "$EXC_AUTHOR" ] && return 1
  [ -n "$_a_ts" ] || return 1
  # 批准人须在门方名册内 —— 名册本身也取自同一受保护 ref 的同一 commit（不取工作区那份）
  _reg=$($_G show "$_cmt:${EXC_REVIEWERS_INREF:-reviewers.txt}" 2>/dev/null) || return 2
  printf '%s\n' "$_reg" | grep -qx "$_a_by" || return 1
  return 0
}

# exc_code_ok <理由码> <件> <行号> <被抑制对象>
#   判【码与行形态是否相符】。🔴 判不了【豁免该不该给】，也判不了【码选得对不对】——归门方。
EXC_CODES='cross-line-false-positive historical-narrative rv-annotation is-current-rule evidence-of-absence mention-not-assertion normative-not-process'
exc_code_ok() {
  _c=$1; _cf=$2; _cl=$3; _cp=$4
  _line=$(sed -n "${_cl}p" "$_cf")
  case " $EXC_CODES " in *" $_c "*) : ;; *) return 1 ;; esac     # 未知码一律拒
  case "$_c" in
    cross-line-false-positive)
      printf '%s' "$_line" | grep -qF "$_cp" && return 1 || return 0 ;;
    historical-narrative)
      printf '%s' "$_line" | grep -qE '现有|现为|目前' && return 1
      printf '%s' "$_line" | grep -qE 'v[0-9]+\.[0-9]+|[0-9]{4}-[0-9]{2}|之前|当时|原[为写]' && return 0 || return 1 ;;
    rv-annotation)
      printf '%s' "$_line" | grep -q '【RV' && return 0 || return 1 ;;
    is-current-rule)
      grep -n '〔权威定义〕' "$_cf" >/dev/null 2>&1 || return 1
      awk -v p="$_cp" '/〔权威定义〕/{f=1} f && index($0,p){hit=1} END{exit !hit}' "$_cf" && return 0 || return 1 ;;
    evidence-of-absence)
      # 被抑制对象本身即仓内路径形态时直接实查；否则回退到取本行首个反引号对象。
      case "$_cp" in
        docs/*|specs/*|scripts/*|internal/*)
          [ ! -e "$_cp" ] && return 0 || return 1 ;;
      esac
      _obj=$(printf '%s' "$_line" | grep -oE '`[^`]+`' | head -1 | tr -d '`')
      [ -n "$_obj" ] && [ ! -e "$_obj" ] && return 0 || return 1 ;;
    normative-not-process)
      # 🔴 「本行是规范内容，不是过程叙述」——体例闸通道专用。
      #   后置条件设计成**限制它能用在哪类命中上**，而不是去判语义（语义判不了）：
      #   · 命中若是**版本戳更正 / 沿革自述 / 过程叙述**形态 ⇒ 一律不许用本码
      #     （那正是本闸要拦的东西，用本码就是拿豁免代替修复）；
      #   · 只有命中落在**角色/评审方词**上时才许用 —— 因为规范里确实存在
      #     「关于这些角色的规定」（如 §0.1「执笔方不得宣布自己的件通过」）。
      #   ⚠️ 写明不假装：本码**判不了这一句究竟是不是规范内容**，判断量在 approval。
      printf '%s' "$_line" | grep -qE 'v[0-9]+\.[0-9]+[[:space:]]*(更正|补|压缩|删去|收窄|改写|新增|撤回|再次)|打脸|撤回为|原句已撤回|此前写|本段此前|原文写成|已是第[二三四五六七八九]次|本轮|本次' && return 1
      case "$_cp" in 甲域|乙域|丙域|本席) return 0 ;; esac
      case "$_cp" in 执笔方*) return 0 ;; esac
      printf '%s' "$_cp" | grep -qE '^(sol|terra|kimi)' && return 0
      return 1 ;;
    mention-not-assertion)
      # 🔴 「提及而非断言」的机器判据：被抑制短语须落在【引号内】（「…」/ 『…』/ "…"）。
      #   自由文本写「这是提及」谁都会写；**引号是形状，形状可判**。
      awk -v p="$_cp" '
        BEGIN{ok=0}
        { s=$0
          while (match(s, /「[^」]*」|『[^』]*』|"[^"]*"/)) {
            seg=substr(s, RSTART, RLENGTH)
            if (index(seg,p)>0) ok=1
            s=substr(s, RSTART+RLENGTH)
          } }
        END{exit !ok}' <<EOF
$_line
EOF
      return $? ;;
  esac
  return 1
}

# exc_decide <登记件> <被抑制对象> <件> <行号>
#   rc 0 = 受控例外成立（已获可验证批准）｜ 1 = 不成立/拒 ｜ 2 = UNKNOWN
# 登记件列：<claim_id> <被抑制对象> <件> <行指纹> <理由码> <approval_ref>
exc_decide() {
  _reg=$1; _obj=$2; _df=$3; _dl=$4
  [ -f "$_reg" ] || return 1
  _dfp=$(exc_fp "$_df" "$_dl")
  _row=$(awk -F'\t' -v o="$_obj" -v f="$_df" -v p="$_dfp" '
          $0 !~ /^[[:space:]]*#/ && $2==o && $3==f && $4==p {print; exit}' "$_reg" 2>/dev/null)
  [ -n "$_row" ] || return 1
  _d_cid=$(printf '%s' "$_row" | cut -f1)
  _d_code=$(printf '%s' "$_row" | cut -f5)
  _d_ref=$(printf '%s' "$_row" | cut -f6)
  [ -n "$_d_cid" ] || return 1
  [ -n "$_d_code" ] || return 1                                  # 空码 ＝ 没码 ⇒ 拒
  exc_code_ok "$_d_code" "$_df" "$_dl" "$_obj" || return 1
  exc_approval_verify "$_d_ref" "$_d_cid" "$_df" "$_dfp" "$_d_code"
}

# 🔴 **冻结基线机制已整体删除**（乙域 terra 收尾裁定 2026-08-26 第三条）：
#   「冻结基线不容作为通过条件：**待批准 ＝ 拒绝**；`PASS WITH REGISTERED DEBT`
#     不能返回成功、也不能被聚合为合规 PASS。」
#   ⇒ 上一版用它让四处旧抑制在授权根落地前继续放行 —— 那实质是**用一个新名字继续放行未获批准的内容豁免**。
#   ⚠️ 本轮的正解不是「批准它们」，也不是「记成债务」，而是**把它们消掉**：
#     · 架构 A§ 取证行去掉反引号（该行断言的正是那个路径不存在，本就不该是路径锚）；
#     · spec.md 两行改述（不再字面出现清单短语）；
#     · 原型帖子样例数据改写（不再撞痕迹指纹）。
#   🔴 **能靠收窄判据或改写正文消掉的豁免，就不该存在。** 四处现已全部为零，基线件随之删除。

# ⚠️ 原 `exc_count()`（数基线有效行）随冻结基线机制一并删除 ——
#   **死代码会被下一个人当成还在生效的机制**，本轮已在「改契约不删旧反测」上踩过同族。
