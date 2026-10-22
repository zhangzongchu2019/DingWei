"""Finalize only a completely successful, immutable retention candidate."""
from collections import Counter
from datetime import datetime,timezone
import hashlib,json,re
from pathlib import Path
BASE=Path(__file__).resolve().parent
meta=json.loads((BASE/'FINAL-INPUT.json').read_text());gate=BASE/meta['gate_directory']
result=json.loads((gate/'RESULT.json').read_text())
required={'pg-prewarm','canonical','vet','go-full','helper','withdrawals','pce-withdrawals','native-0161','native-0163','browser-admission','browser-mention','browser-backfill'}
assert result['source']==meta['source_commit'] and result['passed']
assert {r['label'] for r in result['rows']}==required and all(r['rc']==0 for r in result['rows'])
assert 'BUILD_TESTS_OK python=61 shell=2' in (gate/'canonical.stdout').read_text()
helper=(gate/'helper.stderr').read_text();tests=int(re.findall(r'Ran (\d+) tests in',helper)[-1]);assert helper.rstrip().endswith('OK')
packages={};skips=[];failed=[];last={}
for line in (gate/'go-full.stdout').read_text().splitlines():
 try:r=json.loads(line)
 except ValueError:continue
 key=(r.get('Package'),r.get('Test'))
 if r.get('Action')=='output':last[key]=r.get('Output','').strip()
 if r.get('Action')=='fail':failed.append(r)
 if not r.get('Test') and r.get('Action') in ('pass','skip','fail'):packages[r['Package']]=r['Action']
 if r.get('Test') and r.get('Action')=='skip':skips.append({'package':r['Package'],'test':r['Test'],'last_output':last.get(key,'')})
assert not failed
counts=Counter(packages.values());assert counts['pass']==53 and counts['skip']==13
(gate/'GO-SUMMARY.json').write_text(json.dumps({'packages':packages,'counts':dict(counts),'conditional_skips':skips,'failed':failed},indent=2)+'\n')
withdrawals=[]
for label,count in [('withdrawals',49),('pce-withdrawals',meta['new_mutations'])]:
 d=json.loads((gate/label/'RESULT.json').read_text());assert d['source_head']==meta['source_commit'] and not d['failures']
 assert d['mutations']==count and sum(r.get('designated_red') is True for r in d['rows'])==count
 assert all(r['rc']==0 for r in d['rows'] if r['label'].endswith('-restored'))
 withdrawals.append({'label':label,'mutations':count,'commands':d['commands']})
for version in ('0.161.0','0.163.0'):
 d=json.loads((gate/('native-'+version)/'RESULT.json').read_text());assert d['source']==meta['source_commit'] and d['passed']
 assert all(d[k] is True for k in ('helper_fresh_process_reuses_owner','child_exit_same_id_resumed','controlled_real_pty_resume_same_id','owner_clean_stop'))
 assert not d.get('fixture_force_cleanup')
inputs=json.loads((gate/'SOURCE.json').read_text())
summary={'source':meta['source_commit'],'tree':meta['source_tree'],'source_count':len(inputs),'helper_tests':tests,
 'go_packages_pass':counts['pass'],'go_no_test_packages':counts['skip'],'conditional_go_skips':len(skips),
 'canonical':'61+2','withdrawals':withdrawals,'native':['0.161.0','0.163.0'],'gates':len(result['rows']),
 'source_locked':True,'deployment':False,'private_enabled':False,'launcher_migration_included':False}
