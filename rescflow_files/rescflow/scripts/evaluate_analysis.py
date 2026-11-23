#!/usr/bin/env python3
"""Step 2 check: compare the analyzer's output with the hand-written labels.

Usage: python3 scripts/evaluate_analysis.py <rescflow binary>
Runs the analyzer on ./benchmarks/... and ./heldout/..., then scores:
  - acquisition sites and defer sites (precision / recall by source line)
  - defer -> resource associations: the resources each labelled defer acts on, compared with
    what the analysis resolves at the function's exits (exact / superset / mismatch)
  - expected anomalies (use after close, double close, leak) and expected unresolved cases
Writes results/analysis_eval.json.
"""
import json, re, subprocess, sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

# Behaviour the Go programs really have (confirmed by the Go runs), which the analysis must report.
EXPECTED_ANOMALY = {'10': 'use after close', '16': 'double close', '18': 'use after close',
                    '19': 'double close', 'H14': 'may still be open'}
EXPECTED_UNRESOLVED = {'H13': 'goroutine', 'H14': 'dispatch-multi'}


def run(binary, pattern):
    p = subprocess.run([binary, pattern], cwd=ROOT, capture_output=True, text=True)
    return json.loads(p.stdout)


def parse_defer_label(label):
    # "D1 A" | "D1 A,B" | "D0 none" | "D1 A?"
    parts = label.split(None, 1)
    if len(parts) < 2 or parts[1] == 'none':
        return set()
    return {x.strip().rstrip('?') for x in parts[1].split(',') if x.strip()}


def evaluate(labels_file, rep, prefix):
    labels = json.loads((ROOT / labels_file).read_text())
    rows, tot = [], dict(acq_tp=0, acq_fp=0, acq_fn=0, def_tp=0, def_fp=0, def_fn=0,
                         assoc_exact=0, assoc_superset=0, assoc_wrong=0, anomaly_ok=0, anomaly_bad=0,
                         unresolved_ok=0, unresolved_bad=0)
    for c in labels:
        file = c['file']
        pkg = Path(file).parent.name
        lab_acq = {s['line']: s['label'] for s in c['sites'] if s['kind'] == 'acquire'}
        lab_def = {s['line']: parse_defer_label(s['label']) for s in c['sites'] if s['kind'] == 'defer'}
        recs = [r for r in rep['records'] if r['file'] == file]
        ana_acq = {r['line'] for r in recs}
        defs = [d for d in rep['defers'] if d['func'].split('.')[0] == pkg]
        ana_def = {d['line'] for d in defs}
        line_to_label = dict(lab_acq)
        row = dict(id=c['id'], name=c['name'])
        row['acq_missed'] = sorted(set(lab_acq) - ana_acq)
        row['acq_extra'] = sorted(ana_acq - set(lab_acq))
        row['defer_missed'] = sorted(set(lab_def) - ana_def)
        # a defer the labels do not list is only an error if it acts on a resource
        # (e.g. `defer wg.Done()` is a real defer statement but not a resource site)
        acting = {d['line'] for d in defs if any(v for v in d['resolves'].values())}
        row['defer_extra'] = sorted((ana_def - set(lab_def)) & acting)
        tot['acq_tp'] += len(set(lab_acq) & ana_acq); tot['acq_fp'] += len(row['acq_extra']); tot['acq_fn'] += len(row['acq_missed'])
        tot['def_tp'] += len(set(lab_def) & ana_def); tot['def_fp'] += len(row['defer_extra']); tot['def_fn'] += len(row['defer_missed'])
        row['assoc'] = {}
        for line, want in lab_def.items():
            got = set()
            for d in defs:
                if d['line'] == line:
                    for acts in d['resolves'].values():
                        for a in acts or []:
                            m = re.search(r'@(\d+)$', a)
                            if m:
                                got.add(line_to_label.get(int(m.group(1)), 'line' + m.group(1)))
            verdict = 'exact' if got == want else 'superset (path-dependent)' if want < got else 'MISMATCH'
            tot['assoc_exact' if verdict == 'exact' else 'assoc_superset' if verdict.startswith('superset') else 'assoc_wrong'] += 1
            row['assoc'][line] = dict(label=sorted(want), analysis=sorted(got), verdict=verdict)
        anomalies = [a for r in recs for a in r['anomalies']]
        exp = EXPECTED_ANOMALY.get(c['id'])
        ok = (any(exp in a for a in anomalies) if exp else not anomalies)
        tot['anomaly_ok' if ok else 'anomaly_bad'] += 1
        row['anomalies'], row['anomaly_expected'], row['anomaly_ok'] = anomalies, exp, ok
        unres = [u['kind'] for u in rep['unresolved'] if (u.get('file') or '').startswith(str(Path(file).parent))]
        expu = EXPECTED_UNRESOLVED.get(c['id'])
        uok = (expu in unres) if expu else not unres
        tot['unresolved_ok' if uok else 'unresolved_bad'] += 1
        row['unresolved'], row['unresolved_expected'], row['unresolved_ok'] = unres, expu, uok
        rows.append(row)
    return rows, tot


def main():
    binary = sys.argv[1]
    out = {}
    for name, labels_file, pattern in [('benchmarks', 'benchmarks/labels.json', './benchmarks/...'),
                                       ('heldout', 'heldout/labels.json', './heldout/...')]:
        rep = run(binary, pattern)
        rows, tot = evaluate(labels_file, rep, name)
        out[name] = dict(totals=tot, programs=rows)
        print(f"== {name}: {len(rows)} programs")
        print(f"  acquisition sites: TP {tot['acq_tp']} FP {tot['acq_fp']} FN {tot['acq_fn']}")
        print(f"  defer sites:       TP {tot['def_tp']} FP {tot['def_fp']} FN {tot['def_fn']}")
        print(f"  defer->resource:   exact {tot['assoc_exact']}, superset {tot['assoc_superset']}, mismatch {tot['assoc_wrong']}")
        print(f"  anomalies as expected: {tot['anomaly_ok']}/{tot['anomaly_ok'] + tot['anomaly_bad']}")
        print(f"  unresolved as expected: {tot['unresolved_ok']}/{tot['unresolved_ok'] + tot['unresolved_bad']}")
        for r in rows:
            issues = []
            if r['acq_missed'] or r['acq_extra']: issues.append(f"acq missed={r['acq_missed']} extra={r['acq_extra']}")
            if r['defer_missed'] or r['defer_extra']: issues.append(f"defer missed={r['defer_missed']} extra={r['defer_extra']}")
            for line, a in r['assoc'].items():
                if a['verdict'] != 'exact': issues.append(f"defer@{line}: label {a['label']} analysis {a['analysis']} ({a['verdict']})")
            if not r['anomaly_ok']: issues.append(f"anomalies {r['anomalies']} expected {r['anomaly_expected']}")
            if not r['unresolved_ok']: issues.append(f"unresolved {r['unresolved']} expected {r['unresolved_expected']}")
            if issues:
                print(f"    {r['id']} {r['name']}: " + '; '.join(issues))
    (ROOT / 'results/analysis_eval.json').write_text(json.dumps(out, indent=2) + '\n')


if __name__ == '__main__':
    main()
