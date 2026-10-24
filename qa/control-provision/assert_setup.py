"""Hosted-CI teacher probes, no repairs or student solution.
m12 creates two diagnostic records to observe the supplied retry defect.
"""
import json
import os
import stat
import sys
import urllib.error
import urllib.request
from pathlib import Path
from control_provision import Runner,digest,plan,gate,diagnostic,exclusive,local_docker

def require(value,check):
    if not value: raise AssertionError(check)
    diagnostic({'event':'fixture-assertion','check':check,'passed':True})

def check(n,root,kit):
    r=Runner(root,kit,n); home=r.home
    gate(root,kit,r.id,r.id)
    local_docker(r)
    receipt=json.loads((root/'prepared.json').read_text())
    require(receipt['setup_only'] and receipt['actions']==plan(n),'receipt-plan')
    meta=json.loads((home/'.peaky-instance.json').read_text())
    require((meta['course_id'],meta['module_id'],meta['instance_id'],meta['seed'],meta['port'],meta['ownership'])==(301758,f'm{n:02d}',r.id,r.seed,r.port,'peaky301758-v3'),'actual-prepared-course-matrix')
    if n in (1,4,5):
        require(not list((home/'evidence').iterdir()),'student-evidence-initially-empty')
        require(meta['pid']==0,'file-only-control-no-api-pid')
    if n==1:
        notes=[p for p in (home/'inputs').glob('*.txt') if p.name!='only-owner.txt']
        require(len(notes)==1 and ' ' in notes[0].name,'m01-original-note-with-space')
        require(notes[0].read_text(encoding='utf-8')==f'Заявка T{r.seed} не сохраняется.\n','m01-original-note-seed418')
        expected_log=(f'2026-10-04T10:00:00Z INFO ticket=T{r.seed-1} created\n'
            f'2026-10-04T10:00:01Z ERROR ticket=T{r.seed} write_failed\n'
            f'2026-10-04T10:00:02Z INFO ticket=T{r.seed} retry_scheduled\n'
            f'2026-10-04T10:00:03Z WARN ticket=T{r.seed+1} slow\n')
        require((home/'inputs/service.log').read_text()==expected_log,'m01-original-service-log-T418-and-neighbors')
    if n==4:
        require((home/'config/pipeline.env').read_text()=='pipefail=off\n','m04-pipeline-initially-off')
        require((home/'inputs/codes.log').read_text()=='E_IO\nE_DNS\nE_IO\nE_DNS\n','m04-original-repeated-codes')
        result=r.capture([kit/'labcheck','intentional-failure'],expected=1)
        require(result.stdout==b'profile intentional-failure\n' and result.stderr==b'FAIL fixture.invalid\n','m04-actual-separate-streams-and-exit1')
        require(not list((home/'evidence').iterdir()),'m04-probe-did-not-write-student-evidence')
    if n==5:
        def git(*args): return r.run(['git','--no-optional-locks','-C',home/'repo',*args])
        require(git('show','HEAD:README.txt')=='TicketLab\nport=18004','m05-original-HEAD-readme')
        require(git('ls-tree','--name-only','HEAD')=='README.txt','m05-original-commit-only-readme')
        require(git('status','--porcelain','--untracked-files=all')=='?? draft.txt','m05-draft-untracked-no-staged-solution')
    baseline=None
    if receipt['baseline_sha256']:
        require(digest(home/'state/author-before.json')==receipt['baseline_sha256'],'immutable-baseline')
        baseline=json.loads((home/'state/author-before.json').read_text())
    if n in (2,3,6,7,10,11,12):
        version=r.http('/version')
        require(version['instance_id']==r.id,'live-instance-identity')
        require(version['source_revision']==os.environ['GITHUB_SHA'],'live-exact-checkout-source')
        require(version['release_version']=='3.0.0-'+('bad-body' if n==3 else 'bad-retry' if n==12 else 'good'),'live-selected-version')
    if n==2:
        require(stat.S_IMODE((home/'inputs/only-owner.txt').stat().st_mode)==0o200,'m02-write-only-owner-mode')
        pids=[]
        for instance in (r.id,r.id[:-1]+'b'):
            meta=json.loads((root/instance/'.peaky-instance.json').read_text())
            require(meta['pid']>0,'m02-positive-pid-'+instance)
            require(instance.encode() in (Path('/proc')/str(meta['pid'])/'cmdline').read_bytes().split(b'\0'),'m02-owned-live-process-'+instance)
            pids.append(meta['pid'])
        require(len(set(pids))==2,'m02-distinct-peer-pids')
        peer=Runner(root,kit,n); peer.port=r.port+100
        require(peer.http('/version')['instance_id']==r.id[:-1]+'b','m02-peer-address-identity')
    if n==3:
        require(r.http('/health')=={'status':'wrong'},'m03-observed-bad-health-body')
    if n==6:
        require((home/'config/service.env').read_text().strip()=='TICKETLAB_DATA_DIR='+str(home/'data-empty'),'m06-wrong-owned-data-config')
        original=json.loads((home/'data/tickets.json').read_text())
        for old in baseline['tickets']:
            require(any(all(row.get(k)==old[k] for k in ('id','title','status')) for row in original),'m06-old-record-preserved-original-path')
            try: r.http('/tickets/'+str(old['id']))
            except urllib.error.HTTPError as err: require(err.code==404,'m06-old-id-absent-through-wrong-config')
            else: raise AssertionError('m06 old ID unexpectedly visible')
        require(r.http('/tickets?limit=100&offset=0')['total']==0,'m06-live-empty-store')
    if n==7:
        container='peaky301758-'+r.id+'-api-1'
        def inspect(field): return json.loads(r.run(['docker','inspect','--format','{{json .'+field+'}}',container]))
        require(inspect('State.Running') is True,'m07-api-running')
        require(inspect('Id')==baseline['container_id'],'m07-original-container-identity')
        labels=inspect('Config.Labels')
        require(labels.get('io.peaky.course')=='301758' and labels.get('io.peaky.instance')==r.id,'m07-owned-container-labels')
        known=json.loads((home/'state/known-image.json').read_text())
        require(digest(home/'state/known-image.json')==receipt['known_image_sha256'],'m07-immutable-known-image-reference')
        require(inspect('Image')==known['image_id'],'m07-runtime-known-image-match')
        mounts=inspect('Mounts')
        require(any(x['Type']=='bind' and x['Destination']=='/data' and Path(x['Source']).resolve()==(home/'data').resolve() and x['RW'] for x in mounts),'m07-owned-data-bind-mount')
        require(baseline['volume']=='bind:'+str((home/'data').resolve()),'m07-baseline-records-original-bind')
        for old in baseline['tickets']:
            require(r.http('/tickets/'+str(old['id']))==old,'m07-original-baseline-ticket-readable')
    if n==8:
        db='peaky301758-'+r.id+'-db-1'
        state=json.loads(r.run(['docker','inspect','--format','{{json .State}}',db]))
        require(state['Running'] is False,'m08-db-stopped')
        mounts=json.loads(r.run(['docker','inspect','--format','{{json .Mounts}}',db]))
        volume=[x['Name'] for x in mounts if x['Type']=='volume' and x['Destination']=='/var/lib/postgresql/data']
        require(len(volume)==1,'m08-db-volume-retained')
        labels=json.loads(r.run(['docker','volume','inspect','--format','{{json .Labels}}',volume[0]]))
        require(labels.get('io.peaky.course')=='301758' and labels.get('io.peaky.instance')==r.id,'m08-owned-volume')
        require(bool(baseline['postgres_snapshot']['rows']),'m08-baseline-has-old-sql-record')
    if n==10:
        require(not (home/'state/backup.dump').exists(),'m10-no-student-backup-yet')
        require(not (root/'late-intent.json').exists(),'m10-no-late-event-before-backup')
    if n==12:
        known=json.loads((home/'state/known-image.json').read_text())
        require(digest(home/'state/known-image.json')==receipt['known_image_sha256'],'m12-known-good-reference-unchanged')
        current=r.run(['docker','inspect','--format','{{.Image}}','peaky301758-'+r.id+'-api-1'])
        retained=r.run(['docker','image','inspect','--format','{{.Id}}','peaky301758-'+r.id+':known-good'])
        require(retained==known['image_id'] and current!=retained,'m12-retained-good-differs-from-current-bad')
        marker='teacher-probe-'+receipt['baseline_sha256'][:20]
        exclusive(root/'retry-probe-intent.json',{'marker':marker,'setup_probe_only':True})
        def post():
            req=urllib.request.Request(f'http://127.0.0.1:{r.port}/tickets',data=json.dumps({'title':marker}).encode(),headers={'Content-Type':'application/json','Idempotency-Key':marker})
            with urllib.request.urlopen(req,timeout=5) as response:
                require(response.status==201,'m12-defect-new-create-on-each-attempt')
                return json.loads(response.read(65536))
        first=post(); second=post()
        require(first['id']!=second['id'],'m12-observed-duplicate-despite-same-key')
        for old in baseline['tickets']:
            require(r.http('/tickets/'+str(old['id']))==old,'m12-old-baseline-still-readable')
        exclusive(root/'retry-probe-result.json',{'ids':[first['id'],second['id']],'setup_probe_only':True})
    diagnostic({'event':'fixture-complete','control':r.id,'passed':True})

if __name__=='__main__':
    try: check(int(sys.argv[1]),Path(sys.argv[2]),Path(sys.argv[3]))
    except Exception as exc:
        diagnostic({'event':'fixture-failure','exception_type':type(exc).__name__})
        raise
