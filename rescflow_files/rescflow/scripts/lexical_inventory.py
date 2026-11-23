#!/usr/bin/env python3
"""Dependency-free source inventory, not a type-resolved Go/SSA analyzer.
Ignores comments and literals. Recognizes literal os.Open/OpenFile/Create/CreateTemp and
all defer keywords. Does not resolve import aliases, shadowing, or resource associations.
It never consumes RF annotations. Counts are evaluated against separately supplied labels.
"""
import json,re
from pathlib import Path
TOKEN=re.compile(r'(?P<space>\s+)|(?P<comment>//[^\n]*|/\*[\s\S]*?\*/)|(?P<string>"(?:\\.|[^"\\])*"|`[^`]*`|\x27(?:\\.|[^\x27\\])*\x27)|(?P<ident>[A-Za-z_][A-Za-z_0-9]*)|(?P<other>.)')
def scan(source):
 tokens=[];line=1
 for m in TOKEN.finditer(source):
  s=m.group();kind=m.lastgroup
  if kind not in ['space','comment']:tokens.append((s if kind!='string' else '<literal>',line))
  line+=s.count('\n')
 found=[]
 for i,(v,line) in enumerate(tokens):
  if v=='defer':found.append(dict(kind='defer',line=line))
  if i+3<len(tokens) and v=='os' and tokens[i+1][0]=='.' and tokens[i+2][0] in ['Open','OpenFile','Create','CreateTemp'] and tokens[i+3][0]=='(':
   found.append(dict(kind='acquire',line=line))
 return found

def main():
 root=Path(__file__).resolve().parents[1];labels=json.loads((root/'benchmarks/labels.json').read_text())
 rows=[];expected={k:set() for k in ['acquire','defer']};actual={k:set() for k in expected}
 for c in labels:
  found=scan((root/c['file']).read_text())
  for s in found:actual[s['kind']].add((c['file'],s['line']))
  for s in c['sites']:expected[s['kind']].add((c['file'],s['line']))
  rows.append(dict(id=c['id'],file=c['file'],sites=found))
 metrics={}
 for k in expected:
  e,a=expected[k],actual[k];tp=len(e&a)
  metrics[k]=dict(tp=tp,fp=len(a-e),fn=len(e-a),precision=tp/len(a) if a else None,recall=tp/len(e) if e else None)
 # Negative controls reveal both strengths and intentional limits.
 controls=[
  dict(name='ignore_comments_and_strings',source='// defer os.Open("x")\n/* defer os.Open("x") */\nvar x = "defer os.Open(\\\"x\\\")"\n',expected=[]),
  dict(name='shadowed_os_false_positive',source='func f(os Fake) { os.Open("x") }',expected=[dict(kind='acquire',line=1)]),
  dict(name='aliased_import_false_negative',source='import fs "os"\nfunc f() { fs.Open("x") }',expected=[]),
 ]
 for c in controls:
  c['observed']=scan(c['source']);assert c['observed']==c['expected']
 result=dict(evidence_type='executed lexical inventory on authored corpus; no type or SSA validation',metrics=metrics,controls=controls,programs=rows)
 (root/'results/lexical_inventory.json').write_text(json.dumps(result,indent=2)+'\n')
 print(json.dumps(metrics,indent=2))
if __name__=='__main__':main()
