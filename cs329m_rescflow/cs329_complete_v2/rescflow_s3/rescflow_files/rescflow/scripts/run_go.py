#!/usr/bin/env python3
"""Compile each original separately; save observed stdout/stderr and compare to draft oracles."""
import json,shutil,subprocess,tempfile,sys,platform
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
def run(cmd,cwd):
 try:
  p=subprocess.run(cmd,cwd=cwd,capture_output=True,text=True,timeout=90)
  return dict(command=list(map(str,cmd)),returncode=p.returncode,stdout=p.stdout,stderr=p.stderr)
 except (OSError,subprocess.TimeoutExpired) as e:return dict(command=list(map(str,cmd)),error=str(e))
def main():
 labels=json.loads((ROOT/'benchmarks/labels.json').read_text()); rows=[]
 go=shutil.which('go')
 env=dict(python=sys.version,platform=platform.platform(),go=run([go,'version'],ROOT) if go else None)
 with tempfile.TemporaryDirectory(prefix='rescflow-') as temp:
  tmp=Path(temp);inp=tmp/'input.txt';inp.write_text('resource fixture\n')
  for c in labels:
   if not go:
    rows.append(dict(id=c['id'],status='blocked_environment',reason='go executable not found',runs=[]));continue
   binary=tmp/('case'+c['id']);build=run([go,'build','-o',str(binary),str(ROOT/c['file'])],ROOT)
   row=dict(id=c['id'],build=build,runs=[])
   if build.get('returncode')!=0:row['status']='go_build_failed'
   else:
    for t in c['inputs']:
     r=run([str(binary),str(inp),t['mode']],tmp)
     r['mode']=t['mode'];r['oracle_match']=(r.get('returncode')==t['expected_exit'] and r.get('stdout','').splitlines()==t['expected_stdout'] and not r.get('stderr'))
     row['runs'].append(r)
    row['status']='oracle_pass' if all(r['oracle_match'] for r in row['runs']) else 'oracle_mismatch'
   rows.append(row)
 result=dict(environment=env,programs=rows)
 (ROOT/'results/go_runs.json').write_text(json.dumps(result,indent=2)+'\n')
 print(json.dumps({s:sum(r['status']==s for r in rows) for s in sorted({r['status'] for r in rows})}))
 return 0 if all(r['status']=='oracle_pass' for r in rows) else 2
if __name__=='__main__':sys.exit(main())
