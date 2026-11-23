#!/usr/bin/env python3
"""Writes the SECOND unseen set (testdata/final_unseen/UNN_*/main.go) and its labels.json.

Authored 2026-11-23, after step 1/2 (analysis + path checks) and BEFORE the step-3 rules.
SEALED: do not run rescflow, evaluate_analysis.py or check_traces.py on this folder until the
final evaluation. Only real Go has been run on it, to check the expected outputs
(scripts/run_heldout_go.py GOROOT --labels=testdata/final_unseen/labels.json)
It lives under testdata/ so that `./...` patterns (rescflow's default) never include it.. SEAL.txt holds the sha256 of
every file, so any later change is visible.

Same conventions as heldout/make_heldout.py: corpus style (site labels are line numbers),
optional `inject` per input ("openat:2:EMFILE" = the 2nd open of the input file fails).
"""
import hashlib, json, sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent.parent / 'heldout'))
from make_heldout import HEAD, MAIN, HELPERS, DEFAULT_MAIN  # noqa: E402

P = [
 dict(id='U01', name='two_files_error_join', expected_rescflow='A',
      plan='OWNED_GUARD for A; RESULT_SLOT_GUARD for B (its close error joins the named result)',
      body='''func scenario() (err error) {
src, err := os.Open(path) // RF:acquire A
if !recordOpen("A", src, err) { return err }
defer closeFile("A", src) // RF:defer D1 A
dst, err := os.Open(path) // RF:acquire B
if !recordOpen("B", dst, err) { return err }
defer func() { if cerr := closeFile("B", dst); err == nil { err = cerr } }() // RF:defer D2 B
useFile("A", src)
useFile("B", dst)
return nil
}
''',
      main='err := scenario(); fmt.Println("result-error", err != nil)',
      inputs=[dict(mode='', out=['acquire A true', 'acquire B true', 'use A true', 'use B true', 'close B true', 'close A true', 'result-error false']),
              dict(mode='', inject='openat:1:ENOENT', out=['acquire A false', 'result-error true']),
              dict(mode='', inject='openat:2:EMFILE', out=['acquire A true', 'acquire B false', 'close A true', 'result-error true']),
              dict(mode='', inject='close:1:EIO', out=['acquire A true', 'acquire B true', 'use A true', 'use B true', 'close B false', 'close A true', 'result-error true'])]),
 dict(id='U02', name='retry_until_open', expected_rescflow='A',
      plan='DEFER_STACK (a defer in a loop; at most one registration because of the break)',
      body='''func scenario() {
ids := []string{"0", "1", "2"}
for i := 0; i < 3; i++ {
f, err := os.Open(path) // RF:acquire A
if !recordOpen(ids[i], f, err) { continue }
defer closeFile(ids[i], f) // RF:defer D1 A
useFile(ids[i], f)
break
}
fmt.Println("after-loop")
}
''',
      inputs=[dict(mode='', out=['acquire 0 true', 'use 0 true', 'after-loop', 'close 0 true', 'returned']),
              dict(mode='', inject='openat:1:EMFILE', out=['acquire 0 false', 'acquire 1 true', 'use 1 true', 'after-loop', 'close 1 true', 'returned'])]),
 dict(id='U03', name='reopen_through_pointer', expected_rescflow='A',
      plan='CELL_CLOSURE_GUARD: a helper replaces the file through a pointer; the deferred closure reads it at exit',
      body='''func reopen(fp **os.File) bool {
g, err := os.Open(path) // RF:acquire B
if !recordOpen("B", g, err) { return false }
closeFile("A", *fp)
*fp = g
return true
}
func scenario() {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
name := "A"
defer func() { closeFile(name, f) }() // RF:defer D1 A,B
if mode == "reopen" && reopen(&f) { name = "B" }
useFile(name, f)
}
''',
      inputs=[dict(mode='', out=['acquire A true', 'use A true', 'close A true', 'returned']),
              dict(mode='reopen', out=['acquire A true', 'acquire B true', 'close A true', 'use B true', 'close B true', 'returned']),
              dict(mode='reopen', inject='openat:2:EMFILE', out=['acquire A true', 'acquire B false', 'use A true', 'close A true', 'returned'])]),
 dict(id='U04', name='struct_owns_two', expected_rescflow='A',
      plan='OWNED_GUARD on a struct that owns both files; its cleanup closes B then A',
      types='''type pair struct{ a, b *os.File }
func (p *pair) Close() { closeFile("B", p.b); closeFile("A", p.a) }
''',
      body='''func scenario() {
a, err := os.Open(path) // RF:acquire A
if !recordOpen("A", a, err) { return }
b, err := os.Open(path) // RF:acquire B
if !recordOpen("B", b, err) { closeFile("A", a); return }
p := &pair{a: a, b: b}
defer p.Close() // RF:defer D1 A,B
useFile("A", p.a)
useFile("B", p.b)
}
''',
      inputs=[dict(mode='', out=['acquire A true', 'acquire B true', 'use A true', 'use B true', 'close B true', 'close A true', 'returned']),
              dict(mode='', inject='openat:2:EMFILE', out=['acquire A true', 'acquire B false', 'close A true', 'returned'])]),
 dict(id='U05', name='forgotten_close', expected_rescflow='C',
      plan='REVIEW: the "skip" path returns without closing A (leak). Intended?',
      body='''func scenario() {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
if mode == "skip" { fmt.Println("skip"); return }
useFile("A", f)
closeFile("A", f)
}
''',
      inputs=[dict(mode='', out=['acquire A true', 'use A true', 'close A true', 'returned']),
              dict(mode='skip', out=['acquire A true', 'skip', 'returned'])]),
 dict(id='U06', name='bufio_reader', expected_rescflow='C',
      plan='REVIEW until bufio.NewReader is modelled: the file is passed to library code with no summary',
      imports='"bufio"; "fmt"; "os"',
      body='''func scenario() {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
defer closeFile("A", f) // RF:defer D1 A
r := bufio.NewReader(f)
line, _ := r.ReadString('\\n')
fmt.Println("read", len(line) > 0)
}
''',
      inputs=[dict(mode='', out=['acquire A true', 'read true', 'close A true', 'returned']),
              dict(mode='', inject='openat:1:ENOENT', out=['acquire A false', 'returned'])]),
 dict(id='U07', name='stored_closure_deferred', expected_rescflow='A',
      plan='OWNED_GUARD (the cleanup closure is created first and deferred later; f is never reassigned)',
      body='''func scenario() {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
cleanup := func() { closeFile("A", f) }
useFile("A", f)
defer cleanup() // RF:defer D1 A
fmt.Println("body")
}
''',
      inputs=[dict(mode='', out=['acquire A true', 'use A true', 'body', 'close A true', 'returned'])]),
 dict(id='U08', name='kept_in_registry', expected_rescflow='C',
      plan='REVIEW: on the "keep" path the file is stored in a map and closed later by main',
      types='var registry = map[string]*os.File{}\n',
      body='''func scenario() {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
if mode == "keep" { registry["A"] = f; fmt.Println("kept"); return }
defer closeFile("A", f) // RF:defer D1 A
useFile("A", f)
}
''',
      main='scenario(); fmt.Println("returned"); if g, ok := registry["A"]; ok { useFile("A", g); closeFile("A", g) }',
      inputs=[dict(mode='', out=['acquire A true', 'use A true', 'close A true', 'returned']),
              dict(mode='keep', out=['acquire A true', 'kept', 'returned', 'use A true', 'close A true'])]),
 dict(id='U09', name='callee_panics_owns_file', expected_rescflow='A',
      plan='CALLEE_OWNS: work() takes the file and its guard drops during unwinding; recover needs catch_unwind',
      body='''func work(f *os.File) {
defer closeFile("A", f) // RF:defer D1 A
useFile("A", f)
if mode == "panic" { panic("boom") }
}
func scenario() {
defer func() { if recover() != nil { fmt.Println("recovered") } }() // RF:defer D0 none
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
work(f)
fmt.Println("after-work")
}
''',
      inputs=[dict(mode='', out=['acquire A true', 'use A true', 'close A true', 'after-work', 'returned']),
              dict(mode='panic', out=['acquire A true', 'use A true', 'close A true', 'recovered', 'returned'])]),
 dict(id='U10', name='out_param_transfer', expected_rescflow='B',
      plan='Two plans: ownership moves to the caller through *out on the "give" path, guard otherwise. '
           'Choice rule: the defer is registered only after the transferring branch, so move on "give", guard on the rest',
      body='''func scenario(out **os.File) {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
if mode == "give" { *out = f; return }
defer closeFile("A", f) // RF:defer D1 A
useFile("A", f)
}
''',
      main='var g *os.File; scenario(&g); fmt.Println("returned"); if g != nil { useFile("A", g); closeFile("A", g) }',
      inputs=[dict(mode='', out=['acquire A true', 'use A true', 'close A true', 'returned']),
              dict(mode='give', out=['acquire A true', 'returned', 'use A true', 'close A true'])]),
]


