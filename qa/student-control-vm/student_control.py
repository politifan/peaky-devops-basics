"""Separate self-paced Ubuntu VM entrypoint; default dry-run, no reset/repair.
Local review candidate only. Existing hosted engine gate is unchanged.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import socket
import stat
import subprocess
import sys
import urllib.request

HERE=Path(__file__).resolve().parent
CATALOG_SHA256='77acc12ad031238faaeb364ee2d53cb122650afb717070cd007ae924ea44b382'
ACK='I_OWN_THIS_ISOLATED_UBUNTU_VM'
VALID=(1,2,3,4,5,6,7,8,10,11,12)
ENDPOINT='unix:///var/run/docker.sock'
BLOCKED=('DOCKER_HOST','DOCKER_CONTEXT','DOCKER_TLS_VERIFY','DOCKER_CERT_PATH','COMPOSE_FILE','COMPOSE_PROJECT_NAME','DOCKER_CONFIG','PEAKY_AUTHOR_PROBE_SSH','CONTROL_DIAGNOSTICS','BASH_ENV','ENV','LD_PRELOAD','LD_LIBRARY_PATH','GIT_DIR','GIT_WORK_TREE')
VM_TYPES={'kvm','qemu','vmware','microsoft','oracle','xen','parallels','bhyve','bochs','amazon','google','apple','acrn','powervm'}

def sha(path): return hashlib.sha256(Path(path).read_bytes()).hexdigest()
def load_catalog():
    path=HERE/'student-kit-trust.json'
    no_symlinks(path)
    if sha(path)!=CATALOG_SHA256: raise ValueError('bundled trust catalog changed; review required')
    data=json.loads(path.read_text(encoding='utf-8'))
    if data['schema']!=1: raise ValueError('unsupported trust catalog')
    for name,value in data['engine_files'].items():
        no_symlinks(HERE/name)
        if sha(HERE/name)!=value: raise ValueError('reviewed provision engine changed')
    return data
def load_engine():
    spec=importlib.util.spec_from_file_location('student_scoped_provision_engine',HERE/'engine/control_provision.py')
    engine=importlib.util.module_from_spec(spec); spec.loader.exec_module(engine)
    return engine

def no_symlinks(path):
    path=Path(path)
    if any(x.is_symlink() for x in (path,*path.parents)): raise ValueError('symlink path refused')
def private_owned(path,uid,private=True):
    no_symlinks(path)
    info=path.stat()
    if info.st_uid!=uid or info.st_mode&0o022 or (private and info.st_mode&0o077): raise ValueError('path ownership/permissions refused')
def validate_paths(home,root,kit,n,verb,uid):
    home,root,kit=Path(home),Path(root),Path(kit)
    if not all(x.is_absolute() for x in (home,root,kit)): raise ValueError('absolute paths required')
    for path in (home,root,kit): no_symlinks(path)
    base=home/'course-control-labs'
    if root.parent!=base or not re.fullmatch(f'control{n:02d}-[a-z0-9][a-z0-9-]{{0,40}}',root.name): raise ValueError('fresh named root must be directly under HOME/course-control-labs')
    private_owned(home,uid,private=False)
    if base.exists(): private_owned(base,uid)
    if not kit.is_dir() or base==kit or base in kit.parents or kit==home or kit in root.parents: raise ValueError('kit location overlaps control storage')
    private_owned(kit,uid,private=False)
    if verb=='prepare' and root.exists(): raise ValueError('existing root refused, including a partial attempt')
    if verb=='after-backup':
        if n!=10 or not root.is_dir(): raise ValueError('after-backup requires an issued m10 root')
        private_owned(root,uid)
        for path in root.rglob('*'):
            if path.is_symlink(): raise ValueError('issued root contains a symlink')
    return base
def verify_kit(kit,entry,uid):
    for name,value in entry['files'].items():
        path=Path(kit)/name
        private_owned(path,uid,private=False)
        if not stat.S_ISREG(path.stat().st_mode) or not path.stat().st_mode&0o111 or sha(path)!=value: raise ValueError('known kit member/hash mismatch')
    if len({entry['files'][name] for name in ('ticketlab','labctl','labcheck')})!=1: raise ValueError('catalog multicall identity mismatch')
def validate_environment(ack):
    if ack!=ACK: raise ValueError('explicit isolated personal VM acknowledgement required')
    if platform.system()!='Linux' or platform.machine().lower() not in ('x86_64','amd64') or os.getuid()==0: raise ValueError('non-root Linux amd64 user required')
    release={}
    for line in Path('/etc/os-release').read_text().splitlines():
        key,sep,value=line.partition('=')
        if sep: release[key]=value.strip('"')
    if release.get('ID')!='ubuntu' or release.get('VERSION_ID')!='24.04': raise ValueError('dedicated Ubuntu24.04 VM required')
    if any(os.environ.get(key) for key in BLOCKED): raise ValueError('environment override refused')

def runner_class(engine):
    class StudentRunner(engine.Runner):
        def __init__(self,root,kit,n,home,uid):
            super().__init__(root,kit,n)
            self.home_dir=Path(home); self.uid=uid
            self.child_env={'PATH':'/usr/bin:/bin','HOME':str(home),'LANG':'C.UTF-8','LC_ALL':'C.UTF-8','GIT_CONFIG_NOSYSTEM':'1','GIT_CONFIG_GLOBAL':'/dev/null','GIT_CONFIG_SYSTEM':'/dev/null','GIT_TERMINAL_PROMPT':'0'}
        def http(self,path,data=None):
            if not isinstance(path,str) or not path.startswith('/') or path.startswith('//') or not 1024<=self.port<=65535: raise ValueError('only explicit local fixture paths/ports are allowed')
            class NoRedirect(urllib.request.HTTPRedirectHandler):
                def redirect_request(self,*args,**kwargs): return None
            request=urllib.request.Request(f'http://127.0.0.1:{self.port}'+path,data=None if data is None else json.dumps(data).encode('utf-8'),headers={'Content-Type':'application/json'})
            opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
            with opener.open(request,timeout=5) as response:
                if response.status not in (200,201): raise ValueError('unexpected local fixture status')
                return json.loads(response.read(1048576))
        def capture(self,args,expected=0):
            for path in (self.root,self.kit): no_symlinks(path)
            args=[str(x) for x in args]
            tool=Path(args[0]).name
            if tool=='docker':
                args[0]='/usr/bin/docker'
                if len(args)<2 or args[1]!='context': args[1:1]=['--host',ENDPOINT]
            elif tool in ('git','systemd-detect-virt'):
                args[0]='/usr/bin/'+tool
            elif Path(args[0]).parent!=self.kit or tool not in ('ticketlab','labctl','labcheck'):
                raise ValueError('unexpected provision command')
            result=subprocess.run(args,capture_output=True,timeout=900,env=self.child_env,shell=False)
            if result.returncode!=expected: raise RuntimeError('preparation operation failed; partial state retained; no automatic retry')
            return result
    return StudentRunner

def verify_vm(r):
    if r.run(['systemd-detect-virt','--vm']) not in VM_TYPES: raise ValueError('VM could not be established; no container/physical-host fallback')
    result=r.capture(['systemd-detect-virt','--container'],expected=1)
    if result.stdout.decode().strip() not in ('','none'): raise ValueError('container execution refused')
def ports(n,after=False):
    if after: return (18110,)
    if n in (1,4,5): return ()
    return {2:(18002,18102),10:(18010,18110),11:(18011,18111,18211)}.get(n,(18000+n,))
def free_ports(values):
    for port in values:
        with socket.socket(socket.AF_INET,socket.SOCK_STREAM) as probe:
            try: probe.bind(('127.0.0.1',port))
            except OSError as exc: raise ValueError('required loopback port already occupied') from exc

def instances(r): return [r.id,r.id[:-1]+'b'] if r.n in (2,11) else [r.id]
def refuse_existing_resources(r):
    if r.n<7: return
    for instance in instances(r):
        prefix='peaky301758-'+instance
        for kind,fmt in (('container','{{.Names}}'),('network','{{.Name}}'),('volume','{{.Name}}')):
            args=['docker',kind,'ls']+(['-a'] if kind=='container' else [])
            named=r.run([*args,'--format',fmt]).splitlines()
            if any(name==prefix or name.startswith(prefix+'-') or name.startswith(prefix+'_') for name in named): raise ValueError('existing target resource refused, regardless of owner label')
            labelled=r.run([*args,'--filter','label=io.peaky.instance='+instance,'--format',fmt])
            if labelled: raise ValueError('pre-existing instance label refused')
        images=r.run(['docker','image','ls','--format','{{.Repository}}:{{.Tag}}']).splitlines()
        if any(x.startswith(prefix+':') for x in images): raise ValueError('existing target image tag refused')

def owned_marker(r,entry,uid):
    marker_path=r.root/'student-provision.json'
    private_owned(marker_path,uid,private=False)
    marker=json.loads(marker_path.read_text())
    if marker.get('course')!=301758 or marker.get('control')!=r.id or marker.get('uid')!=uid or marker.get('root')!=str(r.root) or marker.get('kit_id')!=entry['id'] or marker.get('prepared_sha256')!=sha(r.root/'prepared.json'):
        raise ValueError('issued root ownership/provenance mismatch')
    for instance in instances(r):
        path=r.root/instance/'.peaky-instance.json'; private_owned(path,uid,private=False)
        meta=json.loads(path.read_text())
        expected_port=r.port if instance==r.id else r.port+100
        if (meta.get('course_id'),meta.get('module_id'),meta.get('instance_id'),meta.get('seed'),meta.get('port'),meta.get('ownership'))!=(301758,f'm{r.n:02d}',instance,r.seed,expected_port,'peaky301758-v3'): raise ValueError('instance ownership mismatch')
    return marker

def verify_issued(r,entry):
    if r.n in (1,4,5): return
    expected_release='3.0.0-'+('bad-body' if r.n==3 else 'bad-retry' if r.n==12 else 'good')
    for instance in instances(r):
        base=r.root/instance; meta=json.loads((base/'.peaky-instance.json').read_text())
        probe=type(r)(r.root,r.kit,r.n,r.home_dir,r.uid); probe.port=meta['port']
        version=probe.http('/version')
        if version.get('instance_id')!=instance or version.get('source_revision')!=entry['source_commit'] or version.get('release_version')!=expected_release: raise ValueError('running instance identity mismatch')
        if r.n<7:
            pid=meta['pid']
            if pid<1 or instance.encode() not in (Path('/proc')/str(pid)/'cmdline').read_bytes().split(b'\0'): raise ValueError('owned live process mismatch')
            continue
        project='peaky301758-'+instance; name=project+'-api-1'
        def inspect(field,target=name): return json.loads(r.run(['docker','inspect','--format','{{json .'+field+'}}',target]))
        labels=inspect('Config.Labels')
        if labels.get('io.peaky.course')!='301758' or labels.get('io.peaky.instance')!=instance: raise ValueError('API ownership labels mismatch')
        bound=inspect('NetworkSettings.Ports')
        mappings=[item for values in bound.values() if values for item in values]
        if mappings!=[{'HostIp':'127.0.0.1','HostPort':str(meta['port'])}]: raise ValueError('API must bind only assigned loopback port')
        built=json.loads((base/'state/built-image.json').read_text())
        if inspect('Image')!=built['image_id']: raise ValueError('running image differs from observed build')
        if r.n in (7,12):
            mounts=inspect('Mounts')
            if not any(x['Type']=='bind' and x['Destination']=='/data' and Path(x['Source']).resolve()==(base/'data').resolve() and x['RW'] for x in mounts): raise ValueError('owned data mount mismatch')
        else:
            db=project+'-db-1'; db_labels=inspect('Config.Labels',db)
            if db_labels.get('io.peaky.course')!='301758' or db_labels.get('io.peaky.instance')!=instance: raise ValueError('DB ownership labels mismatch')
            db_ports=inspect('NetworkSettings.Ports',db)
            if any(db_ports.values()): raise ValueError('DB must not publish a host port')
            mounts=inspect('Mounts',db); volume=[x['Name'] for x in mounts if x['Type']=='volume' and x['Destination']=='/var/lib/postgresql/data']
            if len(volume)!=1: raise ValueError('DB volume identity mismatch')
            labels=json.loads(r.run(['docker','volume','inspect','--format','{{json .Labels}}',volume[0]]))
            if labels.get('io.peaky.course')!='301758' or labels.get('io.peaky.instance')!=instance: raise ValueError('DB volume ownership mismatch')
        network=project+'_default'
        labels=json.loads(r.run(['docker','network','inspect','--format','{{json .Labels}}',network]))
        if labels.get('com.docker.compose.project')!=project: raise ValueError('network project ownership mismatch')
        attached=json.loads(r.run(['docker','network','inspect','--format','{{json .Containers}}',network]))
        allowed={name,project+'-db-1'}
        if name not in {x['Name'] for x in attached.values()} or any(x['Name'] not in allowed for x in attached.values()): raise ValueError('unexpected network attachment')

def execute(args):
    validate_environment(args.isolated_vm_ack)
    catalog=load_catalog(); engine=load_engine()
    matches=[x for x in catalog['kits'] if x['id']==args.kit_id]
    if len(matches)!=1: raise ValueError('kit id is not in the bundled reviewed catalog')
    import pwd
    uid=os.getuid(); home=Path(pwd.getpwuid(uid).pw_dir)
    if home!=Path.home(): raise ValueError('HOME differs from the account home directory')
    entry=matches[0]; root=Path(args.root); kit=Path(args.kit)
    base=validate_paths(home,root,kit,args.control,args.verb,uid)
    verify_kit(kit,entry,uid)
    r=runner_class(engine)(root,kit,args.control,home,uid)
    verify_vm(r)
    if args.control>=7: engine.local_docker(r)
    if args.verb=='prepare':
        free_ports(ports(args.control)); refuse_existing_resources(r)
        base.mkdir(mode=0o700,exist_ok=True); private_owned(base,uid)
        # Exclusive per-control intent prevents competing roots/uncertain retries.
        lock=base/(f'.control{args.control:02d}-provision-intent.json')
        engine.exclusive(lock,{'course':301758,'control':r.id,'uid':uid,'root':str(root),'kit_id':entry['id'],'created_utc':engine.utc_now(),'scope':'prepare only; no reset or repair'})
        validate_paths(home,root,kit,args.control,args.verb,uid)
        if args.control>=7: engine.preload(r,args.control)
        r.setup()
        verify_issued(r,entry)
        engine.exclusive(root/'student-provision.json',{'course':301758,'control':r.id,'uid':uid,'root':str(root),'kit_id':entry['id'],'source_commit':entry['source_commit'],'prepared_sha256':sha(root/'prepared.json'),'created_utc':engine.utc_now(),'scope':'initial assessment setup, not completion'})
    else:
        owned_marker(r,entry,uid); verify_issued(r,entry); free_ports(ports(10,after=True)); r.ready()
        r.after_backup()  # Exactly-one durable intent; unknown outcome is reconcile-only.
    print(json.dumps({'prepared':True,'control':args.control,'root':str(root),'scope':'initial setup' if args.verb=='prepare' else 'after-backup continuation','assessment_completed':False}))

def main(argv=None):
    p=argparse.ArgumentParser(description='Prepare only the current control in your isolated Ubuntu VM; does not repair or grade it')
    p.add_argument('verb',choices=('prepare','after-backup')); p.add_argument('control',type=int)
    p.add_argument('--root',required=True); p.add_argument('--kit',required=True); p.add_argument('--kit-id',default='basic-quality-5e602022')
    p.add_argument('--execute',action='store_true'); p.add_argument('--isolated-vm-ack')
    args=p.parse_args(argv)
    if args.control not in VALID or (args.verb=='after-backup' and args.control!=10): raise ValueError('unsupported local control operation')
    if not args.execute:
        print(json.dumps({'dry_run':True,'control':args.control,'operation':args.verb,'creates_fresh_assessment_workspace':args.verb=='prepare','requires':'own isolated non-root Ubuntu24.04 amd64 VM, reviewed kit, fresh private HOME/course-control-labs root; Docker only from control07','assessment_completed':False})); return
    execute(args)
if __name__=='__main__':
    try: main()
    except ValueError as exc:
        print(str(exc)+'. No reset or automatic retry of existing/partial state.',file=sys.stderr)
        raise SystemExit(64)
    except Exception:
        print('Preparation stopped. Existing/partial state is retained; no reset or automatic retry. Review the current control workspace before continuing.',file=sys.stderr)
        raise SystemExit(64)
