# Analysis and evaluation design

## Research question

For a deliberately restricted, sequential Go subset, what evidence is sufficient to replace
function-exit deferred resource actions with Rust ownership-based cleanup while preserving
observable acquisition, use, release, return and error behavior? Which sites require explicit
cleanup state or developer policy instead?

## Pipeline and implementation status

1. Go package loading and type checking: source implemented; execution pending.
2. SSA construction: source implemented with packages.LoadAllSyntax / ssautil.AllPackages;
   execution pending. An executed lexical inventory is an auxiliary fallback, not this stage.
3. Site discovery and SSA value origins: source implemented; execution pending.
4. Path-sensitive resource obligations and escape summaries: specified below; not implemented.
5. Policy classification: proposed rules; no automated safe-to-translate decision yet.
6. Rust generation and differential validation: November work, not a checkpoint achievement.

## Proposed abstract state

At a program point track: points-to candidates for each SSA value; resource state
(unacquired/open/closed/unknown); success/error branch predicates; pending cleanup entries
in registration order; escape reasons; whether cleanup writes a return slot or can fail.
Use allocation-site IDs plus dynamic instance counters in observed traces. A static site in
a loop is not one dynamic resource. At joins, retain path alternatives or conservatively
merge to unknown. Do not identify two resources merely because variable names match.

A direct deferred call evaluates its receiver/arguments at registration; a closure can retain
a mutable cell. At each function exit, assign explicit return expressions to result slots,
then execute the applicable stack in reverse registration order, then return the resulting
slots. Panic paths require separately modeled unwinding and recovery. os.Exit bypasses
Go defer and is outside the initial supported subset. Range-over-function defers and
concurrency are also excluded. An SSA RunDefers node is not all exceptional control flow.

## Proposed translation categories (not measured outputs)

- LOCAL_CANDIDATE: local resource, explicit successful acquisition, known close target,
  no escape, cleanup error unobserved, no result mutation, no unsupported unwind behavior;
  guard must remain alive until the source function's exit and preserve defer registration order.
- EXPLICIT_STATE: loops, conditional registration, mutable closure captures, named results,
  observable close errors, or differences between allocation and defer registration order.
  Use an explicit cleanup stack/epilogue or decline translation; proof obligations remain.
- OWNERSHIP_TRANSFER: returned or stored open resource; requires a proven caller contract.
- PRESERVE_AND_WARN: returned-closed or double-close source behavior. Diagnostics and
  repair suggestions must be separate from a fidelity-preserving translation.
- UNRESOLVED: unknown interface implementation, wrapper, alias, escape, concurrency,
  panic contract, or insufficient interprocedural summary. Never guess a unique owner.

## Better metrics than comparing unrelated IR variable lifetimes

Site metrics: exact source location matching, TP/FP/FN, precision and recall separately for
acquisition discovery, defer discovery, and acquisition-defer association edges. An unresolved
association is a false negative for recall, not a successful prediction. Record uncertain
edges separately from definite edges. Current gold associations are conditional on selected
inputs in the closure case; they are not universal path labels.

Execution metrics: translation success / attempted translations; compile success / emitted
programs; exact trace+stdout+stderr+exit matches / executed input pairs; end-to-end matching
input pairs / all scheduled pairs. Report blocked and unsupported counts separately. With
no executed pairs, equivalence is N/A, not 0% or 100%. Match occurrence identity, success
state, order, control-flow placement and usability observations. OS process teardown is not
an explicit Close event. Uninstrumented side effects require separate tests.

Performance: only after behavior passes, with uninstrumented binaries, a declared workload,
repeated independent runs, machine/compiler details and a distribution of times. Five means
alone do not eliminate noise. No performance experiment is included in this checkpoint.

## Threats and next experiments

Small synthetic corpus, author-created labels, no independent review, instrumentation effects,
limited branch coverage, no database semantics, no concurrency, no real-project frequency
estimate, no compiler execution and version compatibility yet. Add held-out human-reviewed
fixtures, imported package aliases and shadowing, branch joins, acquisition failure in each
position, loop lengths 0/1/N, independent post-return probes, and actual wrapper summaries.

Collect real-world snippets only after pinning repository SHAs and paths/lines and recording
licenses and extraction changes. Do not count authored fixtures as repository-mined evidence.
