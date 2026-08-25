#!/bin/sh
# SP7a 收口闸 —— 仓储包边界（架构设计 A§12 甲组 / SpecDoc §22.2c #1）。
#
# 立此闸的直接原因（本席自查）：
#   判据「仓储包外 sql.Open 恒零」此前【没有任何闸执行】——
#   specs/evidence/collect.sh 只把数字记进证据、整篇无一处非零退出，speclint-gate.sh 也没调过它。
#   判据不接进失败条件，等于没有判据。故 SP7a 顺序为【先立闸、后收编】。
#
# 🔴 v2（同日补，仍在 SP7a 开工当天）：v1 只数 sql.Open，漏了 §22.2c #1 的【后半句】。
#   该条判据原文是「*sql.DB / *pgx.Conn 不导出、**任何函数不返回它**」—— 两半。
#   v1 只实现前半句，于是 board.go 的 `func (b *Board) DB() *sql.DB`（12 处调用方）
#   一个字都拦不住：sql.Open 收到 0 的同时，裸句柄照样递给包外任何人。
#   ⇒ 指标达标而收口未达成，即「安全属性互相冒充」。
#   教训与今天早些时候「用单一指标推整个作用域」是同一条：**判据有几半，闸就得有几半。**
#
# 🔴🔴 本闸【做不到】什么 —— 甲域 sol CR 指出,原注释把可见性说成了强制力,此处更正:
#
#  ① **本闸不能实现「执笔不得自决放宽」。** 打印整份允许清单只增加【可见性】,
#     执笔方完全可以同时改 repo-packages.txt 与代码并让闸通过;基线文件同样没有
#     reviewer ownership、没有签名、没有与受保护前锚的单调比较。
#     **脚本无法凭自身识别谁批准了修改** —— 这件事只能由仓侧落实
#     (CODEOWNERS / 受保护分支要求甲域批准,或门方签名的 manifest 由 CI 验签)。
#     ⇒ 在仓侧保护落地之前,「清单变更属评审面」是一条【纪律】,不是一道【闸】。不得混称。
#
#  ② **判据一(建库调用)仍是正则**:保证范围＝PAT 列明 API 的直接字面调用。
#     import alias、跨行调用、未列入的新库仍可绕过 ⇒ **不得宣称「已按全部 effect 闭合」**。
#     ⚠️ 判据二已于 v3 换成 Go AST(见下),判据一尚未 —— 两条判据的强度【不一样】,不要一并宣称。
#
#  ③ **判据二(句柄逃逸)自 v3 起由 Go AST 判**(specs/tools/handle-escape),
#     因甲域 sol CR v2 #5:逐行正则被「导出函数签名换行」整个绕过,而 SpecDoc 写的是
#     「**任何**函数不返回它」—— 正则能看的只有一行,作用域远小于该句。
#     AST 版覆盖:换行签名 · import alias · 本地类型别名链 · 导出字段 · 匿名嵌入 · 接口方法。
#     已知不覆盖(写明不假装):跨包类型别名 · 经 interface{}/反射/unsafe 传出 ·
#     经导出函数【入参】交出句柄(§22.2c #1 只写「不返回」,不擅自扩严)。
#
#  以上两条按 SpecDoc E.4 #58「断言作用域不得大于实际覆盖」写明,不藏。
#
# 用法：
#   scripts/repo-boundary-gate.sh              仓根执行
#   scripts/repo-boundary-gate.sh --selftest   反测：证明本闸拦得住
#   scripts/repo-boundary-gate.sh ROOT ALLOW BASE   （反测内部用）
set -u

DEF_ALLOW=specs/gates/repo-packages.txt
DEF_BASE=specs/gates/boundary-baseline.txt

