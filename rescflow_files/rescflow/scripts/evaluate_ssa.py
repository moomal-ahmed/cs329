#!/usr/bin/env python3
"""Compare saved SSA findings to draft labels; abstentions are association false negatives.
Use only after compiling and running cmd/rescflow. No source labels are fed to that analyzer.
"""
import json,sys
from pathlib import Path
root=Path(__file__).resolve().parents[1]
p=root/'results/ssa.json'
if not p.exists():
 print('BLOCKED: results/ssa.json does not exist; no precision/recall reported.');sys.exit(2)
data=json.loads(p.read_text());labels=json.loads((root/'benchmarks/labels.json').read_text())
expected={k:set() for k in ['acquire','defer','association']}
for c in labels:
 ac={s['label']:f"{c['file']}:{s['line']}" for s in c['sites'] if s['kind']=='acquire'}
 for s in c['sites']:
  loc=f"{c['file']}:{s['line']}";expected[s['kind']].add(loc)
  if s['kind']=='defer':
   resource=s['label'].split()[1]
   if resource!='none':expected['association'].add((loc,ac[resource]))
actual={k:set() for k in expected}
for s in data['sites']:
 loc=f"{s['file']}:{s['line']}"
 if s['kind'] in ['acquire','defer']:actual[s['kind']].add(loc)
 if s['kind']=='defer':
  for a in s['origins']:actual['association'].add((loc,a))
results={}
for k in expected:
 e,a=expected[k],actual[k];tp=len(e&a)
 results[k]=dict(tp=tp,fp=len(a-e),fn=len(e-a),precision=tp/len(a) if a else None,recall=tp/len(e) if e else None,missed=sorted(e-a),incorrect=sorted(a-e))
(root/'results/ssa_metrics.json').write_text(json.dumps(results,indent=2)+'\n')
print(json.dumps(results,indent=2))
