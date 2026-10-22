"""Finalize only after every frozen gate has actually succeeded."""
from collections import Counter
from datetime import datetime,timezone
import hashlib,json,re
from pathlib import Path
BASE=Path(__file__).resolve().parent
meta=json.loads((BASE/'FINAL-INPUT.json').read_text());gate=BASE/Path(meta['gates_result']).parent
result=json.loads((gate/'RESULT.json').read_text())
assert result['source']==meta['source_commit'] and result['passed'] is True
required={'pg-prewarm','canonical','vet','go-full','helper','withdrawals','native-0161','native-0163','browser-admission','browser-mention','browser-backfill'}
assert {r['label'] for r in result['rows']}==required and all(r['rc']==0 for r in result['rows'])
assert 'BUILD_TESTS_OK python=60 shell=2' in (gate/'canonical.stdout').read_text()
helper=(gate/'helper.stderr').read_text();tests=int(re.findall(r'Ran (\d+) tests in',helper)[-1]);assert helper.rstrip().endswith('OK')
packages={};skips=[];failed=[];last_output={}
for line in (gate/'go-full.stdout').read_text().splitlines():
    try:row=json.loads(line)
    except ValueError:continue
    key=(row.get('Package'),row.get('Test'))
    if row.get('Action')=='output':last_output[key]=row.get('Output','').strip()
    if row.get('Action')=='fail':failed.append(row)
    if not row.get('Test') and row.get('Action') in ('pass','skip','fail'):packages[row['Package']]=row['Action']
    if row.get('Test') and row.get('Action')=='skip':skips.append({'package':row['Package'],'test':row['Test'],'last_output':last_output.get(key,'')})
assert not failed
counts=Counter(packages.values());assert counts['pass']==53 and counts['skip']==13
(go_summary:=gate/'GO-SUMMARY.json').write_text(json.dumps({'packages':packages,'counts':dict(counts),'conditional_skip_tests':skips,'failed':failed},indent=2)+'\n')
withdraw=json.loads((gate/'withdrawals/RESULT.json').read_text());assert withdraw['source_head']==meta['source_commit']
assert withdraw['mutations']==49 and not withdraw['failures']
assert sum(r.get('designated_red') is True for r in withdraw['rows'])==49
assert all(r['rc']==0 for r in withdraw['rows'] if r['label'].endswith('-restored'))
for version in ('0.161.0','0.163.0'):
    data=json.loads((gate/('native-'+version)/'RESULT.json').read_text())
    assert data['source']==meta['source_commit'] and data['passed']
    for key in ('helper_fresh_process_reuses_owner','child_exit_same_id_resumed','controlled_real_pty_resume_same_id','owner_clean_stop'):
        assert data[key] is True
    assert not data.get('fixture_force_cleanup')
inputs=json.loads((BASE/meta['source_manifest']).read_text())
summary={'source':meta['source_commit'],'source_tree':meta['source_tree'],'source_inputs':len(inputs),'helper_tests':tests,
         'canonical':{'python':60,'shell':2},'withdrawals':49,'withdrawal_commands':withdraw['commands'],
         'go_test_packages':counts['pass'],'go_no_test_packages':counts['skip'],'conditional_skips':len(skips),
         'native_versions':['0.161.0','0.163.0'],'gates':len(result['rows']),'selfcheck':'all frozen gates rc0',
         'deployment':False,'activation_blocker':'S-PCE-UPDATER-RETAIN'}