# 判据按【效果】列，不按 driver 名列 —— 换 driver 名或改用 pgxpool/sqlx 都得拦得住。
# 「按名字判非按效果判」是已归纳的自造缺陷之一。
PAT_OPEN='(sql\.Open|sql\.OpenDB|sqlx\.(Open|Connect|MustConnect)|pgxpool\.(New|NewWithConfig|Connect)|pgx\.(Connect|ConnectConfig))\('
# 裸库句柄类型（用于「逃逸」判据）
H='\*(sql\.(DB|Tx|Conn)|pgx\.(Conn|Tx)|pgxpool\.(Pool|Conn))'
# 逃逸两形态：① 导出函数/方法的返回值里出现裸句柄 ② 导出字段持有裸句柄
PAT_ESC_FUNC="^func (\([^)]+\) )?[A-Z][A-Za-z0-9_]*\([^)]*\)[^{]*$H"
PAT_ESC_FIELD="^[[:space:]]+[A-Z][A-Za-z0-9_]*[[:space:]]+$H"

_scan() { # _scan ROOT PATTERN  -> path:line:内容（已剥 root 前缀、已去 vendor）
  grep -rnE "$2" --include=*.go "$1" 2>/dev/null | sed "s|^$1/||" | grep -v '^vendor/'
}

