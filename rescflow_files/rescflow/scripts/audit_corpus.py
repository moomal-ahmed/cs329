#!/usr/bin/env python3
"""Manifest/source consistency only: not a Go parser, SSA analyzer, or semantic validator."""
import json,re,hashlib
from pathlib import Path
root=Path(__file__).resolve().parents[1]
labels=json.loads((root/'benchmarks/labels.json').read_text()); errors=[]; rows=[]
for c in labels:
 data=(root/c['file']).read_bytes();lines=data.decode().splitlines()
 for s in c['sites']:
  if not 1<=s['line']<=len(lines) or s['source'] not in lines[s['line']-1]:errors.append([c['id'],s])
 # Lexical counts are useful consistency checks only; comments/literals are not generally excluded.
 ac=sum('os.Open(' in l and '// RF:acquire' in l for l in lines)
 ds=sum('// RF:defer' in l for l in lines)
 rows.append(dict(id=c['id'],sha256=hashlib.sha256(data).hexdigest(),acquisition_sites=ac,defer_sites=ds,inputs=len(c['inputs'])))
result=dict(check='source manifest consistency; no execution or semantic accuracy claim',programs=len(rows),inputs=sum(r['inputs'] for r in rows),acquisition_sites=sum(r['acquisition_sites'] for r in rows),defer_sites=sum(r['defer_sites'] for r in rows),errors=errors,files=rows)
(root/'results/corpus_audit.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps({k:v for k,v in result.items() if k!='files'},indent=2))
assert not errors
