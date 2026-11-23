# Final unseen set (sealed)

10 programs (U01–U10, 22 inputs, 7 with injected failures), written on 2026-11-23 **before** the
step-3 rules existed. **Do not run `rescflow`, `evaluate_analysis.py` or `check_traces.py` on this
folder until the final evaluation.** Only real Go has been run on it, to confirm the labels:

    python3 scripts/run_heldout_go.py /path/to/go1.26.5 --labels=testdata/final_unseen/labels.json   # 22/22

It sits under `testdata/` so that `./...` (rescflow's default pattern) never includes it.
`SEAL.txt` holds the sha256 of every file here; recompute it before the final run to show
that nothing changed after sealing.

| ID | What it probes (new compared with the 34 earlier programs) | Expected |
|---|---|---|
| U01 | two files, one plain defer + one close-error join into `err` | A |
| U02 | retry loop: `continue` on failure, defer + `break` on success | A |
| U03 | a helper replaces the file through `**os.File`; the deferred closure reads it at exit | A |
| U04 | a struct owns two files; a deferred method closes both | A |
| U05 | explicit closes, but one early return leaks | C |
| U06 | file passed to `bufio.NewReader` (unmodelled library code) | C |
| U07 | cleanup closure stored in a variable, deferred later | A |
| U08 | on one path the file is stored in a global map, closed by `main` | C |
| U09 | the callee that owns the file panics; the caller recovers | A |
| U10 | ownership moves out through an out-parameter on one path | B |

Caveat: Claude wrote these after building the analysis, so they are unseen by the *tools*, not by
their author. For a stronger claim, a teammate should add 2–3 programs of their own here (then
rerun `make_unseen.py` only if they extend it, and re-seal).
