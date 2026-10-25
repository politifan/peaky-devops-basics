"""Small actual learner routes on a disposable GitHub-hosted Ubuntu VM."""
import os,sys,pathlib,subprocess,json,hashlib,tarfile,urllib.request,urllib.error,time,socket,pty,select,signal,fcntl,termios,struct

if os.environ.get('RUNNER_ENVIRONMENT')!='github-hosted' or os.environ.get('GITHUB_REF')!='refs/heads/codex/devops-finalize-20261026':
    raise SystemExit('Disposable author CI only')
if sys.platform!='linux' or os.getuid()==0:raise SystemExit('Nonroot Linux runner required')
BASE=pathlib.Path(os.environ['RUNNER_TEMP'])/('learner-final-'+os.environ['GITHUB_RUN_ID'])
BASE.mkdir(mode=0o700)
REPO=pathlib.Path.cwd(); HOME=pathlib.Path.home(); KIT=HOME/'peaky-kit'
if KIT.exists():raise SystemExit('Fresh learner kit directory required')
KIT.mkdir(mode=0o700)
REPORT=REPO/'reports/learner-finalize.json';REPORT.parent.mkdir(exist_ok=True)
env={k:v for k,v in os.environ.items() if not k.startswith(('GIT_','DOCKER_','COMPOSE_','TICKETLAB_'))}
env.update(PATH=str(KIT)+':'+env['PATH'],GIT_CONFIG_NOSYSTEM='1',GIT_CONFIG_GLOBAL=str(BASE/'gitconfig'),GIT_TERMINAL_PROMPT='0',TERM='xterm-256color',XDG_RUNTIME_DIR='/run/user/'+str(os.getuid()))
(BASE/'gitconfig').write_text('[user]\n name = Learner route\n email = learner@example.invalid\n[commit]\n gpgsign = false\n')
evidence={'scope':'actual published kit, owned disposable VM; no human-pilot claim','source_commit':os.environ['GITHUB_SHA'],'cases':[]}
def record(name,**values):
    evidence['cases'].append({'case':name,**values});REPORT.write_text(json.dumps(evidence,indent=2));print(name,values,flush=True)
def run(args,cwd=None,input=None,ok=(0,),timeout=120):
    r=subprocess.run(list(map(str,args)),cwd=cwd,env=env,input=input,capture_output=True,timeout=timeout)
    if r.returncode not in ok:raise RuntimeError(f'{args[0:4]} exit {r.returncode}: '+r.stderr.decode(errors='replace')[-1800:])
    return r.stdout.decode().strip()
def http(port,path,body=None,expected=200):
    data=None if body is None else json.dumps(body).encode()
    req=urllib.request.Request(f'http://127.0.0.1:{port}{path}',data=data,headers={'Content-Type':'application/json'})
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with opener.open(req,timeout=4) as r:status=r.status;payload=r.read()
    except urllib.error.HTTPError as r:status=r.code;payload=r.read()
    if status!=expected:raise ValueError(f'HTTP {path}: {status}, expected {expected}')
    return json.loads(payload)
def ready(port):
    for _ in range(30):
        try:
            d=http(port,'/ready')
            if d.get('status')=='ready':return
        except Exception:pass
        time.sleep(1)
    raise RuntimeError('readiness budget expired')
def free(port):
    with socket.socket() as s:s.bind(('127.0.0.1',port))

archive=BASE/'kit.tar.gz'
run(['curl','--fail','--location','--max-time','90','--output',archive,'https://github.com/politifan/peaky-devops-basics/releases/download/course-ready-20261024/peaky-devops-301758-3.0.0-linux-amd64.tar.gz'])
sha=hashlib.sha256(archive.read_bytes()).hexdigest()
if sha!='700e25a09e9f61105d2a2b6846bffaddbe5c32a0f60246ecf999c3c5d56d02e5':raise ValueError('Published kit differs')
with tarfile.open(archive) as t:
    for name in ('ticketlab','labctl','labcheck'):
        m=t.getmember(name)
        if not m.isfile() or m.size>40000000:raise ValueError('Unexpected kit entry')
        dst=KIT/name;dst.write_bytes(t.extractfile(m).read());dst.chmod(0o700)
record('published-kit',sha256=sha,version=run([KIT/'ticketlab','version']))

