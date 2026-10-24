"""Separate post-initial-green CI candidate: backup simulation + teacher barrier.
Never restores, exports a dump, repairs a fault or runs on a workstation.
"""
import argparse
from datetime import datetime,timezone
import json
from pathlib import Path
from control_provision import Runner,gate,local_docker,digest,diagnostic

def utc(value):
    parsed=datetime.fromisoformat(value.replace('Z','+00:00'))
    if parsed.tzinfo is None or parsed.utcoffset()!=timezone.utc.utcoffset(parsed): raise ValueError('UTC timestamp required')
    return parsed

def exercise(root,kit,confirm):
    gate(root,kit,'control10-a',confirm)
    r=Runner(root,kit,10); local_docker(r)
    receipt=json.loads((r.root/'prepared.json').read_text())
    if receipt['control']!=r.id: raise ValueError('prepared control identity mismatch')
    for binary in ('ticketlab','labctl','labcheck'):
        if digest(r.kit/binary)!=receipt['kit_sha256']: raise ValueError('prepared multicall kit identity mismatch')
    baseline_path=r.home/'state/author-before.json'
    if digest(baseline_path)!=receipt['baseline_sha256']: raise ValueError('baseline changed before backup')
    baseline=json.loads(baseline_path.read_text())
    if any((r.home/'state'/name).exists() for name in ('backup.dump','backup.json')) or (r.root/'late-intent.json').exists(): raise ValueError('fresh initial-green fixture required; no automatic retry')
    version=r.http('/version')
    if version['instance_id']!=r.id or version['release_version']!='3.0.0-good': raise ValueError('source identity mismatch')
    r.ctl('backup')  # Test simulation, not part of initial provision.
    r.ready()
    dump=r.home/'state/backup.dump'; before_dump=digest(dump)
    backup=json.loads((r.home/'state/backup.json').read_text())
    if utc(baseline['created_utc'])>utc(backup['created_utc']): raise ValueError('backup precedes baseline')
    r.after_backup()
    result=json.loads((r.root/'after-backup.json').read_text())
    timeline=[utc(x) for x in (baseline['created_utc'],backup['created_utc'],result['post_attempt_started_utc'],result['confirmed_utc'])]
    if timeline!=sorted(timeline): raise ValueError('baseline/backup/late event UTC ordering mismatch')
    for collection in (baseline['tickets'],baseline['postgres_snapshot']['rows'],backup['snapshot']['rows']):
        if any(str(row['id'])==str(result['id']) or row['title']==result['title'] for row in collection): raise ValueError('late identity already present before late event')
    actual=r.http('/tickets/'+str(result['id']))
    if str(actual['id'])!=str(result['id']) or actual['title']!=result['title'] or digest(dump)!=before_dump: raise ValueError('source event or preserved backup mismatch')
    for old in baseline['tickets']:
        if r.http('/tickets/'+str(old['id']))!=old: raise ValueError('old source record changed after late event')
    if digest(baseline_path)!=receipt['baseline_sha256']: raise ValueError('baseline changed')
    diagnostic({'event':'m10-backup-barrier-harness','passed':True,'dump_sha256':before_dump,'baseline_sha256':receipt['baseline_sha256'],'baseline_utc':baseline['created_utc'],'backup_utc':backup['created_utc'],'post_attempt_started_utc':result['post_attempt_started_utc'],'confirmed_utc':result['confirmed_utc'],'late_absent_before_backup':True,'old_source_records_retained':True})

def main(argv=None):
    p=argparse.ArgumentParser()
    p.add_argument('--root',required=True); p.add_argument('--kit',required=True)
    p.add_argument('--execute',action='store_true'); p.add_argument('--confirm-control')
    a=p.parse_args(argv)
    if not a.execute:
        print(json.dumps({'dry_run':True,'actions':['verify-prepared-m10','simulate-student-backup','teacher-after-backup-barrier','verify-source-late-id'],'restore':False,'upload_dump':False})); return
    exercise(a.root,a.kit,a.confirm_control)

if __name__=='__main__':
    try: main()
    except Exception as exc:
        diagnostic({'event':'m10-barrier-failure','exception_type':type(exc).__name__})
        raise