(BASE/'SUMMARY.json').write_text(json.dumps(summary,indent=2)+'\n')
report=f'''# S-PCE-UPDATER-RETAIN 最小首期候选交审

作者完整门通过，提交组长审、prodops 交叉核；没有部署、稳定 launcher 迁移或 private 启用。版本仍0.2.3.10，默认 legacy 不变。设计 7d0756b8 的最小①至④实现，基 c6851ed9；冻结源 `{meta['source_commit']}`、tree `{meta['source_tree']}`、不可变标签 `{meta['source_tag']}`。之后交件仅 docs。实施合同在 docs/designs/S-PCE-UPDATER-RETAIN-PHASE1.md。

产品：保留核内嵌原稳定guard、源码包根集合不变。owner启动前落 durable pin／SH文件锁并fsync，实际pass_fds交接，子端核同inode／包／运行时／端点绑定后持锁至退出；helper停止／升级不解除。包与解释器prefix／锁身份固定，任何整个Python包或运行时变化均manual_required，不做兼容放宽：版本号单独变化亦属包变化，首期普通Console升版会被拒，须另批空闲维护，不声称实现不停owner跨版本升级。升级下载不持retain/state锁，目录替换采用retain.txn→state顺序，碰撞检查pin，arm及trial/current/previous/factory亦受门；反向新owner注册核pending。失联／未完成交接保留active或unknown，即使锁已释放亦不自动回收。

空闲stop沿用同FD身份／challenge门，核pin nonce；owner真实wait并精确清理后先释继承锁再释owner.lock。只有认证成功响应且锁已释放才由稳定核记stopped回执。显式 --retain-reclaim 单pin-id只收无journal／其他pin引用的managed slot，资源独占与最后inode均须通过；不收factory／外部根／venv／解释器，不后台GC、不按PID杀孤儿。坏记录／模式／软链／上限／存储失败只拒绝。稳定旧launcher能力不足拒private，不让源码更新偷偷替换稳定挂载件。

相关入口修正：owner Popen采用稳定guard同类的显式包根trampoline，使业务cwd中的同名sessionhelper不能先执行；资源pin必须对应实际执行包。现有原49撤修的孤儿／停机负夹具补真实launcher能力前置，确保撤掉旧门能到达禁止Popen的指定断言，而非被新能力门提前拦。该变化没有放宽默认值、模型准入或恢复行为。

| 最终冻结输入实际门 | 读数 |
| --- | --- |
| Go1.22.2 vet／全仓（严格内嵌PG、DSN未设） | rc0；{counts['pass']}测试包通过、{counts['skip']}无测试文件；{len(skips)}既有条件SKIP逐项在GO-SUMMARY.json |
| helper隔离HOME＋锁定venv | rc0，{tests}项OK |
| canonical | BUILD_TESTS_OK python=61 shell=2，rc0 |
| 原PCE／新增保留门撤修 | 49＋{meta['new_mutations']}项指定红、恢复绿，共{sum(x['commands'] for x in withdrawals)}次命令；新增检查器不接受普通ERROR当指定红 |
| 官方0.161.0／0.163.0 | 两版真实Popen／FD门、双启一子、helper复用owner、child异常后同ID、受控实际PTY与空闲停止均rc0 |
| 三套相关浏览器 | model-admission、mention-reply、backfill均rc0 |

新增本地回归另含真正FD继承子进程、pin注册与回收竞态、同SHA碰撞实际HTTP／验包／stage路径、guard选择与回滚门、最后时刻inode替换、存储失败及实际owner崩溃后child仍活。崩溃注入只在独立子进程启subreaper，核原生FD后停止自持owner，child由预先绑定pidfd精确清理并waitpid；不改变在线helper或主测试进程标记。官方版本的运行仅用隔离HOME／CODEX_HOME与不可达本地provider物化rollout，无真实模型、Hub或生产席位；两版真实门非Console升级演练。解释器依赖锁仍5ef8d581，不新增运行时依赖。

前序原始红均保留。development为未提交工作树读数，其JSON HEAD字段不是实现源码锚；记录磁盘错误夹具预期、后置guard抢先拒绝、双inode互证及subTest后续ERROR等修正过程。第一冻结轮b75的旧49两格未到指定红；另独立shadow探针证明业务包先执行后端点准入失败，未启动native或调用模型，遂补包根固定后全门重跑。各轮实际rc不得改写为PASS，不能以开发或旧轮局部绿替代最终门。

限域：原设计“连续两次跨版本升级仍复用owner”不作为首期已实现的能力，按收窄裁定以活pin拒绝变化及移出指针后的保留／回收门验收。稳定retention-capable launcher迁移执行单未交、未安装；现网旧launcher仍拒private，后续本组隔离验证与明确启用令仍必要。兼容摘要放宽、人工lease、owner失联恢复、pin审计归档均后续。运行时／系统标准库是固定基线，受控源码升级不原位pip／替换venv；不声称阻止管理员绕过流程改系统。pin达到128（含审计）只阻新注册，不自动清历史。不存在任何Hub迁移、Go产品、席位或43／dw123操作。

原始stdout／stderr／rc／命令／环境键名／UTC在 `{meta['gate_directory']}`；SOURCE.json核{len(inputs)}输入的Git blob／大小／SHA，VERIFY.py另核全部证据清单。报告生成UTC {datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')}。作者门不代替独立验收。
'''
(BASE/'REVIEW.md').write_text(report)
files=sorted(p for p in BASE.rglob('*') if p.is_file() and p.name!='EVIDENCE-MANIFEST.json')
manifest=[{'path':str(p.relative_to(BASE)),'size':p.stat().st_size,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()} for p in files]
(BASE/'EVIDENCE-MANIFEST.json').write_text(json.dumps(manifest,indent=2)+'\n')
print(json.dumps({'source':len(inputs),'evidence':len(manifest),'summary':summary},indent=2))
