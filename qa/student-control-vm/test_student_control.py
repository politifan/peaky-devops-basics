import contextlib
import hashlib
import io
import json
from pathlib import Path
from types import SimpleNamespace
import stat
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import student_control as s

class Tests(unittest.TestCase):
    def test_dryrun_opaque_and_zero_effect(self):
        with patch.object(s,'execute',side_effect=AssertionError),patch.object(s.subprocess,'run',side_effect=AssertionError),patch.object(s.socket,'socket',side_effect=AssertionError),contextlib.redirect_stdout(io.StringIO()) as out:
            s.main(['prepare','6','--root','/none','--kit','/none'])
        result=json.loads(out.getvalue())
        self.assertTrue(result['dry_run']); self.assertFalse(result['assessment_completed'])
        self.assertFalse(any(word in out.getvalue() for word in ('data-empty','bad-body','bad-retry','solution','actions')))
    def test_catalog_complete_and_engine_exact(self):
        catalog=s.load_catalog(); self.assertEqual(len(catalog['kits']),1)
        self.assertEqual(set(catalog['kits'][0]['files']),{'ticketlab','labctl','labcheck','ticketlab-bad-body','ticketlab-bad-write','ticketlab-bad-retry'})
        engine=s.load_engine()
        self.assertEqual(engine.SPECS['10']['seed'],426)
        with self.assertRaises(ValueError): engine.gate('/a','/b','control10-a','control10-a',{},'Linux')
    def test_unknown_catalog_and_engine_tamper(self):
        with patch.object(s,'sha',return_value='wrong'):
            with self.assertRaises(ValueError): s.load_catalog()
    def test_paths_fresh_home_scope_and_no_overlap(self):
        with tempfile.TemporaryDirectory() as t,patch.object(s,'private_owned'):
            home=Path(t); kit=home/'kit'; kit.mkdir(); base=home/'course-control-labs'; root=base/'control06-attempt1'
            self.assertEqual(s.validate_paths(home,root,kit,6,'prepare',123),base)
            for bad in (home/'wrong',base/'../outside',base/'nested/control06-a',base/'control07-a'):
                with self.assertRaises(ValueError): s.validate_paths(home,bad,kit,6,'prepare',123)
            base.mkdir(); root.mkdir()
            with self.assertRaises(ValueError): s.validate_paths(home,root,kit,6,'prepare',123)
            with self.assertRaises(ValueError): s.validate_paths(home,root,root,6,'after-backup',123)
    def test_symlink_guard(self):
        with tempfile.TemporaryDirectory() as t:
            folder=Path(t); target=folder/'target'; target.mkdir(); link=folder/'link'
            try: link.symlink_to(target,target_is_directory=True)
            except OSError: self.skipTest('platform cannot create symlink')
            with self.assertRaises(ValueError): s.no_symlinks(link/'future')
    def test_ownership_and_private_permissions(self):
        with tempfile.TemporaryDirectory() as t,patch.object(Path,'stat',return_value=SimpleNamespace(st_uid=123,st_mode=stat.S_IFDIR|0o700)):
            s.private_owned(Path(t),123)
            with self.assertRaises(ValueError): s.private_owned(Path(t),124)
        with tempfile.TemporaryDirectory() as t,patch.object(Path,'stat',return_value=SimpleNamespace(st_uid=123,st_mode=stat.S_IFDIR|0o777)):
            with self.assertRaises(ValueError): s.private_owned(Path(t),123)
    def test_known_kit_hash_and_non_executable_refusal(self):
        with tempfile.TemporaryDirectory() as t,patch.object(s,'private_owned'):
            kit=Path(t); names=('ticketlab','labctl','labcheck','ticketlab-bad-body','ticketlab-bad-write','ticketlab-bad-retry')
            value=hashlib.sha256(b'known').hexdigest(); entry={'files':{name:value for name in names}}
            for name in names: (kit/name).write_bytes(b'known')
            with patch.object(Path,'stat',return_value=SimpleNamespace(st_mode=stat.S_IFREG|0o700)):
                s.verify_kit(kit,entry,123)
                (kit/'labctl').write_bytes(b'unknown')
                with self.assertRaises(ValueError): s.verify_kit(kit,entry,123)
            with patch.object(Path,'stat',return_value=SimpleNamespace(st_mode=stat.S_IFREG|0o600)):
                with self.assertRaises(ValueError): s.verify_kit(kit,entry,123)
    def test_vm_ack_platform_and_environment_guards(self):
        with self.assertRaises(ValueError): s.validate_environment(None)
        with patch.object(s.platform,'system',return_value='Linux'),patch.object(s.platform,'machine',return_value='x86_64'),patch.object(s.os,'getuid',return_value=123,create=True),patch.object(Path,'read_text',return_value='ID=ubuntu\nVERSION_ID="24.04"\n'),patch.dict(s.os.environ,{},clear=True):
            s.validate_environment(s.ACK)
            with patch.dict(s.os.environ,{'DOCKER_HOST':'tcp://remote'}):
                with self.assertRaises(ValueError): s.validate_environment(s.ACK)
            with patch.object(s.os,'getuid',return_value=0):
                with self.assertRaises(ValueError): s.validate_environment(s.ACK)
    def test_vm_detector_refuses_container(self):
        r=SimpleNamespace(run=lambda args:'kvm',capture=lambda args,expected:subprocess.CompletedProcess(args,1,b'none\n',b''))
        s.verify_vm(r)
        r.capture=lambda args,expected:subprocess.CompletedProcess(args,1,b'docker\n',b'')
        with self.assertRaises(ValueError): s.verify_vm(r)
    def test_busy_port_guard_without_binding_host(self):
        class Probe:
            def __enter__(self):return self
            def __exit__(self,*args):pass
            def bind(self,address):
                if address[1]==18006: raise OSError('busy')
        with patch.object(s.socket,'socket',return_value=Probe()):
            with self.assertRaises(ValueError): s.free_ports((18006,))
        self.assertEqual(s.ports(1),()); self.assertEqual(s.ports(11),(18011,18111,18211)); self.assertEqual(s.ports(10,after=True),(18110,))
    def test_existing_named_or_labelled_resource_refused(self):
        for kind in ('named','labelled','image'):
            def run(args):
                if kind=='named' and args[1]=='container' and '--filter' not in args:return 'peaky301758-control07-a-api-1'
                if kind=='labelled' and '--filter' in args:return 'foreign-name'
                if kind=='image' and args[1]=='image':return 'peaky301758-control07-a:old'
                return ''
            with self.subTest(kind=kind):
                with self.assertRaises(ValueError): s.refuse_existing_resources(SimpleNamespace(n=7,id='control07-a',run=run))
        s.refuse_existing_resources(SimpleNamespace(n=7,id='control07-a',run=lambda args:''))
    def test_child_docker_command_pins_endpoint_and_env(self):
        engine=s.load_engine(); cls=s.runner_class(engine)
        with tempfile.TemporaryDirectory() as t:
            r=cls(Path(t)/'root',Path(t)/'kit',7,Path(t),123)
            with patch.object(s.subprocess,'run',return_value=subprocess.CompletedProcess([],0,b'{}',b'')) as run:
                r.capture(['docker','inspect','owned'])
                args=run.call_args.args[0]
                self.assertEqual(args[:3],['/usr/bin/docker','--host',s.ENDPOINT])
                self.assertEqual(run.call_args.kwargs['env']['PATH'],'/usr/bin:/bin')
                self.assertNotIn('CONTROL_DIAGNOSTICS',run.call_args.kwargs['env'])
                self.assertNotIn('DOCKER_HOST',run.call_args.kwargs['env'])
    def test_local_http_disables_ambient_proxy_and_redirects(self):
        engine=s.load_engine();cls=s.runner_class(engine)
        class Response:
            status=200
            def __enter__(self):return self
            def __exit__(self,*args):pass
            def read(self,limit):return b'{"status":"ready"}'
        opener=SimpleNamespace(open=lambda request,timeout:Response())
        with tempfile.TemporaryDirectory() as t,patch.dict(s.os.environ,{'HTTP_PROXY':'http://outside.invalid:8080','http_proxy':'http://outside.invalid:8080','NO_PROXY':'','no_proxy':''}):
            r=cls(Path(t)/'root',Path(t)/'kit',10,Path(t),123)
            with patch.object(s.urllib.request,'build_opener',return_value=opener) as build:
                self.assertEqual(r.http('/ready'),{'status':'ready'})
                handlers=build.call_args.args
                self.assertEqual(handlers[0].proxies,{})
                self.assertIsNone(handlers[1].redirect_request(None,None,None,None,None,None))
                with self.assertRaises(ValueError):r.http('//outside.invalid/tickets')
    def test_issued_root_marker_and_foreign_uid(self):
        engine=s.load_engine()
        with tempfile.TemporaryDirectory() as t,patch.object(s,'private_owned'):
            root=Path(t); r=engine.Runner(root,root/'kit',10); (r.home).mkdir()
            prepared=root/'prepared.json'; prepared.write_text('{}')
            meta={'course_id':301758,'module_id':'m10','instance_id':r.id,'seed':426,'port':18010,'ownership':'peaky301758-v3'}
            (r.home/'.peaky-instance.json').write_text(json.dumps(meta))
            marker={'course':301758,'control':r.id,'uid':123,'root':str(root),'kit_id':'known','prepared_sha256':s.sha(prepared)}
            (root/'student-provision.json').write_text(json.dumps(marker))
            s.owned_marker(r,{'id':'known'},123)
            with self.assertRaises(ValueError):s.owned_marker(r,{'id':'known'},124)
            prepared.write_text('{"changed":true}')
            with self.assertRaises(ValueError):s.owned_marker(r,{'id':'known'},123)
    def test_actual_issued_label_binding_mount_and_image_guards(self):
        with tempfile.TemporaryDirectory() as t:
            root=Path(t); base=root/'control07-a'; (base/'state').mkdir(parents=True); (base/'data').mkdir()
            (base/'.peaky-instance.json').write_text(json.dumps({'port':18007}))
            (base/'state/built-image.json').write_text(json.dumps({'image_id':'sha256:known'}))
            mode={'bad':None}
            class Fake:
                def __init__(self,root,kit,n,home,uid):
                    self.root=Path(root);self.kit=Path(kit);self.n=n;self.id='control07-a';self.home_dir=Path(home);self.uid=uid
                def http(self,path):return {'instance_id':'control07-a','source_revision':'known-source','release_version':'3.0.0-good'}
                def run(self,args):
                    bad=mode['bad']; api='peaky301758-control07-a-api-1'
                    if args[1]=='network':
                        if '.Labels' in args[4]: return json.dumps({'com.docker.compose.project':'peaky301758-control07-a'})
                        return json.dumps({'id':{'Name':api},**({'foreign':{'Name':'unrelated'}} if bad=='network' else {})})
                    field=args[3]
                    if '.Config.Labels' in field: return json.dumps({'io.peaky.course':'foreign' if bad=='labels' else '301758','io.peaky.instance':'control07-a'})
                    if '.NetworkSettings.Ports' in field:return json.dumps({'18000/tcp':[{'HostIp':'0.0.0.0' if bad=='binding' else '127.0.0.1','HostPort':'18007'}]})
                    if '.Image' in field:return json.dumps('sha256:wrong' if bad=='image' else 'sha256:known')
                    if '.Mounts' in field:return json.dumps([{'Type':'bind','Destination':'/data','Source':str(root/'foreign' if bad=='mount' else base/'data'),'RW':True}])
                    raise AssertionError(args)
            r=Fake(root,root/'kit',7,root,123)
            s.verify_issued(r,{'source_commit':'known-source'})
            for bad in ('labels','binding','image','mount','network'):
                with self.subTest(bad=bad):
                    mode['bad']=bad
                    with self.assertRaises(ValueError):s.verify_issued(r,{'source_commit':'known-source'})
    def test_exclusive_intent_cannot_retry(self):
        engine=s.load_engine()
        with tempfile.TemporaryDirectory() as t:
            lock=Path(t)/'intent'; engine.exclusive(lock,{'root':'owned'})
            with self.assertRaises(FileExistsError): engine.exclusive(lock,{'root':'other'})

if __name__=='__main__': unittest.main()