# Actual local remote, divergent edits, conflict and content-based resolution.
root=BASE/'labs'; root.mkdir()
run(['labctl','prepare','m05','--instance','m05-remote-a','--root',root,'--seed','417'])
repo=root/'m05-remote-a/repo'
run(['git','init','--bare','../remote.git'],cwd=repo)
run(['git','remote','add','origin','../remote.git'],cwd=repo)
run(['git','push','-u','origin','main'],cwd=repo)
run(['git','--git-dir=../remote.git','symbolic-ref','HEAD','refs/heads/main'],cwd=repo)
for who,port,extra in [('a',18005,'support=email'),('b',18006,'status=draft')]:
    run(['git','clone','../remote.git',f'../clone-{who}'],cwd=repo)
    clone=repo.parent/f'clone-{who}'
    (clone/'README.txt').write_text('service=ticketlab\nport='+str(port)+'\n')
    (clone/'EXTRA.txt').write_text(extra+'\n')
    run(['git','add','README.txt','EXTRA.txt'],cwd=clone)
    run(['git','commit','-m','Prepare '+who],cwd=clone)
a=repo.parent/'clone-a';b=repo.parent/'clone-b'
run(['git','push'],cwd=a);run(['git','fetch','origin'],cwd=b)
merge=run(['git','merge','origin/main'],cwd=b,ok=(1,))
if 'CONFLICT' not in merge:raise ValueError('Expected actual merge conflict')
(b/'README.txt').write_text('service=ticketlab\nport=18005\n')
(b/'EXTRA.txt').write_text('support=email\nstatus=draft\n')
run(['git','add','README.txt','EXTRA.txt'],cwd=b);run(['git','commit','-m','Resolve by agreed behavior'],cwd=b)
if run(['git','show','HEAD:README.txt'],cwd=b)!='service=ticketlab\nport=18005':raise ValueError('Wrong conflict result')
if run(['git','show','HEAD:EXTRA.txt'],cwd=b)!='support=email\nstatus=draft':raise ValueError('Lost independent change')
record('git-local-remote-conflict',readme_agreed=True,both_extra_fragments=True,merge_commit=run(['git','rev-parse','HEAD'],cwd=b))
run(['labctl','prepare','m05','--instance','m05-revert-a','--root',root,'--seed','417'])
r=root/'m05-revert-a/repo'; readme=r/'README.txt'
readme.write_text('service=ticketlab\nport=18005\n');run(['git','add','README.txt'],cwd=r);run(['git','commit','-m','Correct port'],cwd=r)
run(['git','tag','lab-v1'],cwd=r)
readme.write_text('service=ticketlab\nport=18999\n');run(['git','add','README.txt'],cwd=r);run(['git','commit','-m','Wrong port'],cwd=r)
run(['git','revert','--no-edit','HEAD'],cwd=r)
if readme.read_text()!='service=ticketlab\nport=18005\n':raise ValueError('Revert failed')
readme.write_text('service=ticketlab\nport=18006\n');run(['git','add','README.txt'],cwd=r);run(['git','restore','--staged','README.txt'],cwd=r)
if run(['git','diff','--cached'],cwd=r) or not run(['git','diff'],cwd=r):raise ValueError('Index/worktree distinction failed')
record('git-revert-tag-index',tag_preserved=True,revert_preserved_history=True,unstage_preserved_worktree=True)

# Send real keyboard input through a PTY. These observations concern tools, not a novice pilot.
def terminal(command,steps):
    master,slave=pty.openpty()
    fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',24,100,0,0))
    def session():
        os.setsid();fcntl.ioctl(slave,termios.TIOCSCTTY,0)
    p=subprocess.Popen(command,cwd=BASE,env=env,stdin=slave,stdout=slave,stderr=slave,preexec_fn=session);os.close(slave);output=b''
    def drain(duration):
        nonlocal output
        until=time.monotonic()+duration
        while time.monotonic()<until:
            if select.select([master],[],[],0.1)[0]:
                try:output+=os.read(master,65536)
                except OSError:break
    try:
        drain(0.8)
        for keys in steps:os.write(master,keys);drain(0.7)
        try:p.wait(timeout=8)
        except subprocess.TimeoutExpired:
            record('terminal-failure',command=command,terminal_tail=output[-4000:].decode(errors='replace'))
            raise
        drain(0.2)
        return p.returncode,output
    finally:
        if p.poll() is None:os.killpg(p.pid,signal.SIGTERM);p.wait(timeout=3)
        os.close(master)
