#!/usr/bin/env python3
"""Writes the held-out RescFlow programs (heldout/HNN_*/main.go) and heldout/labels.json.

Authored 2026-11-23, after the LLM translations of the original 20 existed and before any
RescFlow analysis was written. Do not tune RescFlow on these; report final numbers on them.

Each expected trace was derived by hand from Go semantics and then checked against real
Go runs with scripts/run_heldout_go.py (it does not copy Go output into the labels).
Inputs may carry an `inject` spec "syscall:N:ERRNO": the Nth `syscall` on the input file
fails with ERRNO (strace fault injection), e.g. "openat:2:EMFILE" or "close:1:EIO".
Sources keep the original corpus style (one statement per line, no gofmt), because site
labels are line numbers.
"""
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
HEAD = 'package main\n\nimport ({imports})\n\nvar path, mode string\n\n'
MAIN = '''func main() {
if len(os.Args) < 2 { panic("pass input file path") }
path = os.Args[1]
if len(os.Args) > 2 { mode = os.Args[2] }
%s
}
'''
HELPERS = '''
func recordOpen(id string, f *os.File, err error) bool {
 fmt.Println("acquire", id, err == nil)
 return err == nil
}
func closeFile(id string, f *os.File) error {
 err := f.Close()
 fmt.Println("close", id, err == nil)
 return err
}
func useFile(id string, f *os.File) {
 _, err := f.Stat()
 fmt.Println("use", id, err == nil)
}
'''
DEFAULT_MAIN = 'scenario(); fmt.Println("returned")'

