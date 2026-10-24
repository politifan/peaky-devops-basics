from pathlib import Path
import re,unittest
R=Path(__file__).resolve().parent
class Guards(unittest.TestCase):
 def test_guards_precede_mutation(self):
  s=(R/'git-fixture-replay.sh').read_text()
  for guard in ('GITHUB_ACTIONS','RUNNER_OS','GITHUB_REF','! -L "$RUNNER_TEMP"','sha256sum -c -'):
   self.assertLess(s.index(guard),s.index('root=$(mktemp'))
 def test_failfast_and_actual_fixture(self):
  s=(R/'git-fixture-replay.sh').read_text();self.assertIn('set -euo pipefail',s);self.assertIn('prepare git',s);self.assertIn('check git',s);self.assertNotIn('config/app.conf',s)
 def test_no_broad_cleanup_or_network_shell(self):
  s=(R/'git-fixture-replay.sh').read_text();self.assertNotIn('rm -',s);self.assertNotIn('docker',s);self.assertNotIn('curl',s);self.assertIn("m.isfile()",s);self.assertIn("'xb'",s)
 def test_workflow(self):
  s=(R/'git-fixture-replay.yml').read_text();self.assertNotIn('pull_request',s);self.assertNotIn('contents: write',s);self.assertIn('persist-credentials: false',s)
  refs=re.findall(r'uses: ([^\s]+)',s);self.assertEqual(len(refs),2)
  for ref in refs:self.assertRegex(ref,r'@[0-9a-f]{40}$')
if __name__=='__main__':unittest.main(verbosity=2)
