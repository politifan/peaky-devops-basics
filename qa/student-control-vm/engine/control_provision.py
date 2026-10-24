"""Teacher setup only. Default prints a plan; execution is hosted-disposable-CI only."""
import argparse
from datetime import datetime,timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import time
import urllib.request

PROFILES = {6:'m06-service',7:'m07-persistence',8:'m08-compose',10:'m10-restore',11:'m11-project',12:'m12-retry'}
SPECS=json.loads(Path(__file__).with_name('control-matrix.json').read_text(encoding='utf-8'))['controls']
def utc_now(): return datetime.now(timezone.utc).isoformat().replace('+00:00','Z')
BLOCKED = ('DOCKER_HOST','DOCKER_CONTEXT','DOCKER_TLS_VERIFY','DOCKER_CERT_PATH','COMPOSE_FILE','PEAKY_AUTHOR_PROBE_SSH')
IMAGES = ('ubuntu@sha256:534baea6a22c03a63003dbc8dbe78fe34bc0d7e595d9a9dc9834884ff530eb55',
          'postgres@sha256:1a6ab3f5345eb6dbe04a1349529caabdb0ab09293a09590fad07b2246bfa4b54')
def diagnostic(event):
    """Allowlisted metadata only: never captured stdout/stderr, env, SQL or dump."""
    target=os.environ.get('CONTROL_DIAGNOSTICS')
    if target:
        with Path(target).open('a',encoding='utf-8') as f:
            f.write(json.dumps(event,sort_keys=True)+'\n')
def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()
def exclusive(path, value):
    path = Path(path)
    with path.open('x', encoding='utf-8') as f:
        json.dump(value, f, ensure_ascii=False, indent=2)
        f.flush()
        os.fsync(f.fileno())
    path.chmod(0o444)
    if os.name == 'posix':
        fd=os.open(path.parent,os.O_RDONLY)
        try: os.fsync(fd)
        finally: os.close(fd)
def plan(n):
    if n not in range(1,13) or n == 9:
        raise ValueError('m09 is a real fork/Actions control, not a local runtime fixture')
    actions = ['prepare']
    if n in (2,11): actions += ['prepare-peer']
    if n == 3: actions += ['select-bad-body']
    if n in (2,3,6,7,8,10,11,12): actions += ['start']
    if n in (2,11): actions += ['start-peer']
    if n in (8,10,11): actions += ['migrate']
    if n == 11: actions += ['ci-config']
    if n in PROFILES: actions += ['baseline']
    if n == 6: actions += ['stop','config-data-empty','start']
    if n == 8: actions += ['stop-db-only']
    if n == 12: actions += ['stop','select-bad-retry','start']
    return actions
def gate(root, kit, control, confirm, env=None, system=None):
    env = os.environ if env is None else env
    if confirm != control: raise ValueError('explicit matching --confirm-control required')
    if (system or platform.system()) != 'Linux' or env.get('GITHUB_ACTIONS') != 'true' or env.get('RUNNER_ENVIRONMENT') != 'github-hosted':
        raise ValueError('execution requires disposable GitHub-hosted Linux runner')
    if any(env.get(k) for k in BLOCKED): raise ValueError('remote/overridden Docker environment refused')
    temp = Path(env['RUNNER_TEMP']).resolve()
    root, kit = Path(root), Path(kit)
    if not root.is_absolute() or not kit.is_absolute(): raise ValueError('absolute paths required')
    if root == temp or temp not in root.resolve().parents: raise ValueError('root must be a new child of RUNNER_TEMP')
    if root.resolve() in kit.resolve().parents or kit.resolve() in root.resolve().parents or root.resolve() == kit.resolve(): raise ValueError('kit/root overlap')
    for p in (root, kit):
        if any(q.is_symlink() for q in (p,*p.parents)): raise ValueError('symlink path refused')

def local_docker(r):
    if r.run(['docker','context','show'])!='default': raise ValueError('non-default Docker context')
    endpoint=json.loads(r.run(['docker','context','inspect','default']))[0]['Endpoints']['docker']['Host']
    if endpoint!='unix:///var/run/docker.sock': raise ValueError('nonlocal Docker endpoint')

