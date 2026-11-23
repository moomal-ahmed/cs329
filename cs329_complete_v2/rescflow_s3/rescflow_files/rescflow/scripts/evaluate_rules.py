#!/usr/bin/env python3
"""Step 3 check: compare the rules' A/B/C decisions with the expected categories.

Usage: python3 scripts/evaluate_rules.py <rescflow binary>
Expected categories: benchmarks/expected_categories.json (proposed, before the rules were
written; pending team sign-off) and heldout/labels.json (written in step 0, before any analysis).
The sealed set testdata/final_unseen is NOT evaluated here.
Writes results/rules_eval.json and results/rules_eval.txt.
"""
import json, subprocess, sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def main():
    binary = sys.argv[1]
    exp_b = json.loads((ROOT / 'benchmarks/expected_categories.json').read_text())['programs']
    exp_h = {c['id']: c for c in json.loads((ROOT / 'heldout/labels.json').read_text())}
    out, lines = {}, []
    for setname, pattern in [('benchmarks', './benchmarks/...'), ('heldout', './heldout/...')]:
        rep = json.loads(subprocess.run([binary, pattern], cwd=ROOT, capture_output=True, text=True).stdout)
        decisions = {}
        for d in rep['decisions']:
            decisions.setdefault(d['func'].split('.')[0], []).append(d)
        rows, agree, confusion = [], 0, {}
        for pd in rep['program_decisions']:
            pid = pd['package'].split('_')[0]
            exp = exp_b[pid] if setname == 'benchmarks' else dict(category=exp_h[pid]['expected_rescflow_category'],
                                                                  plan=exp_h[pid]['expected_plan'])
            ok = pd['category'] == exp['category']
            agree += ok
            confusion[(exp['category'], pd['category'])] = confusion.get((exp['category'], pd['category']), 0) + 1
            plans = [f"{d['var']}@{d['resource'].rsplit(':', 1)[1]}: {d['plan']}" for d in decisions.get(pd['package'], [])]
            rows.append(dict(program=pd['package'], expected=exp['category'], decided=pd['category'], agree=ok,
                             expected_plan=exp['plan'], decided_plans=plans))
            lines.append(f"  {pd['package']:30} expected {exp['category']}  decided {pd['category']}  "
                         f"{'ok' if ok else 'DISAGREE'}   {'; '.join(plans)}")
        head = f"== {setname}: categories agree on {agree}/{len(rows)} programs"
        cm = '   confusion (expected->decided): ' + ', '.join(f"{a}->{b}: {n}" for (a, b), n in sorted(confusion.items()))
        lines[len(lines) - len(rows):len(lines) - len(rows)] = [head, cm]
        out[setname] = dict(agree=agree, programs=len(rows), confusion={f"{a}->{b}": n for (a, b), n in confusion.items()}, rows=rows)
    text = '\n'.join(lines)
    print(text)
    (ROOT / 'results/rules_eval.json').write_text(json.dumps(out, indent=2) + '\n')
    (ROOT / 'results/rules_eval.txt').write_text(text + '\n')


if __name__ == '__main__':
    main()
