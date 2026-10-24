import contextlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
import control_provision as c

class Tests(unittest.TestCase):
    def test_file_only_actual_fixture_assertions_positive_and_negative(self):
        import assert_setup as a
        for n in (1,4,5):
            with self.subTest(n=n),tempfile.TemporaryDirectory() as t:
                root=Path(t)/'root'; kit=Path(t)/'kit'; r=c.Runner(root,kit,n)
                for sub in ('inputs','config','evidence','repo'): (r.home/sub).mkdir(parents=True,exist_ok=True)
                (root/'prepared.json').write_text(json.dumps({'setup_only':True,'actions':c.plan(n),'baseline_sha256':None}))
                (r.home/'.peaky-instance.json').write_text(json.dumps({'course_id':301758,'module_id':f'm{n:02d}','instance_id':r.id,'seed':r.seed,'port':r.port,'ownership':'peaky301758-v3','pid':0}))
                target=None
                if n==1:
                    target=r.home/'inputs/note with space.txt'
                    target.write_text(f'Заявка T{r.seed} не сохраняется.\n',encoding='utf-8')
                    (r.home/'inputs/service.log').write_text(f'2026-10-04T10:00:00Z INFO ticket=T{r.seed-1} created\n2026-10-04T10:00:01Z ERROR ticket=T{r.seed} write_failed\n2026-10-04T10:00:02Z INFO ticket=T{r.seed} retry_scheduled\n2026-10-04T10:00:03Z WARN ticket=T{r.seed+1} slow\n')
                if n==4:
                    target=r.home/'config/pipeline.env'; target.write_text('pipefail=off\n')
                    (r.home/'inputs/codes.log').write_text('E_IO\nE_DNS\nE_IO\nE_DNS\n')
                    r.capture=lambda args,expected: c.subprocess.CompletedProcess(args,1,b'profile intentional-failure\n',b'FAIL fixture.invalid\n')
                if n==5:
                    def git(args):
                        if 'show' in args:return 'TicketLab\nport=18004'
                        if 'ls-tree' in args:return 'README.txt'
                        return '?? draft.txt'
                    r.run=git
                with patch.object(a,'Runner',return_value=r),patch.object(a,'gate'),patch.object(a,'local_docker'):
                    a.check(n,root,kit)
                    if target: target.write_text('wrong')
                    else: r.run=lambda args:'wrong'
                    with self.assertRaises(AssertionError): a.check(n,root,kit)
                self.assertEqual(list((r.home/'evidence').iterdir()),[])
    def test_fixture_module_import_has_no_runtime_effects(self):
        with patch.object(c.subprocess,'run',side_effect=AssertionError),patch.object(c.urllib.request,'urlopen',side_effect=AssertionError):
            import assert_setup
            self.assertTrue(callable(assert_setup.check))
    def test_all_course_control_seeds_ports_instances(self):
        expected={1:418,2:419,3:420,4:420,5:421,6:422,7:423,8:424,10:426,11:427,12:428}
        self.assertEqual(set(c.SPECS),set(map(str,expected)))
        for n,seed in expected.items():
            r=c.Runner('/root','/kit',n)
            self.assertEqual((r.id,r.port,r.seed),(f'control{n:02d}-a',18000+n,seed))
        self.assertEqual(c.SPECS['2']['peer_port'],18102)
        self.assertEqual(c.SPECS['11']['peer_port'],18111)
        self.assertEqual(c.SPECS['11']['peer_restored_api_port'],18211)
    def test_exact_image_preload_and_rejection(self):
        events=[]
        class Fake:
            def run(self,args):
                events.append(args)
                return json.dumps([args[-1]]) if args[1]=='image' else ''
        c.preload(Fake(),8)
        self.assertEqual([x[-1] for x in events if x[1]=='pull'],list(c.IMAGES))
        self.assertTrue(all('@sha256:' in x[-1] for x in events))
        class Wrong:
            def run(self,args): return '[]' if args[1]=='image' else ''
        with self.assertRaises(ValueError): c.preload(Wrong(),10)
    def test_remote_context_blocks_before_pull(self):
        class Fake:
            def run(self,args):
                if args==['docker','context','show']: return 'default'
                return json.dumps([{'Endpoints':{'docker':{'Host':'tcp://remote:2375'}}}])
        with self.assertRaises(ValueError): c.local_docker(Fake())
    def test_diagnostics_omit_captured_output(self):
        result=c.subprocess.CompletedProcess(['docker'],7,stdout=b'PASSWORD=secret',stderr=b'postgres://user:secret@db')
        with tempfile.TemporaryDirectory() as t,patch.dict(c.os.environ,{'CONTROL_DIAGNOSTICS':str(Path(t)/'diag')}),patch.object(c.subprocess,'run',return_value=result):
            with self.assertRaises(RuntimeError): c.Runner(t,t,8).run(['docker','compose','start'])
            text=(Path(t)/'diag').read_text()
            self.assertNotIn('secret',text); self.assertNotIn('PASSWORD',text)
            self.assertEqual(json.loads(text)['exit_code'],7)
    def test_dry_run_no_effects(self):
        with patch.object(c.Runner,'setup',side_effect=AssertionError), patch.object(c.subprocess,'run',side_effect=AssertionError),contextlib.redirect_stdout(io.StringIO()) as out:
            c.main(['6','--root','/nonexistent','--kit','/nonexistent'])
        self.assertTrue(json.loads(out.getvalue())['dry_run'])
    def test_order_and_no_solution(self):
        self.assertEqual(c.plan(6)[-3:],['stop','config-data-empty','start'])
        self.assertLess(c.plan(8).index('migrate'),c.plan(8).index('baseline'))
        self.assertEqual(c.plan(8)[-1],'stop-db-only')
        self.assertLess(c.plan(12).index('baseline'),c.plan(12).index('select-bad-retry'))
        self.assertNotIn('backup',c.plan(10))
        with self.assertRaises(ValueError): c.plan(9)
    def test_gate(self):
        with tempfile.TemporaryDirectory() as t:
            env={'GITHUB_ACTIONS':'true','RUNNER_ENVIRONMENT':'github-hosted','RUNNER_TEMP':t}
            c.gate(Path(t)/'new',Path(t)/'kit','control06-a','control06-a',env,'Linux')
            for bad in ({**env,'DOCKER_HOST':'tcp://remote'},{**env,'RUNNER_ENVIRONMENT':'self-hosted'}):
                with self.assertRaises(ValueError): c.gate(Path(t)/'new',Path(t)/'kit','control06-a','control06-a',bad,'Linux')
            with self.assertRaises(ValueError): c.gate(t,Path(t)/'kit','control06-a','control06-a',env,'Linux')
            with self.assertRaises(ValueError): c.gate(Path(t)/'new',Path(t)/'kit','control06-a',None,env,'Linux')
    def test_existing_root_and_immutable_receipt(self):
        with tempfile.TemporaryDirectory() as t:
            with self.assertRaises(ValueError): c.Runner(t,t,1).setup()
            target=Path(t)/'receipt'; c.exclusive(target,{'a':1})
            with self.assertRaises(FileExistsError): c.exclusive(target,{'a':2})
    def test_m06_actual_config_fault_preserves_original(self):
        with tempfile.TemporaryDirectory() as t:
            kit=Path(t)/'kit'; kit.mkdir(); (kit/'ticketlab').write_bytes(b'fake')
            r=c.Runner(Path(t)/'root',kit,6); events=[]
            def ctl(command,*args,**kwargs):
                events.append(command)
                if command=='prepare':
                    for sub in ('data','config','state'): (r.home/sub).mkdir(parents=True,exist_ok=True)
                    (r.home/'data/tickets.json').write_text('[{"id":"old"}]')
                    (r.home/'config/service.env').write_text('TICKETLAB_DATA_DIR='+str(r.home/'data')+'\n')
            def run(args):
                self.assertEqual(args[1],'baseline')
                events.append('baseline')
                (r.home/'state/author-before.json').write_text('{"tickets":[{"id":"old"}]}')
            r.ctl=ctl; r.run=run; r.ready=lambda:None
            r.setup()
            self.assertEqual(events,['prepare','start','baseline','stop','start'])
            self.assertIn('data-empty',(r.home/'config/service.env').read_text())
            self.assertEqual((r.home/'data/tickets.json').read_text(),'[{"id":"old"}]')
            self.assertEqual(list((r.home/'data-empty').iterdir()),[])
    def fixture(self,t):
        r=c.Runner(t,Path(t)/'kit',10); (r.home/'state').mkdir(parents=True)
        before=r.home/'state/author-before.json'; before.write_text(json.dumps({'postgres_snapshot':{'rows':[]}}))
        dump=r.home/'state/backup.dump'; dump.write_bytes(b'PGDMP-test')
        (r.home/'state/backup.json').write_text(json.dumps({'instance_id':r.id,'course_id':301758,'dump_sha256':c.digest(dump),'snapshot':{'rows':[]}}))
        (r.root/'prepared.json').write_text(json.dumps({'control':r.id,'baseline_sha256':c.digest(before)}))
        return r
    def test_uncertain_post_never_retried(self):
        with tempfile.TemporaryDirectory() as t:
            r=self.fixture(t); calls=[]
            def failed(path,data=None):
                calls.append((path,data)); raise TimeoutError('unknown outcome')
            r.http=failed
            with self.assertRaises(TimeoutError): r.after_backup()
            title=json.loads((r.root/'late-intent.json').read_text())['title']
            def reconcile(path,data=None):
                self.assertIsNone(data)
                return {'total':1,'items':[{'id':'42','title':title}]} if '?' in path else {'id':'42','title':title}
            r.http=reconcile; r.after_backup()
            self.assertEqual(len(calls),1)
            done=json.loads((r.root/'after-backup.json').read_text())
            self.assertTrue(done['confirmed_utc'].endswith('Z'))
            self.assertLessEqual(done['post_attempt_started_utc'],done['confirmed_utc'])
            with self.assertRaises(ValueError): r.after_backup()
    def test_bad_barrier_zero_writes(self):
        with tempfile.TemporaryDirectory() as t:
            r=self.fixture(t); (r.home/'state/backup.dump').write_bytes(b'bad')
            r.http=lambda *args: self.fail('network reached')
            with self.assertRaises(ValueError): r.after_backup()
    def test_uncertain_missing_no_write(self):
        with tempfile.TemporaryDirectory() as t:
            r=self.fixture(t)
            r.http=lambda *args: (_ for _ in ()).throw(TimeoutError())
            with self.assertRaises(TimeoutError): r.after_backup()
            def missing(path,data=None):
                self.assertIsNone(data); return {'total':0,'items':[]}
            r.http=missing
            with self.assertRaises(ValueError): r.after_backup()
if __name__=='__main__': unittest.main()