def main():
    labels = []
    for p in P:
        d = HERE / f"{p['id']}_{p['name']}"
        d.mkdir(exist_ok=True)
        src = (HEAD.format(imports=p.get('imports', '"fmt"; "os"')) + p['body'] + '\n\n\n' +
               MAIN % p.get('main', DEFAULT_MAIN) + HELPERS + ('\n' + p['types'] if 'types' in p else ''))
        (d / 'main.go').write_text(src)
        sites = []
        for n, line in enumerate(src.splitlines(), 1):
            if '// RF:' in line:
                code, tag = line.split('// RF:')
                kind, *label = tag.split()
                sites.append(dict(kind=kind, label=' '.join(label), line=n, source=code.strip()))
        labels.append(dict(
            id=p['id'], name=p['name'], expected_rescflow_category=p['expected_rescflow'], expected_plan=p['plan'],
            file=f"testdata/final_unseen/{p['id']}_{p['name']}/main.go", sites=sites,
            inputs=[dict(mode=i['mode'], inject=i.get('inject'), expected_stdout=i['out'], expected_exit=0)
                    for i in p['inputs']],
            label_status='author-derived (Claude, 2026-11-23, before step-3 rules); checked against Go only; SEALED'))
    (HERE / 'labels.json').write_text(json.dumps(labels, indent=1) + '\n')
    seal = []
    for f in sorted(HERE.rglob('*')):
        if f.is_file() and f.name != 'SEAL.txt' and '__pycache__' not in f.parts:
            seal.append(f"{hashlib.sha256(f.read_bytes()).hexdigest()}  {f.relative_to(HERE)}")
    (HERE / 'SEAL.txt').write_text('\n'.join(seal) + '\n')
    print(f"{len(labels)} programs, {sum(len(l['inputs']) for l in labels)} inputs, "
          f"{sum(1 for l in labels for i in l['inputs'] if i['inject'])} injected; categories "
          f"{''.join(sorted(l['expected_rescflow_category'] for l in labels))}")


if __name__ == '__main__':
    main()