rc,out=terminal(['nano','note.txt'],[b'learner-edited-line',b'\x0f',b'\r',b'\x18'])
if rc or (BASE/'note.txt').read_text()!='learner-edited-line\n':raise ValueError('nano keyboard save failed')
(BASE/'reading.log').write_text(''.join(f'line {n}\n' for n in range(80))+'READY_MARKER\n')
rc,out=terminal(['less','reading.log'],[b'/READY_MARKER\r',b'q'])
if rc or b'READY_MARKER' not in out:raise ValueError('less keyboard search failed')
record('interactive-nano-less',nano_saved=True,less_search_and_exit=True)

# Current lesson user unit, own fresh service and published binary.
free(18986)
unit='peaky-final-'+os.environ['GITHUB_RUN_ID']+'.service';ud=HOME/'.config/systemd/user';ud.mkdir(parents=True,exist_ok=True)
data=BASE/'systemd-data';data.mkdir()
uf=ud/unit
uf.write_text('[Unit]\nDescription=Course final user unit\n[Service]\nExecStart='+str(KIT/'ticketlab')+' serve --listen 127.0.0.1:18986 --data-dir '+str(data)+' --instance my-systemd\nRestart=on-failure\n[Install]\nWantedBy=default.target\n')
run(['systemctl','--user','daemon-reload']);run(['systemctl','--user','start',unit])
try:
    ready(18986);run(['systemctl','--user','status',unit,'--no-pager']);journal=run(['journalctl','--user','-u',unit,'-n','20','--no-pager'])
    if http(18986,'/version').get('instance_id')!='my-systemd':raise ValueError('Wrong systemd instance')
    record('user-systemd',service_active=True,owned_http_identity=True,journal_read=True)
finally:run(['systemctl','--user','stop',unit])

# Exact manual Compose pg_dump / createdb / pg_restore / run --no-deps sequence.
free(18983);free(18984)
project='peaky-final-'+os.environ['GITHUB_RUN_ID']; restored=project+'-restored-api'; dbname='ticketlab_restore_a1'
env['TICKETLAB_PORT']='18983'
run(['docker','build','-t','ticketlab:check','-f',REPO/'Dockerfile',KIT],timeout=240)
def compose(*args):return run(['docker','compose','-p',project,*args],cwd=REPO,timeout=180)
try:
    compose('up','-d','--wait','--wait-timeout','90');ready(18983)
    before=[http(18983,'/tickets',{'title':x},201) for x in ('first preserved','second preserved')]
    compose('stop','api')
    sql='SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),\'[]\'::jsonb) FROM tickets t;'
    baseline=compose('exec','-T','db','psql','-U','student','-d','ticketlab','-Atc',sql)
    dump=BASE/'ticketlab.dump'
    with dump.open('xb') as f:
        p=subprocess.run(['docker','compose','-p',project,'exec','-T','db','pg_dump','-U','student','-d','ticketlab','-Fc'],cwd=REPO,env=env,stdout=f,stderr=subprocess.PIPE,timeout=90)
    if p.returncode or dump.stat().st_size==0:raise ValueError('Manual dump failed')
    dump_sha=hashlib.sha256(dump.read_bytes()).hexdigest();compose('start','api');ready(18983)
    late=http(18983,'/tickets',{'title':'source-only after dump'},201)
    compose('exec','-T','db','createdb','-U','student',dbname)
    run(['docker','compose','-p',project,'exec','-T','db','pg_restore','-U','student','-d',dbname,'--exit-on-error'],cwd=REPO,input=dump.read_bytes())
    restored_rows=compose('exec','-T','db','psql','-U','student','-d',dbname,'-Atc',sql)
    if json.loads(restored_rows)!=json.loads(baseline):raise ValueError('Restored full rows differ')
    compose('run','--rm','--no-deps','-d','--name',restored,'-p','127.0.0.1:18984:8080','-e',f'TICKETLAB_DATABASE_URL=postgres://student:training-only-change-for-real-use@db:5432/{dbname}?sslmode=disable','api')
    ready(18984)
    for obj in before:
        if http(18984,'/tickets/'+obj['id'])!=obj or http(18983,'/tickets/'+obj['id'])!=obj:raise ValueError('Old object mismatch')
    http(18984,'/tickets/'+late['id'],expected=404);http(18983,'/tickets/'+late['id'])
    if hashlib.sha256(dump.read_bytes()).hexdigest()!=dump_sha:raise ValueError('Dump changed after snapshot')
    record('manual-postgres-restore',old_records=2,full_sql_rows_equal=True,late_source_only=True,dump_sha256=dump_sha)
finally:
    run(['docker','stop',restored],ok=(0,1));compose('down')
record('complete',passed=True)