(BASE/'SUMMARY.json').write_text(json.dumps(summary,indent=2)+'\n')
report=f'''# S-HELPER-PRIVATE-CONTROL-ENDPOINT 一期：作者候选交审

作者门已过，提交组长审查与 prodops 交叉核；没有部署、席位修改或集成线推送。默认 legacy 不变。任意席位启用 private 前，必须先闭合后续 **S-PCE-UPDATER-RETAIN**（活 owner 运行包保留与自升级器留槽冲突）；本件不声称已改自升级器或做真实席位升级。

冻结源 `{meta['source_commit']}`，tree `{meta['source_tree']}`；不可变输入标签 `{meta['source_tag']}`，基 r37 `{meta['base_commit']}`（0.2.3.10）。其后交件提交只增加本目录证据与文档。设计 `dbb7cf0e` 与组长一期／原生别名七硬门裁定，落实于 docs/designs/S-HELPER-PRIVATE-CONTROL-ENDPOINT-PHASE1.md。

变更：独立 owner 自持 Popen、flock、私有根与控制 socket；helper 只复用活 owner，以同 FD 核 UID／预期 exe／自持 PID＋starttime 后才发 HTTP。唯一 helper writer、准入／停止互斥、当前绑定提示单独持久化，fresh helper 读提示后仍须 read/resume 原 ID；历史回执不选或改写当前绑定。child 异常由真实 Popen wait 后有界退避重启，未知投递不自动重放；owner 失联或遗留对象则拒接／拒补启／拒按 PID 文件杀进程。显式 stop 仅在操作人确认人工退出且原生加载线程全部 idle 后停止自持 child；停止迟退不补启。

原生别名：Popen 前 app.sock 必须不存在；仅接受随后自启程序生成的确定别名。readlink 一次，严格等于自算 canonical /tmp/codex-daemon-UID/路径字节SHA256；原生共享 UID 目录非软链、0700，目标本 UID 的 socket0600。连接直接走计算目标，同 FD 核真实 child；解析前后根／alias／directory／target 不漂移，owner 持 O_PATH 句柄防 inode 立即复用。只精确清自己的 inode，不扫描或改其他原生端点。根、锁、registry、控制 socket 仍拒软链，registry 只作 locator。

配套最小修护：UnixWS stop 时捕获 socket 或以 OSError 结束读取，避免已清空对象的后台断言；只对 private connector 清除握手验证的短超时，安静的持久流不自断，legacy 自定义超时语义保留。Adapter 冲突按 private 配置与补显开关并集解决；canonical 新增私有端点测试并同步60项合同，业务接口／授权／Hub协议／运行时依赖锁未改。

| 冻结输入上的实际门 | 读数 |
| --- | --- |
| vet、全仓 Go（Go1.22.2、严格内嵌PG、DSN未设） | rc0；{counts['pass']}测试包通过、{counts['skip']}无测试文件，{len(skips)}个既有条件SKIP逐项在 GO-SUMMARY.json |
| helper 全套（每命令独占HOME、锁定venv） | rc0，{tests}项OK |
| canonical | BUILD_TESTS_OK python=60 shell=2，rc0 |
| 本地42项与指定撤修 | 49刀全红、恢复绿，{withdraw['commands']}次命令；不是原生版本验收的替代 |
| 官方0.161.0与0.163.0 | 两版真实owner、竞争首启一owner一child、fresh helper-driver进程复用、pidfd故障注入后同ID恢复、受控入口真实PTY与正常退出、空闲精确清理均rc0 |
| 相关真实浏览器夹具 | model-admission、mention-reply、backfill均rc0 |

原始命令、环境键名、独立HOME／TMPDIR、UTC、stdout／stderr／rc在 `{Path(meta['gates_result']).parent}`。原生准备仅向私有配置中不可达的127.0.0.1:9 provider提交一次本地turn以物化rollout；没有真实凭据或模型推理。人为故障只用绑定自建child的pidfd；owner正常清理均无兜底强杀。PTY通过受控CLI核owner／当前ID／app FD后exec原生resume，记录TUI确实存活、同ID且退出0；初始Resuming画面不是成功证据。fresh helper-driver是独立客户端进程，不是真实Hub席位或Console升级。

前序失败原样保留：95becaa入口清单引用中文Git路径错误，尚未跑门；d624首轮canonical的旧0.2秒候选marker未落、helper10格新增mock目标过长；b4轮helper891过，但canonical计数遗漏、Go唯一notify.sock首次bind路径恰108字节。各原因与修正见对应OUTCOME。没有改旧hung-candidate格／guard、产品长路径门或Go测试；最终重新完整跑全部要求门，不用前轮局部绿覆盖红。开发早期空线程缺rollout、等待本地turn及夹具错误也留 development 限域记录。

官方来源与对象类型订正见 OFFICIAL-SOURCES.json：0.163.0的129b8ec7是annotated tag，peeled commit为83086188；0.161.0 peeled为97901140。两版路径函数／目录合同相同，transport全文件的startup lock生命周期有差异，本件不动原生lock文件。所有二进制只在本地，未入Git、未上传。

限域：Linux，private须显式选择并要求身份enforce，启动命令须能绑定实际原生Popen PID（SH_CODEX_BIN可指定已核原生程序）；不猜包装器后代PID。不自动启用、不做透明热切；人工lease心跳、owner失联恢复单属二期。运行owner的包与解释器必须保留，启用阻断S-PCE-UPDATER-RETAIN未闭。本件不改变组外helper，未连43／dw123或修改防火墙、Hub、在线会话。

证据自检：VERIFY.py核{len(inputs)}项冻结输入、Git blob／大小／SHA，以及本目录证据清单；作者测试通过不代替独立验收。生成UTC {datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')}。
'''
(BASE/'REVIEW.md').write_text(report)
files=sorted(p for p in BASE.rglob('*') if p.is_file() and p.name!='EVIDENCE-MANIFEST.json')
manifest=[{'path':str(p.relative_to(BASE)),'size':p.stat().st_size,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()} for p in files]
(BASE/'EVIDENCE-MANIFEST.json').write_text(json.dumps(manifest,indent=2)+'\n')
print(json.dumps({'source':len(inputs),'evidence':len(manifest),'summary':summary},indent=2))
