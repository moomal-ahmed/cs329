#!/usr/bin/env python3
"""Small executable semantic model, NOT execution of Go/Rust or a source translator.
Only the listed operations are modeled. 'bad_*' policies are deliberately wrong mutants,
not implementations attributed to any baseline. Expected traces are separately specified.
"""
from pathlib import Path
import json
class Machine:
 def __init__(self,policy='go'):
  self.policy=policy;self.env={};self.stack=[];self.trace=[];self.result=0
 def close(self,r):self.trace.append('close '+r)
 def drain(self,start=0):
  pending=self.stack[start:];del self.stack[start:]
  if self.policy!='bad_fifo':pending=list(reversed(pending))
  for kind,arg in pending:
   if kind=='value':self.close(arg)
   elif kind=='cell':self.close(self.env[arg])
   elif kind=='result':self.result=arg;self.trace.append('set-result '+str(arg))
 def run(self,ops):
  for op in ops:
   kind=op[0]
   if kind=='bind':self.env[op[1]]=op[2];self.trace.append('acquire '+op[2])
   elif kind=='defer_value':
    self.stack.append(('cell',op[1]) if self.policy=='bad_late_argument' else ('value',self.env[op[1]]))
   elif kind=='defer_cell':
    self.stack.append(('value',self.env[op[1]]) if self.policy=='bad_eager_closure' else ('cell',op[1]))
   elif kind=='defer_result':self.stack.append(('result',op[1]))
   elif kind=='event':self.trace.append(op[1])
   elif kind=='block':
    start=len(self.stack);self.run(op[1])
    if self.policy=='bad_block_exit':self.drain(start)
   elif kind=='result':self.result=op[1]
   else:raise ValueError(op)
 def execute(self,ops):
  self.run(ops);before=self.result;self.drain()
  if self.policy=='bad_result_snapshot':self.result=before
  return self.trace
CASES=[
 ('local',[('bind','f','A'),('defer_value','f'),('event','body')],['acquire A','body','close A'],'bad_block_exit',False),
 ('lifo',[('bind','f','A'),('defer_value','f'),('bind','g','B'),('defer_value','g')],['acquire A','acquire B','close B','close A'],'bad_fifo',True),
 ('nested_block',[('block',[('bind','f','A'),('defer_value','f'),('event','inside')]),('event','after-block')],['acquire A','inside','after-block','close A'],'bad_block_exit',True),
 ('loop_unrolled',[('block',[('bind','f',str(i)),('defer_value','f'),('event','iteration '+str(i))]) for i in range(3)]+[('event','after-loop')],['acquire 0','iteration 0','acquire 1','iteration 1','acquire 2','iteration 2','after-loop','close 2','close 1','close 0'],'bad_block_exit',True),
 ('eager_argument',[('bind','f','A'),('defer_value','f'),('bind','f','B')],['acquire A','acquire B','close A'],'bad_late_argument',True),
 ('late_closure',[('bind','f','A'),('defer_cell','f'),('bind','f','B')],['acquire A','acquire B','close B'],'bad_eager_closure',True),
]
def main():
 rows=[]
 for name,ops,expected,mutant,should_differ in CASES:
  good=Machine();observed=good.execute(ops);bad=Machine(mutant).execute(ops)
  assert observed==expected,(name,observed,expected)
  assert (observed!=bad)==should_differ
  rows.append(dict(case=name,expected=expected,model=observed,mutant=mutant,mutant_trace=bad,difference=observed!=bad))
 ops=[('defer_result',7),('result',0)]
 good=Machine();good.execute(ops);bad=Machine('bad_result_snapshot');bad.execute(ops)
 assert good.result==7 and bad.result==0
 rows.append(dict(case='named_result',model_result=good.result,mutant_result=bad.result,difference=True))
 result=dict(evidence_type='executed Python semantic model; not Go or Rust results',cases=rows,checks_passed=len(rows),distinguished_case_policy_pairs=6,negative_controls=1)
 p=Path(__file__).resolve().parents[1]/'results/model_results.json';p.write_text(json.dumps(result,indent=2)+'\n')
 print(json.dumps(result,indent=2))
if __name__=='__main__':main()