def preload(r,n):
    if n>=7:
        for image in IMAGES if n in (8,10,11) else IMAGES[:1]:
            r.run(['docker','pull',image])
            refs=json.loads(r.run(['docker','image','inspect','--format','{{json .RepoDigests}}',image]))
            if image not in refs: raise ValueError('pulled image digest mismatch')
            diagnostic({'event':'pinned-image','reference':image,'verified':True})

class Runner:
    def __init__(self, root, kit, n):
        self.root,self.kit,self.n = Path(root),Path(kit),n
        spec=SPECS[str(n)]
        self.id=spec['instance']; self.port=spec['port']
        self.home=self.root/self.id; self.seed=spec['seed']
    def capture(self, args, expected=0):
        result=subprocess.run([str(x) for x in args], capture_output=True, timeout=900)
        diagnostic({'event':'command','tool':Path(args[0]).name,'operation':str(args[1]) if len(args)>1 else '', 'exit_code':result.returncode,'stdout_sha256':hashlib.sha256(result.stdout).hexdigest(),'stderr_sha256':hashlib.sha256(result.stderr).hexdigest()})
        if result.returncode!=expected: raise RuntimeError('setup command failed: '+str(args[0])+' (output withheld; inspect disposable runner)')
        return result
    def run(self, args):
        return self.capture(args).stdout.decode().strip()
    def ctl(self, command, *extra, peer=False):
        return self.run([self.kit/'labctl',command,*extra,'--instance',self.id[:-1]+'b' if peer else self.id,'--root',self.root])
    def compose(self,*args):
        return self.run(['docker','compose','--project-directory',self.home,'-p','peaky301758-'+self.id,*args])
    def http(self,path, data=None):
        req=urllib.request.Request(f'http://127.0.0.1:{self.port}'+path, data=None if data is None else json.dumps(data).encode(),headers={'Content-Type':'application/json'})
        with urllib.request.urlopen(req,timeout=5) as r:
            if r.status not in (200,201): raise ValueError('unexpected HTTP status')
            return json.loads(r.read(1048576))
    def ready(self):
        for _ in range(60):
            try:
                if self.http('/ready').get('status')=='ready': return
            except Exception: pass
            time.sleep(.5)
        raise RuntimeError('readiness deadline')
    def setup(self):
        if self.root.exists(): raise ValueError('existing root refused; partial setup requires review, never automatic retry')
        self.root.mkdir(mode=0o700)
        exclusive(self.root/'setup-intent.json',{'control':self.id,'actions':plan(self.n)})
        for action in plan(self.n):
            if action.startswith('prepare'):
                peer=action=='prepare-peer'
                self.ctl('prepare',f'm{self.n:02d}','--seed',str(self.seed),'--port',str(self.port+100 if peer else self.port),peer=peer)
            elif action in ('start','stop'): self.ctl(action)
            elif action=='start-peer': self.ctl('start',peer=True)
            elif action.startswith('select-'): self.ctl('select-release','--release',action[7:])
            elif action=='migrate': self.compose('exec','-T','api','ticketlab','migrate','--apply'); self.ready()
            elif action=='ci-config':
                revision=json.loads((self.home/'state/release.json').read_text())['source_revision']
                (self.home/'config/ci.env').write_text('mandatory-check=on\nartifact-source='+revision+'\n')
            elif action=='baseline':
                self.ready()
                args=[self.kit/'labcheck','baseline',PROFILES[self.n],'--seed',str(self.seed),'--instance',self.id,'--base-url',f'http://127.0.0.1:{self.port}','--state',self.home/'state/author-before.json']
                if self.n in (7,12): args+=['--container','peaky301758-'+self.id+'-api-1']
                if self.n in (8,10,11): args+=['--postgres-container','peaky301758-'+self.id+'-db-1']
                self.run(args)
                (self.home/'state/author-before.json').chmod(0o444)
            elif action=='config-data-empty':
                empty=self.home/'data-empty'; empty.mkdir()
                (self.home/'config/service.env').write_text('TICKETLAB_DATA_DIR='+str(empty)+'\n')
            elif action=='stop-db-only': self.compose('stop','db')
        baseline=self.home/'state/author-before.json'
        known=self.home/'state/known-image.json'
        exclusive(self.root/'prepared.json',{'control':self.id,'baseline_sha256':digest(baseline) if baseline.exists() else None,'known_image_sha256':digest(known) if known.exists() else None,'kit_sha256':digest(self.kit/'ticketlab'),'setup_only':True,'actions':plan(self.n)})
        diagnostic({'event':'prepared','control':self.id,'receipt_sha256':digest(self.root/'prepared.json'),'baseline_sha256':digest(baseline) if baseline.exists() else None,'known_image_sha256':digest(known) if known.exists() else None,'kit_sha256':digest(self.kit/'ticketlab')})
    def after_backup(self):
        if self.n != 10: raise ValueError('after-backup is m10 only')
        receipt=json.loads((self.root/'prepared.json').read_text())
        before=self.home/'state/author-before.json'; dump=self.home/'state/backup.dump'
        backup=json.loads((self.home/'state/backup.json').read_text())
        if receipt['control']!=self.id or digest(before)!=receipt['baseline_sha256'] or backup['instance_id']!=self.id or backup['course_id']!=301758 or digest(dump)!=backup['dump_sha256'] or dump.read_bytes()[:5]!=b'PGDMP': raise ValueError('backup/baseline barrier mismatch')
        baseline=json.loads(before.read_text())
        if backup['snapshot'] != baseline['postgres_snapshot']: raise ValueError('backup snapshot differs from author baseline; review before late event')
        bound={'baseline_sha256':digest(before),'backup_sha256':digest(dump),'title':'author-late-'+digest(dump)[:24]}
        intent=self.root/'late-intent.json'; done=self.root/'after-backup.json'
        if done.exists(): raise ValueError('completed late event cannot be repeated')
        if intent.exists():
            previous=json.loads(intent.read_text())
            if any(previous.get(k)!=v for k,v in bound.items()): raise ValueError('late intent binding mismatch')
            attempt_utc=previous['post_attempt_started_utc']
            page=self.http('/tickets?limit=100&offset=0')
            if page['total']>100: raise ValueError('reconciliation incomplete; manual review')
            matches=[x for x in page['items'] if x['title']==bound['title']]
            if len(matches)!=1: raise ValueError('uncertain POST: zero/multiple matches; no retry permitted')
            ticket=self.http('/tickets/'+str(matches[0]['id']))
        else:
            attempt_utc=utc_now()
            exclusive(intent,{**bound,'post_attempt_started_utc':attempt_utc})  # durable intent precedes exactly one write
            ticket=self.http('/tickets',{'title':bound['title']})
            ticket=self.http('/tickets/'+str(ticket['id']))
        if ticket['title']!=bound['title']: raise ValueError('late ticket mismatch')
        exclusive(done,{**bound,'id':ticket['id'],'source_only':True,'post_attempt_started_utc':attempt_utc,'confirmed_utc':utc_now(),'time_semantics':'attempt start and subsequent GET confirmation; not an asserted database creation timestamp'})

