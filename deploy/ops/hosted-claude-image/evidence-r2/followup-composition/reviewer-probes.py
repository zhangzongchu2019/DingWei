import importlib.util,stat,json,unittest
from pathlib import Path
p=Path('/data/ai-dev-data/claude-packager-composition-lab/mutant/deploy/ops/hosted-claude-image/test_claude_start.py')
spec=importlib.util.spec_from_file_location('fixture',p);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class Contracts(unittest.TestCase):
 def setUp(self):
  self.f=m.ClaudePackager('test_actual_h3_shape_composes_git_claude_overlay_and_common_guard');self.f.setUp();self.addCleanup(self.f.doCleanups);self.f.run_prepare()
 def test_guard_is_included_by_ignore_contract(self):
  out=self.f.out
  patterns=(out/'.dockerignore').read_text().splitlines()
  print('guard_mapping',json.dumps({'copy_source':'start.py','source_exists':(out/'start.py').exists(),'ignore_lines':patterns}))
  self.assertIn('!start.py',patterns,'COPY start.py is excluded by leading wildcard and absent inclusion')
 def test_runtime_directories_traversable_for_seat_uid(self):
  out=self.f.out
  names=['helper/factory','helper/factory/sessionhelper','helper/sessionhelper','claude/package','claude/package/bin','native/package','native/package/bin']
  rows={n:oct(stat.S_IMODE((out/n).stat().st_mode)) for n in names}
  print('copied_runtime_directory_modes',json.dumps(rows))
  for n in names:self.assertTrue((out/n).stat().st_mode & stat.S_IXOTH, n+' copied as root must be traversable by UID10001')
unittest.main()