_drop_allowed() { # 从标准输入滤掉允许清单命中的行
  _allow=$1
  while IFS= read -r line; do
    _f=${line%%:*}; _skip=0
    while IFS= read -r p; do
      case "$p" in ''|'#'*) continue;; esac
      # 🔴 必须卡目录边界(甲域 sol CR #4):原写 "$p"* 会让允许项 internal/store
      #    同时豁免 internal/storeevil —— 实测该情形下闸打印「两条判据均恒零 ✓ 达标」,
      #    违例就在隔壁兄弟目录。前缀匹配与路径匹配不是一回事。
      case "$_f" in "$p"|"$p"/*) _skip=1; break;; esac
    done < "$_allow"
    [ "$_skip" -eq 0 ] && printf '%s\n' "$line"
  done
}

# 🔴 按【处】计数,不按【行】计数(甲域 sol CR #3)。
#   原实现 grep -n 后 wc -l:同一行写两个 sql.Open 只算 1 处 ——
#   实测「基线 1、同一行两个调用」时闸报「1 处、持平、rc=0」,一处违例凭换行方式即可隐身。
#   本函数对每一行统计该行内的匹配【次数】并求和。
_count_matches() { # _count_matches <行集> <PAT>
  [ -z "$1" ] && { echo 0; return; }
  printf '%s\n' "$1" | awk -v pat="$2" '
    { line=$0
      sub(/^[^:]*:[0-9]+:/, "", line)     # 剥掉 path:line: 前缀,只数正文里的匹配
      n=0
      while (match(line, pat)) { n++; line=substr(line, RSTART+RLENGTH) }
      total+=n }
    END { print total+0 }'
}
_count() { [ -z "$1" ] && echo 0 || printf '%s\n' "$1" | wc -l | tr -d ' '; }

_baseval() { # _baseval BASEFILE KEY -> 值；缺键回 ABSENT
  awk -v k="$2" '$1==k && $0 !~ /^#/ {print $2; found=1} END{if(!found) print "ABSENT"}' "$1"
}

check() {
  _root=$1; _allow=$2; _basef=$3; _quiet=${4:-0}

  # 🔴 依赖缺失是【闸坏了】，不是【本次免检】。speclint-gate.sh 已为同一条栽过一次。
  [ -d "$_root" ]  || { echo "✗ 闸依赖缺失：扫描根 $_root 不存在 —— 闸坏了，不是免检"; return 2; }
  [ -f "$_allow" ] || { echo "✗ 闸依赖缺失：允许清单 $_allow 不存在"; return 2; }
  [ -f "$_basef" ] || { echo "✗ 闸依赖缺失：基线 $_basef 不存在 —— 无基线即无棘轮，不得放行"; return 2; }

  for k in open escape; do
    v=$(_baseval "$_basef" "$k")
    case "$v" in
      ABSENT) echo "✗ 闸依赖损坏：基线缺键 $k —— 判据少一条即少一半，不得放行"; return 2;;
      ''|*[!0-9]*) echo "✗ 闸依赖损坏：基线键 $k 的值非自然数（读到：$v）"; return 2;;
    esac
    eval "BASE_$k=\$v"
  done

  # 🔴 口径已由业主钉死为【严口径：含 _test.go】（coordinator 转达 2026-08-25T01:07:11Z）。
  #    「不设生产码/测试双值，不留 INCLUDE_TESTS 逃生开关 —— 开关本身就是第二套口径。」
  #    本席先前为等裁定做过一个可切换项，裁定一到即拆。此处立判据防它回潮：
  #    基线里一旦再出现这三个键，即判【闸坏了】，不是「多了个键不要紧」。
  for k in include_tests open_prod open_all; do
    if [ "$(_baseval "$_basef" "$k")" != "ABSENT" ]; then
      echo "✗ 基线出现已被裁撤的键 $k —— 业主已裁严口径、不设双值不留开关，第二套口径不得回潮"; return 2
    fi
  done

  while IFS= read -r p; do
    case "$p" in ''|'#'*) continue;; esac
    [ -e "$_root/$p" ] || { echo "✗ 允许清单指向不存在的路径：$p —— 清单已漂移，闸坏了"; return 2; }
  done < "$_allow"

  # 判据一：仓储包外直接建库句柄 —— 允许清单豁免（清单内就是仓储包，它本来就该开库）
  OUT_open=$(_scan "$_root" "$PAT_OPEN" | _drop_allowed "$_allow")
  # 判据二：裸库句柄逃出包外 —— 由 Go AST 判(v3)。🔴 允许清单【不】豁免:
  #   仓储包自己导出一个 DB() 访问器，正是「句柄出了仓储包」本身；豁免它等于把这条判据豁免掉。
  if [ -z "${ESCBIN:-}" ]; then
    echo "✗ 闸内部错误:句柄逃逸检测器未就绪"; return 2
  fi
  OUT_escape=$("$ESCBIN" "$_root" 2>"$_root/.esc.err")
  if [ -s "$_root/.esc.err" ]; then
    # 🔴 解析失败是【闸坏了】,不是「本次少检几个文件」。
    echo "✗ 句柄逃逸检测器报错(判为闸坏,非通过)："; sed 's/^/    /' "$_root/.esc.err"
    rm -f "$_root/.esc.err"; return 2
  fi
  rm -f "$_root/.esc.err"
  CUR_open=$(_count_matches "$OUT_open" "$PAT_OPEN")
  # AST 输出每行恰为一处逃逸,按行即按处。
  CUR_escape=$(_count "$OUT_escape")

  if [ "$_quiet" -eq 0 ]; then
    echo "  仓储包允许清单（本次生效的全部条目）："
    _n=0
    while IFS= read -r p; do
      case "$p" in ''|'#'*) continue;; esac
      _n=$((_n+1)); echo "    - $p"
    done < "$_allow"
    # 整份打印 —— 靠往清单里加路径把计数洗下去这条路，必须在日志里看得见。
    [ "$_n" -eq 0 ] && echo "    （空：仓储包尚未建立，故「仓储包外」＝全仓）"
    echo "  判据一·仓储包外建库句柄：$CUR_open 处（基线 $BASE_open，终值 0）"
    [ -n "$OUT_open" ] && printf '%s\n' "$OUT_open" | sed 's/^/    /'
    echo "  判据二·裸库句柄逃出包外：$CUR_escape 处（基线 $BASE_escape，终值 0；允许清单对本条不豁免）"
    [ -n "$OUT_escape" ] && printf '%s\n' "$OUT_escape" | sed 's/^/    /'
  fi

  # 🔴 两条判据【各判各的】，不看和 —— 合成一个总数就允许「一个降一个升」蒙混过关。
  rc=0
  [ "$CUR_open"   -gt "$BASE_open" ]   && rc=1
  [ "$CUR_escape" -gt "$BASE_escape" ] && rc=1
  return $rc
}

# ---- 反测（--selftest）：不证明「闸跑了」，证明「闸拦得住」 ----
selftest() {
  SELF=$0            # 反测中需要调用【真正的驱动段】,不能只调 check()
  T=$(mktemp -d) || return 2
  fail=0; pass_n=0
  # 项数由通过计数实算，不写死在结尾那句话里 —— 写死的数字与实际项数一分家，
  # 结尾那句「N 项全过」就成了本席刚点过的同一类缺陷（数字与产出两处不同源）。
  ok()  { pass_n=$((pass_n+1)); echo "  ✓ $1"; }
  bad() { echo "  ✗ 反测未过：$1"; fail=1; }

  mk() { # 造一棵干净的假树（两条判据均 0）
    rm -rf "$T/r"; mkdir -p "$T/r/internal/pkg" "$T/r/internal/store"
    printf 'package pkg\n\nfunc F() {}\n'   > "$T/r/internal/pkg/a.go"
    printf 'package store\n\nfunc G() {}\n' > "$T/r/internal/store/s.go"
    : > "$T/allow"; printf 'open 0\nescape 0\n' > "$T/base"
  }
  run() { check "$T/r" "$T/allow" "$T/base" 1; }

  # ① 干净树两条判据都判 0 —— 闸若恒报违例，后面全部不算数
  mk; run
  [ $? -eq 0 ] && [ "$CUR_open" -eq 0 ] && [ "$CUR_escape" -eq 0 ] \
    && ok "干净树两条判据均判 0（闸不误报）" || bad "干净树被误判 open=$CUR_open escape=$CUR_escape"

  # ② 判据一：仓储包外新增建库
  mk; printf 'package pkg\n\nfunc H() { db, _ := sql.Open("sqlite", "x"); _ = db }\n' > "$T/r/internal/pkg/b.go"
  run; [ $? -eq 1 ] && ok "拦住：仓储包外新增 sql.Open" || bad "仓储包外新增 sql.Open 未被拦"

  # ③ 判据一：换 API / 换 driver 名都得拦（按效果判，不按名字判）
  mk; printf 'package pkg\n\nfunc H() { p, _ := pgxpool.New(ctx, "dsn"); _ = p }\n' > "$T/r/internal/pkg/b.go"
  run; [ $? -eq 1 ] && ok "拦住：pgxpool.New（换 API 绕不过）" || bad "pgxpool.New 未被拦"
  mk; printf 'package pkg\n\nfunc H() { db, _ := sql.Open("mysql", "dsn"); _ = db }\n' > "$T/r/internal/pkg/b.go"
  run; [ $? -eq 1 ] && ok "拦住：sql.Open 换 driver 名" || bad "换 driver 名未被拦"

  # ④ 判据一：_test.go 计入（本席已裁定取较严一侧，避免证据与闸两套口径）
  mk; printf 'package pkg\n\nfunc TestX() { db, _ := sql.Open("pgx", "x"); _ = db }\n' > "$T/r/internal/pkg/b_test.go"
  run; [ $? -eq 1 ] && ok "拦住：_test.go 内的建库（判据含测试文件）" || bad "_test.go 内的建库被漏过"

  # ⑤ 判据二(AST 版)六种形态必须全拦 —— 甲域 sol CR v2 #5 点名的那几类。
  esc() { # esc <文件内容> ; 返回 check 的 rc
    mk; printf "$1" > "$T/r/internal/pkg/b.go"; run; }

  esc 'package pkg\n\nimport "database/sql"\n\ntype B struct{ db *sql.DB }\n\nfunc (b *B) DB() *sql.DB { return b.db }\n'
  [ $? -eq 1 ] && ok "拦住:导出方法返回 *sql.DB(单行)" || bad "单行导出方法未被拦"

  # 🔴 这一条是 v2 的阻断:逐行正则被换行签名整个绕过,且当时打印「达标」。
  esc 'package pkg\n\nimport "database/sql"\n\ntype B struct{ db *sql.DB }\n\nfunc (b *B) DB(\n) *sql.DB {\n\treturn b.db\n}\n'
  [ $? -eq 1 ] && ok "拦住:导出方法【签名换行】(正则盲区,AST 才看得见)" || bad "换行签名未被拦"

  esc 'package pkg\n\nimport dbx "database/sql"\n\nfunc Alias() *dbx.Tx { return nil }\n'
  [ $? -eq 1 ] && ok "拦住:import alias(按导入路径判,不按包名字面)" || bad "import alias 未被拦"

  esc 'package pkg\n\nimport "database/sql"\n\ntype H = *sql.DB\ntype H2 = H\n\nfunc Chain() H2 { return nil }\n'
  [ $? -eq 1 ] && ok "拦住:本地类型别名链 type H2 = H = *sql.DB" || bad "别名链未被拦"

  esc 'package pkg\n\nimport "database/sql"\n\ntype S struct {\n\tPool *sql.DB\n}\n'
  [ $? -eq 1 ] && ok "拦住:导出字段持句柄" || bad "导出字段未被拦"

  esc 'package pkg\n\nimport "database/sql"\n\ntype S struct {\n\t*sql.Conn\n}\n'
  [ $? -eq 1 ] && ok "拦住:匿名嵌入句柄(字段名即类型名,包外可访问)" || bad "匿名嵌入未被拦"

  esc 'package pkg\n\nimport "database/sql"\n\ntype I interface {\n\tHandle() *sql.DB\n}\n'
  [ $? -eq 1 ] && ok "拦住:接口方法返回句柄" || bad "接口方法未被拦"

  # ⑧ 三类【不得误报】—— 误伤会逼人绕过闸而不是满足它。
  esc 'package pkg\n\nimport "database/sql"\n\ntype b struct{ db *sql.DB }\n\nfunc (x *b) handle() *sql.DB { return x.db }\n'
  [ $? -eq 0 ] && [ "$CUR_escape" -eq 0 ] && ok "不误伤:未导出函数返回句柄(留在包内)" || bad "误伤未导出函数(escape=$CUR_escape)"

  esc 'package pkg\n\nimport "database/sql"\n\nfunc Take(tx *sql.Tx) error { return nil }\n'
  [ $? -eq 0 ] && [ "$CUR_escape" -eq 0 ] && ok "不误伤:句柄作【入参】(§22.2c #1 只写不返回,不擅自扩严)" || bad "误伤入参形态"

  esc 'package pkg\n\nimport "database/sql"\n\ntype I interface {\n\thidden() *sql.DB\n}\n'
  [ $? -eq 0 ] && [ "$CUR_escape" -eq 0 ] && ok "不误伤:未导出接口方法" || bad "误伤未导出接口方法"

  # ⑧b 解析失败须判【闸坏了】,不得当成「本次少检几个文件」。
  mk; printf 'package pkg\n\nfunc broken( {\n' > "$T/r/internal/pkg/b.go"
  run >/dev/null 2>&1
  [ $? -eq 2 ] && ok "语法错文件判为闸坏(非通过,也非跳过)" || bad "语法错文件被放行"

  # ⑨ 允许清单豁免判据一
  mk; printf 'package store\n\nfunc G() { db, _ := sql.Open("sqlite", "x"); _ = db }\n' > "$T/r/internal/store/s.go"
  printf 'internal/store\n' > "$T/allow"
  run; [ $? -eq 0 ] && [ "$CUR_open" -eq 0 ] && ok "豁免：允许清单内的建库不计入判据一" || bad "允许清单对判据一未生效"

  # ⑩ 🔴 允许清单【不】豁免判据二 —— 否则把仓储包加进清单，DB() 访问器就地合法化，
  #    判据二等于被清单豁免掉了。这一项是本条设计的骨。
  mk; printf 'package store\n\nimport "database/sql"\n\ntype B struct{ db *sql.DB }\n\nfunc (b *B) DB() *sql.DB { return b.db }\n' > "$T/r/internal/store/s.go"
  printf 'internal/store\n' > "$T/allow"
  run; [ $? -eq 1 ] && ok "允许清单【不】豁免判据二（仓储包也不许导出句柄）" || bad "允许清单把判据二一起豁免了"

  # ⑪ 🔴 两条判据不得合并看总数：一降一升必须拒。
  #    合并成一个总数就允许「收了一处建库、同时新开一个 DB() 访问器」而总数不变地过闸。
  mk; printf 'open 1\nescape 0\n' > "$T/base"
  printf 'package pkg\n\nimport "database/sql"\n\ntype B struct{ db *sql.DB }\n\nfunc (b *B) DB() *sql.DB { return b.db }\n' > "$T/r/internal/pkg/b.go"
  run  # open 实为 0（降），escape 实为 1（升）；若看总数则 1 vs 1 持平会放行
  [ $? -eq 1 ] && ok "一降一升必拒（两条判据各判各的，不看总和）" || bad "一降一升被放行 —— 判据被合并了"

  # ⑫ 基线缺失 / 非数字 / 缺键 须判「闸坏了」，不得当成通过（第六种失效形态）
  mk; rm -f "$T/base"; run >/dev/null 2>&1
  [ $? -eq 2 ] && ok "基线缺失判为闸坏（非通过）" || bad "基线缺失被当成通过"
  mk; printf 'open eight\nescape 0\n' > "$T/base"; run >/dev/null 2>&1
  [ $? -eq 2 ] && ok "基线值非数字判为闸坏（非通过）" || bad "基线非数字被当成通过"
  mk; printf 'open 0\n' > "$T/base"; run >/dev/null 2>&1
  [ $? -eq 2 ] && ok "基线缺 escape 键判为闸坏（判据少一半即不得放行）" || bad "基线缺键被当成通过"

  # ⑫a 已裁撤的开关键若回潮，须判「闸坏了」——业主裁定「不留逃生开关」要有牙，
  #     否则谁都可以再加一个 include_tests 回来，闸照旧打印通过。
  for k in include_tests open_prod open_all; do
    mk; printf "open 0\nescape 0\n$k 0\n" > "$T/base"; run >/dev/null 2>&1
    [ $? -eq 2 ] && ok "拦住：已裁撤的键 $k 回潮（判闸坏，非通过）" || bad "$k 回潮被放行"
  done

  # ⑫b 甲域 sol CR 的四条,逐条补反测 —— 反测不覆盖的地方就是闸的实际边界。
  # #3 同一行多处调用须按【处】计:原按行计数,一行两个 sql.Open 只算 1。
  mk; printf 'package pkg\n\nfunc F(){ a,_ := sql.Open("pgx","x"); b,_ := sql.Open("pgx","y"); _,_ = a,b }\n' > "$T/r/internal/pkg/b.go"
  printf 'open 1\nescape 0\n' > "$T/base"
  run; [ $? -eq 1 ] && ok "拦住:同一行两处调用按处计(不按行)" || bad "同一行两处调用仍只算 1 处(CUR=$CUR_open)"

  # #4 允许项须卡目录边界:internal/store 不得连带豁免 internal/storeevil。
  mk; mkdir -p "$T/r/internal/storeevil"
  printf 'package evil\n\nfunc F(){ db,_ := sql.Open("pgx","x"); _=db }\n' > "$T/r/internal/storeevil/e.go"
  printf 'internal/store\n' > "$T/allow"
  run; [ $? -eq 1 ] && ok "拦住:允许项不越界到兄弟目录(storeevil)" || bad "internal/store 连带豁免了 internal/storeevil"

  # #4 反向:允许项对自身与其子目录仍须生效,否则修法把闸修成了误报。
  mk; mkdir -p "$T/r/internal/store/sub"
  printf 'package sub\n\nfunc F(){ db,_ := sql.Open("pgx","x"); _=db }\n' > "$T/r/internal/store/sub/s.go"
  printf 'internal/store\n' > "$T/allow"
  run; [ $? -eq 0 ] && [ "$CUR_open" -eq 0 ] && ok "不误伤:允许项对其子目录仍生效" || bad "允许项对子目录失效(CUR=$CUR_open)"

  # #2 钩子外发现可收紧须 FAIL,不得写个文件就返回 0。
  # 🔴 本项必须调【真正的驱动段】,不能调 check() —— 收紧逻辑在 check 之外。
  #    上一版这里调的是 run(=check),测的根本不是它以为的东西,连带旁边那条
  #    「不改动基线」也成了假通过(文件本就没机会被改)。与本仓反复点的同一类。
  mk; printf 'open 3\nescape 0\n' > "$T/base"   # 实际为 0,可收紧
  env -u GIT_INDEX_FILE sh "$SELF" "$T/r" "$T/allow" "$T/base" >/dev/null 2>&1
  [ $? -eq 1 ] && ok "钩子外「可收紧」判 FAIL(不以写文件当落盘)" || bad "钩子外可收紧被放行"
  BV=$(awk '$1=="open"{print $2}' "$T/base")
  [ "$BV" = "3" ] && ok "钩子外不改动基线文件(无副作用)" || bad "钩子外偷改了基线(现为 $BV)"

  # #2 反向:钩子上下文里可收紧则应自动落盘并返回 0(否则修法把正常路径堵死)。
  mk; printf 'open 3\nescape 0\n' > "$T/base"
  GIT_INDEX_FILE=/nonexistent sh "$SELF" "$T/r" "$T/allow" "$T/base" >/dev/null 2>&1
  # BASE≠DEF_BASE 时不自动落盘(避免反测污染真基线),故此处仍应 FAIL 而非静默通过
  [ $? -eq 1 ] && ok "钩子内但基线非默认路径时仍不静默通过" || bad "非默认基线路径被静默收紧"

  # ⑬ 允许清单漂移须判「闸坏了」
  mk; printf 'internal/nosuchpkg\n' > "$T/allow"; run >/dev/null 2>&1
  [ $? -eq 2 ] && ok "允许清单漂移判为闸坏（非通过）" || bad "允许清单漂移被放行"

  rm -rf "$T"
  if [ $fail -eq 0 ]; then echo "--- 反测 $pass_n 项全过（闸有牙）"; else echo "--- 反测未过（闸没牙，不得依赖它的 ✓）"; fi
  return $fail
}

# —— 构建句柄逃逸检测器(一次,供后续所有 check 复用;反测会调 check 二十余次) ——
# 🔴 依赖缺失是【闸坏了】,不是【本次免检】。
command -v go >/dev/null 2>&1 || { echo "✗ 闸依赖缺失:未找到 go —— 判据二由 Go AST 判,无 go 即闸坏,不是免检"; exit 1; }
ESCSRC=specs/tools/handle-escape
[ -d "$ESCSRC" ] || { echo "✗ 闸依赖缺失:$ESCSRC 不存在"; exit 1; }
ESCDIR=$(mktemp -d); ESCBIN=$ESCDIR/handle-escape
trap 'rm -rf "$ESCDIR"' EXIT
if ! go build -o "$ESCBIN" ./"$ESCSRC" 2>"$ESCDIR/build.err"; then
  echo "✗ 句柄逃逸检测器构建失败(闸坏,非通过)："; sed 's/^/    /' "$ESCDIR/build.err"; exit 1
fi

case "${1:-}" in --selftest) selftest; exit $? ;; esac

ROOT=${1:-.}; ALLOW=${2:-$DEF_ALLOW}; BASE=${3:-$DEF_BASE}
echo "⑦ 仓储包边界（SP7a 甲组·收口 / §22.2c #1 两半）"
check "$ROOT" "$ALLOW" "$BASE"
rc=$?
[ $rc -eq 2 ] && exit 1
if [ $rc -eq 1 ]; then
  [ "$CUR_open"   -gt "$BASE_open" ]   && echo "✗ 判据一由 $BASE_open 升至 $CUR_open —— 棘轮只许下行。新代码请走仓储包。"
  [ "$CUR_escape" -gt "$BASE_escape" ] && echo "✗ 判据二由 $BASE_escape 升至 $CUR_escape —— 裸库句柄不得逃出包外（§22.2c #1）。"
  echo "  确需例外，改的是 $ALLOW —— 这是【纪律】不是闸(本闸识别不了谁批准)；且它对判据二不豁免。"
  exit 1
fi
# 🔴 收紧只在【真正的 pre-commit 钩子上下文】里自动落盘(甲域 sol CR #2)。
#   原实现:cur < base 即改文件、试 git add、返回 0。在 CI 里跑时那次写入【不会进入任何提交】,
#   于是「基线 8、当前 7」的提交能过 CI 而远端仍是 8,下一次把第 8 处加回来照样过 —— 棘轮空转。
#   ⇒ 「写过文件」不等于「已落盘」。钩子外一律 FAIL,要求提交者显式带上收紧后的基线。
#   判据:git 在执行钩子时会设置 GIT_INDEX_FILE,以此区分钩子上下文与手动/CI 运行。
if [ "$CUR_open" -lt "$BASE_open" ] || [ "$CUR_escape" -lt "$BASE_escape" ]; then
  if [ -n "${GIT_INDEX_FILE:-}" ] && [ "$BASE" = "$DEF_BASE" ]; then
    NO=$CUR_open; NE=$CUR_escape
    awk -v o="$NO" -v e="$NE" '
      /^#/ || NF==0 {print; next}
      $1=="open"   {print "open " o; next}
      $1=="escape" {print "escape " e; next}
      {print}' "$BASE" > "$BASE.tmp" && mv "$BASE.tmp" "$BASE"
    if ! git add "$BASE" 2>/dev/null; then
      echo "✗ 基线可收紧但 git add 失败 —— 不得以「写过文件」当「已落盘」"; exit 1
    fi
    # 自证确已入暂存区:工作树与索引之间不得再有该文件的未暂存差异。
    if ! git diff --quiet -- "$BASE" 2>/dev/null; then
      echo "✗ 基线收紧后仍有未暂存差异,拒绝放行"; exit 1
    fi
    echo "  ⑦ 收编进展:建库 $BASE_open→$NO · 逃逸 $BASE_escape→$NE,基线已收紧【并确认入暂存区】"
    exit 0
  fi
  echo "✗ 基线可收紧(建库 $BASE_open→$CUR_open · 逃逸 $BASE_escape→$CUR_escape),但当前不在 pre-commit 钩子上下文。"
  echo "  棘轮不接受「跑一次就算收紧」——请在 $BASE 里显式写入收紧后的值并随本次改动一起提交。"
  echo "  (钩子内会自动收紧并入暂存区;CI/手动运行一律要求显式落盘,否则远端基线不会变,棘轮空转。)"
  exit 1
fi
# 达标宣布 —— 🔴 口径未裁时不得宣布达标;此处口径已由业主钉死为严口径。
if [ "$CUR_open" -eq 0 ] && [ "$CUR_escape" -eq 0 ]; then
  echo "  ⑦ 两条判据均恒零 ✓（甲组只证收口证不了隔离 —— 乙组四项另验）"
else
  echo "  ⑦ 持平（建库 $CUR_open · 逃逸 $CUR_escape）✓ 未回升；终值均为 0，尚未达标"
fi
exit 0
