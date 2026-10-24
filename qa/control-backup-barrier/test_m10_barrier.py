import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import m10_barrier_harness as h
import control_provision as c

class Tests(unittest.TestCase):
    def fixture(self,t):
        root=Path(t)/'root'; kit=Path(t)/'kit'; kit.mkdir()
        for binary in ('ticketlab','labctl','labcheck'): (kit/binary).write_bytes(b'fake')
        r=c.Runner(root,kit,10); (r.home/'state').mkdir(parents=True)
        old={'id':'old','title':'old title','status':'open'}
        baseline={'created_utc':'2026-10-24T01:00:00Z','tickets':[old],'postgres_snapshot':{'rows':[old]}}
        base=r.home/'state/author-before.json'; base.write_text(json.dumps(baseline))
        (root/'prepared.json').write_text(json.dumps({'control':r.id,'kit_sha256':c.digest(kit/'ticketlab'),'baseline_sha256':c.digest(base)}))
        events=[]
        def backup(command):
            self.assertEqual(command,'backup'); events.append(command)
            (r.home/'state/backup.dump').write_bytes(b'PGDMP-fake')
            (r.home/'state/backup.json').write_text(json.dumps({'created_utc':'2026-10-24T01:01:00Z','snapshot':baseline['postgres_snapshot']}))
        late={'id':'late','title':'late title','post_attempt_started_utc':'2026-10-24T01:02:00Z','confirmed_utc':'2026-10-24T01:03:00Z'}
        def after():
            events.append('teacher-barrier'); (root/'after-backup.json').write_text(json.dumps(late))
        def http(path):
            if path=='/version': return {'instance_id':r.id,'release_version':'3.0.0-good'}
            return old if path.endswith('/old') else {**late,'status':'open'}
        r.ctl=backup; r.ready=lambda:None; r.after_backup=after; r.http=http
        return r,events,late
    def exercise(self,r):
        with patch.object(h,'Runner',return_value=r),patch.object(h,'gate'),patch.object(h,'local_docker'):
            h.exercise(r.root,r.kit,r.id)
    def test_dryrun_has_no_effects(self):
        with patch.object(h,'exercise',side_effect=AssertionError),contextlib.redirect_stdout(io.StringIO()) as out:
            h.main(['--root','/none','--kit','/none'])
        self.assertTrue(json.loads(out.getvalue())['dry_run'])
    def test_positive_backup_then_barrier_no_restore(self):
        with tempfile.TemporaryDirectory() as t:
            r,events,_=self.fixture(t); self.exercise(r)
            self.assertEqual(events,['backup','teacher-barrier'])
    def test_wrong_multicall_blocks_before_backup(self):
        for binary in ('ticketlab','labctl','labcheck'):
            with self.subTest(binary=binary),tempfile.TemporaryDirectory() as t:
                r,events,_=self.fixture(t); (r.kit/binary).write_bytes(b'wrong')
                with self.assertRaises(ValueError): self.exercise(r)
                self.assertEqual(events,[])
    def test_late_present_before_backup_rejected(self):
        with tempfile.TemporaryDirectory() as t:
            r,_,late=self.fixture(t); late['id']='old'
            with self.assertRaisesRegex(ValueError,'already present'): self.exercise(r)
    def test_invalid_utc_order_rejected(self):
        with tempfile.TemporaryDirectory() as t:
            r,_,late=self.fixture(t); late['post_attempt_started_utc']='2026-10-24T00:59:00Z'
            with self.assertRaisesRegex(ValueError,'ordering'): self.exercise(r)
    def test_old_source_record_must_survive(self):
        with tempfile.TemporaryDirectory() as t:
            r,_,_=self.fixture(t); original=r.http
            r.http=lambda path: {'id':'old','title':'wrong','status':'open'} if path.endswith('/old') else original(path)
            with self.assertRaisesRegex(ValueError,'old source'): self.exercise(r)
if __name__=='__main__': unittest.main()
