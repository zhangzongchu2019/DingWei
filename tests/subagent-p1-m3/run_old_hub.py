"""Run the frozen pre-M1 Hub handlers using a test overlay, never edit its tree."""
import argparse,json,os,subprocess,tempfile
from datetime import datetime,timezone
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
p=argparse.ArgumentParser();p.add_argument('--mutate-parent',action='store_true');p.add_argument('--old-tree',required=True);p.add_argument('--output',required=True);a=p.parse_args()
old=Path(a.old_tree).resolve();out=Path(a.output).resolve();out.mkdir(parents=True,exist_ok=True)
commit=subprocess.check_output(['git','rev-parse','HEAD'],cwd=old,text=True).strip()
with tempfile.TemporaryDirectory(prefix='m3old-') as td:
 td=Path(td);target=old/'internal/m8/subagent_compat_guard_test.go';candidate=td/'matrix_test.go';candidate.write_text((ROOT/'tests/subagent-p1-m3/old_hub_matrix_test.go.txt').read_text())
 replacements={str(target):str(candidate)}
 if a.mutate_parent:
  hub=old/'internal/m8/hub.go';text=hub.read_text();needle='func (h *Hub) RouteEnvelope(ctx context.Context, env model.Envelope) error {'
  changed=text.replace(needle,needle+'\n parts:=strings.Split(env.To,"#");segments:=strings.Split(parts[0],".");if len(segments)>2 { parts[0]=strings.Join(segments[len(segments)-2:],".");env.To=strings.Join(parts,"#") }',1);assert changed!=text
  mutable=td/'hub.go';mutable.write_text(changed);replacements[str(hub)]=str(mutable)
 overlay=td/'overlay.json';overlay.write_text(json.dumps({'Replace':replacements}))
 env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DINGWEI_','DW_','DATAD_')) and k not in ('CODEX_HOME','CLAUDE_CONFIG_DIR')}
 env.update(HOME=str(td/'home'),TMPDIR=str(td),TZ="UTC");env.setdefault('GOCACHE',str(Path.home()/'.cache/go-build'));env.setdefault('GOMODCACHE',str(Path.home()/'go/pkg/mod'));(td/'home').mkdir()
 cmd=['go','test','-overlay='+str(overlay),'./internal/m8','-run','^TestM3OldHubMixedMatrix$','-v','-count=1']
 start=datetime.now(timezone.utc).isoformat()
 r=subprocess.run(cmd,cwd=old,env=env,capture_output=True,text=True);(out/'old-hub.stdout').write_text(r.stdout+r.stderr);(out/'old-hub.json').write_text(json.dumps(dict(start=start,end=datetime.now(timezone.utc).isoformat(),commit=commit,rc=r.returncode,target_red=a.mutate_parent and r.returncode!=0 and (r.stdout+r.stderr).count('AA010_OLD_HUB_FULL_CHILD_REJECT')==2,uid=os.getuid(),command=cmd[:2]+['-overlay=<isolated>']+cmd[3:]),indent=2)+'\n');print(r.stdout+r.stderr);raise SystemExit(0 if a.mutate_parent and r.returncode!=0 and (r.stdout+r.stderr).count('AA010_OLD_HUB_FULL_CHILD_REJECT')==2 else r.returncode if not a.mutate_parent else 1)