# expected_rescflow: the category RescFlow should assign.
#   A = one supported plan, B = several plans resolved by a documented rule, C = needs review.
P = [
 dict(id='H01', name='loop_count', category='loop', expected_rescflow='A',
      plan='EXPLICIT_STATE: DeferStack, LIFO; loop may run 0, 1 or N times',
      body='''func scenario() {
n := 3
if mode == "0" { n = 0 }
if mode == "1" { n = 1 }
ids := []string{"0", "1", "2"}
for i := 0; i < n; i++ {
f, err := os.Open(path) // RF:acquire A
if !recordOpen(ids[i], f, err) { return }
defer closeFile(ids[i], f) // RF:defer D1 A
fmt.Println("iteration", ids[i])
}
fmt.Println("after-loop")
}
''',
      inputs=[dict(mode='0', out=['after-loop', 'returned']),
              dict(mode='1', out=['acquire 0 true', 'iteration 0', 'after-loop', 'close 0 true', 'returned']),
              dict(mode='3', out=['acquire 0 true', 'iteration 0', 'acquire 1 true', 'iteration 1', 'acquire 2 true',
                                  'iteration 2', 'after-loop', 'close 2 true', 'close 1 true', 'close 0 true', 'returned']),
              dict(mode='3', inject='openat:2:EMFILE',
                   out=['acquire 0 true', 'iteration 0', 'acquire 1 false', 'close 0 true', 'returned'])]),
 dict(id='H02', name='two_per_iteration', category='loop', expected_rescflow='A',
      plan='EXPLICIT_STATE: one DeferStack shared by both resources, LIFO across iterations',
      body='''func scenario() {
ids := []string{"0", "1"}
for i := 0; i < 2; i++ {
f, err := os.Open(path) // RF:acquire A
if !recordOpen(ids[i]+"a", f, err) { return }
defer closeFile(ids[i]+"a", f) // RF:defer D1 A
g, err := os.Open(path) // RF:acquire B
if !recordOpen(ids[i]+"b", g, err) { return }
defer closeFile(ids[i]+"b", g) // RF:defer D2 B
}
fmt.Println("after-loop")
}
''',
      inputs=[dict(mode='', out=['acquire 0a true', 'acquire 0b true', 'acquire 1a true', 'acquire 1b true', 'after-loop',
                                 'close 1b true', 'close 1a true', 'close 0b true', 'close 0a true', 'returned']),
              dict(mode='', inject='openat:3:EMFILE',
                   out=['acquire 0a true', 'acquire 0b true', 'acquire 1a false', 'close 0b true', 'close 0a true', 'returned'])]),
 dict(id='H03', name='conditional_in_loop', category='loop+conditional', expected_rescflow='A',
      plan='EXPLICIT_STATE: DeferStack, push only on even iterations; explicit close otherwise',
      body='''func scenario() {
ids := []string{"0", "1", "2"}
for i := 0; i < 3; i++ {
f, err := os.Open(path) // RF:acquire A
if !recordOpen(ids[i], f, err) { return }
if i%2 == 0 {
defer closeFile(ids[i], f) // RF:defer D1 A
} else {
closeFile(ids[i], f)
}
}
fmt.Println("after-loop")
}
''',
      inputs=[dict(mode='', out=['acquire 0 true', 'acquire 1 true', 'close 1 true', 'acquire 2 true', 'after-loop',
                                 'close 2 true', 'close 0 true', 'returned'])]),
 dict(id='H04', name='closure_two_vars', category='capture-cell', expected_rescflow='A',
      plan='EXPLICIT_STATE: f, g (and their names) live in cells read by the deferred closure at exit',
      body='''func scenario() {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
g, err := os.Open(path) // RF:acquire B
if !recordOpen("B", g, err) { closeFile("A", f); return }
fn, gn := "A", "B"
defer func() { closeFile(fn, f); closeFile(gn, g) }() // RF:defer D1 A,B
if mode == "swap" { f, g = g, f; fn, gn = gn, fn }
useFile(fn, f)
}
''',
      inputs=[dict(mode='', out=['acquire A true', 'acquire B true', 'use A true', 'close A true', 'close B true', 'returned']),
              dict(mode='swap', out=['acquire A true', 'acquire B true', 'use B true', 'close B true', 'close A true', 'returned']),
              dict(mode='', inject='openat:2:EMFILE', out=['acquire A true', 'acquire B false', 'close A true', 'returned'])]),
 dict(id='H05', name='field_alias', category='alias-heap-field', expected_rescflow='A',
      plan='LOCAL: h.f aliases f; deferred argument evaluated at registration; guard at function scope',
      types='type holder struct{ f *os.File }\n',
      body='''func scenario() {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
h := &holder{f: f}
defer closeFile("A", h.f) // RF:defer D1 A
useFile("A", f)
}
''',
      inputs=[dict(mode='', out=['acquire A true', 'use A true', 'close A true', 'returned'])]),
 dict(id='H06', name='method_defer', category='transfer-to-callee', expected_rescflow='A',
      plan='OWNERSHIP_TRANSFER: run() takes ownership (no caller use afterwards); guard inside run',
      types='type logFile struct{ f *os.File }\n',
      body='''func (l *logFile) run() {
defer closeFile("A", l.f) // RF:defer D1 A
useFile("A", l.f)
}
func scenario() {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
l := &logFile{f: f}
l.run()
fmt.Println("after-run")
}
''',
      inputs=[dict(mode='', out=['acquire A true', 'use A true', 'close A true', 'after-run', 'returned'])]),
 dict(id='H07', name='early_return_in_loop', category='loop+early-return', expected_rescflow='A',
      plan='EXPLICIT_STATE: DeferStack drained on the early return inside the loop',
      body='''func scenario() {
ids := []string{"0", "1", "2"}
for i := 0; i < 3; i++ {
f, err := os.Open(path) // RF:acquire A
if !recordOpen(ids[i], f, err) { return }
defer closeFile(ids[i], f) // RF:defer D1 A
if mode == "early" && i == 1 { fmt.Println("early"); return }
}
fmt.Println("after-loop")
}
''',
      inputs=[dict(mode='', out=['acquire 0 true', 'acquire 1 true', 'acquire 2 true', 'after-loop',
                                 'close 2 true', 'close 1 true', 'close 0 true', 'returned']),
              dict(mode='early', out=['acquire 0 true', 'acquire 1 true', 'early', 'close 1 true', 'close 0 true', 'returned'])]),
 dict(id='H08', name='factory_helper', category='interprocedural-acquire', expected_rescflow='A',
      plan='LOCAL after summary: openChecked returns a fresh resource on success; guard in scenario',
      body='''func openChecked(id string) (*os.File, bool) {
f, err := os.Open(path) // RF:acquire A
if !recordOpen(id, f, err) { return nil, false }
return f, true
}
func scenario() {
f, ok := openChecked("A")
if !ok { return }
defer closeFile("A", f) // RF:defer D1 A
useFile("A", f)
}
''',
      inputs=[dict(mode='', out=['acquire A true', 'use A true', 'close A true', 'returned']),
              dict(mode='', inject='openat:1:ENOENT', out=['acquire A false', 'returned'])]),
 dict(id='H09', name='nested_calls', category='two-frames', expected_rescflow='A',
      plan='LOCAL x2: each function owns its own guard; B closes at inner() exit, before A is used',
      body='''func inner() {
g, err := os.Open(path) // RF:acquire B
if !recordOpen("B", g, err) { return }
defer closeFile("B", g) // RF:defer D2 B
useFile("B", g)
}
func scenario() {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
defer closeFile("A", f) // RF:defer D1 A
inner()
useFile("A", f)
}
''',
      inputs=[dict(mode='', out=['acquire A true', 'acquire B true', 'use B true', 'close B true', 'use A true',
                                 'close A true', 'returned']),
              dict(mode='', inject='openat:2:EMFILE', out=['acquire A true', 'acquire B false', 'use A true',
                                                          'close A true', 'returned'])]),
 dict(id='H10', name='close_error_join', category='named-result+close-error', expected_rescflow='A',
      plan='EXPLICIT_STATE: close error joined into named result only if no earlier error; explicit close, not Drop',
      body='''func scenario() (err error) {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return err }
defer func() { if cerr := closeFile("A", f); err == nil { err = cerr } }() // RF:defer D1 A
useFile("A", f)
return nil
}
''',
      main='err := scenario(); fmt.Println("result-error", err != nil)',
      inputs=[dict(mode='', out=['acquire A true', 'use A true', 'close A true', 'result-error false']),
              dict(mode='', inject='close:1:EIO', out=['acquire A true', 'use A true', 'close A false', 'result-error true']),
              dict(mode='', inject='openat:1:ENOENT', out=['acquire A false', 'result-error true'])]),
 dict(id='H11', name='panic_from_callee', category='panic-unwind', expected_rescflow='A',
      plan='LOCAL + recovery: guard drops during unwinding; catch_unwind for the recover defer',
      body='''func mustFail() { panic("boom") }
func scenario() {
defer func() { if recover() != nil { fmt.Println("recovered") } }() // RF:defer D0 none
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
defer closeFile("A", f) // RF:defer D1 A
useFile("A", f)
mustFail()
fmt.Println("unreachable")
}
''',
      inputs=[dict(mode='', out=['acquire A true', 'use A true', 'close A true', 'recovered', 'returned'])]),
 dict(id='H12', name='path_dependent_ownership', category='escape-on-one-path', expected_rescflow='B',
      plan='Two plans (return ownership vs local guard). Choice rule: the defer is registered only after the '
           'returning branch, so move on the "keep" path and use a guard on the other; return Option<File>',
      body='''func scenario() *os.File {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return nil }
if mode == "keep" { return f }
defer closeFile("A", f) // RF:defer D1 A
useFile("A", f)
return nil
}
''',
      main='f := scenario(); fmt.Println("returned"); if f != nil { useFile("A", f); closeFile("A", f) }',
      inputs=[dict(mode='', out=['acquire A true', 'use A true', 'close A true', 'returned']),
              dict(mode='keep', out=['acquire A true', 'returned', 'use A true', 'close A true'])]),
 dict(id='H13', name='goroutine_defer', category='concurrency', expected_rescflow='C',
      plan='Out of scope: resource acquired and deferred inside a goroutine; report, do not translate',
      imports='"fmt"; "os"; "sync"',
      body='''func scenario() {
var wg sync.WaitGroup
wg.Add(1)
go func() {
defer wg.Done()
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
defer closeFile("A", f) // RF:defer D1 A
useFile("A", f)
}()
wg.Wait()
fmt.Println("joined")
}
''',
      inputs=[dict(mode='', out=['acquire A true', 'use A true', 'close A true', 'joined', 'returned'])]),
 dict(id='H14', name='dynamic_closer', category='interface-multi-target', expected_rescflow='C',
      plan='Deferred Close dispatches to one of two implementations chosen at run time; on the noop path A is '
           'never closed. No single owner or cleanup plan; report',
      types='''type closer interface{ Close() error }
type fileCloser struct{ f *os.File }
func (c fileCloser) Close() error { return closeFile("A", c.f) }
type noopCloser struct{}
func (noopCloser) Close() error { fmt.Println("noop close"); return nil }
''',
      body='''func scenario() {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
var c closer = fileCloser{f}
if mode == "noop" { c = noopCloser{} }
defer c.Close() // RF:defer D1 A?
useFile("A", f)
}
''',
      inputs=[dict(mode='', out=['acquire A true', 'use A true', 'close A true', 'returned']),
              dict(mode='noop', out=['acquire A true', 'use A true', 'noop close', 'returned'])]),
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
            id=p['id'], name=p['name'], category=p['category'],
            expected_rescflow_category=p['expected_rescflow'], expected_plan=p['plan'],
            file=f"heldout/{p['id']}_{p['name']}/main.go", sites=sites,
            inputs=[dict(mode=i['mode'], inject=i.get('inject'), expected_stdout=i['out'], expected_exit=0)
                    for i in p['inputs']],
            label_status='author-derived (Claude, 2026-11-23); checked against Go; needs team review'))
    (HERE / 'labels.json').write_text(json.dumps(labels, indent=1) + '\n')
    print(f"{len(labels)} programs, {sum(len(l['inputs']) for l in labels)} inputs, "
          f"{sum(1 for l in labels for i in l['inputs'] if i['inject'])} with injected failures")


if __name__ == '__main__':
    main()