def main(argv=None):
    p=argparse.ArgumentParser(); p.add_argument('control',type=int); p.add_argument('--root',required=True); p.add_argument('--kit',required=True)
    p.add_argument('--execute',action='store_true'); p.add_argument('--confirm-control'); p.add_argument('--kit-sha256'); p.add_argument('--after-backup',action='store_true')
    a=p.parse_args(argv); actions=plan(a.control)
    if not a.execute: print(json.dumps({'dry_run':True,'actions': ['verify-backup-barrier','late-event-once'] if a.after_backup else actions})); return
    gate(a.root,a.kit,f'control{a.control:02d}-a',a.confirm_control)
    if not a.kit_sha256 or digest(Path(a.kit)/'ticketlab')!=a.kit_sha256: raise ValueError('explicit verified kit digest required')
    for binary in ('labctl','labcheck'):
        if digest(Path(a.kit)/binary)!=a.kit_sha256: raise ValueError('multicall binary digest mismatch')
    r=Runner(a.root,a.kit,a.control)
    local_docker(r)
    if not a.after_backup: preload(r,a.control)
    r.after_backup() if a.after_backup else r.setup()
if __name__=='__main__':
    try: main()
    except Exception as exc:
        diagnostic({'event':'failure','exception_type':type(exc).__name__})
        raise
