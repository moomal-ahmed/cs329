from pathlib import Path
import json,html
from reportlab.platypus import SimpleDocTemplate,Paragraph,Spacer,Table,TableStyle,PageBreak,Preformatted,KeepTogether
from reportlab.lib import colors
from reportlab.lib.styles import getSampleStyleSheet,ParagraphStyle
from reportlab.lib.enums import TA_LEFT
from reportlab.lib.pagesizes import letter
R=Path(__file__).resolve().parents[1];out=R/'docs'; out.mkdir(exist_ok=True)
navy=colors.HexColor('#15334A');teal=colors.HexColor('#007E87');gray=colors.HexColor('#526373');light=colors.HexColor('#EDF4F6');amber=colors.HexColor('#8A4B0B')
ss=getSampleStyleSheet()
ss.add(ParagraphStyle(name='TitleRF',fontName='Helvetica-Bold',fontSize=26,leading=30,textColor=navy,spaceAfter=16))
ss.add(ParagraphStyle(name='SubRF',fontName='Helvetica',fontSize=12,leading=17,textColor=gray,spaceAfter=16))
ss.add(ParagraphStyle(name='HeadRF',fontName='Helvetica-Bold',fontSize=16,leading=20,textColor=navy,spaceAfter=12))
ss.add(ParagraphStyle(name='SmallHeadRF',fontName='Helvetica-Bold',fontSize=11,leading=15,textColor=teal,spaceBefore=10,spaceAfter=5))
ss.add(ParagraphStyle(name='BodyRF',fontName='Helvetica',fontSize=10,leading=14,spaceAfter=8))
ss.add(ParagraphStyle(name='SmallRF',fontName='Helvetica',fontSize=8.1,leading=11,spaceAfter=6))
ss.add(ParagraphStyle(name='CellRF',fontName='Helvetica',fontSize=8,leading=10.5))
ss.add(ParagraphStyle(name='CodeRF',fontName='Courier',fontSize=8,leading=10,spaceAfter=9))
def p(t,style='BodyRF'):return Paragraph(t,ss[style])
def table(headers,rows,widths=None):
 data=[[p('<font color="#FFFFFF"><b>'+html.escape(str(x))+'</b></font>','CellRF') for x in headers]]+[[p(html.escape(str(x)),'CellRF') for x in row] for row in rows]
 t=Table(data,colWidths=widths,repeatRows=1,hAlign='LEFT')
 t.setStyle(TableStyle([('BACKGROUND',(0,0),(-1,0),navy),('TEXTCOLOR',(0,0),(-1,0),colors.white),('ROWBACKGROUNDS',(0,1),(-1,-1),[colors.white,light]),('VALIGN',(0,0),(-1,-1),'TOP'),('LEFTPADDING',(0,0),(-1,-1),6),('RIGHTPADDING',(0,0),(-1,-1),6),('TOPPADDING',(0,0),(-1,-1),6),('BOTTOMPADDING',(0,0),(-1,-1),6),('LINEBELOW',(0,0),(-1,0),.5,navy)]))
 # Paragraph colors override table style, so set header paragraphs explicitly.
 for cell in data[0]:cell.style=ParagraphStyle(name='header',parent=ss['CellRF'],textColor=colors.white,fontName='Helvetica-Bold')
 return t

def footer(c,d):
 c.setStrokeColor(teal);c.setLineWidth(.7);c.line(42,41,570,41)
 c.setFont('Helvetica',8);c.setFillColor(gray);c.drawString(42,28,'RESCFLOW  |  CHECKPOINT EVIDENCE  |  26 OCT 2026');c.drawRightString(570,28,str(d.page))
def build(path,pages):
 story=[]
 for idx,page in enumerate(pages):
  if idx:story.append(PageBreak())
  story+=page
 doc=SimpleDocTemplate(str(path),pagesize=letter,rightMargin=42,leftMargin=42,topMargin=42,bottomMargin=55,title=path.stem.replace('_',' '),author='Prepared for Moomal Ahmed and Rama Chunduri')
 doc.build(story,onFirstPage=footer,onLaterPages=footer)
def head(n,t):return [p(f'{n}  /  {t}','HeadRF')]
def code(t):return Preformatted(t,ss['CodeRF'])
labels=json.loads((R/'benchmarks/labels.json').read_text());sources=json.loads((out/'research_register.json').read_text())
pages=[]
pages.append([
 p('RescFlow','TitleRF'),p('Weeks 1-2 research, benchmark and checkpoint package','SubRF'),
 p('Prepared for Moomal Ahmed and Rama Chunduri<br/>CS 329M | October 27, 2026 checkpoint<br/>Research checked October 26, 2026','BodyRF'),
 p('The defensible project claim','SmallHeadRF'),
 p('RescFlow investigates when Go resource-cleanup behavior can be translated into Rust ownership-based cleanup, and when explicit cleanup state or developer policy is required. The checkpoint contribution is a concrete benchmark, an executable semantics demonstration, a source inventory, and an SSA implementation awaiting compiler validation.'),
 table(['Deliverable','Evidence available now'],[
 ('Benchmark','20 authored Go programs; 23 input cases; 24 acquisition sites; 20 defer sites; source locations and draft expected traces.'),
 ('Executed inventory','24/24 acquisition spellings and 20/20 defers found on this corpus. Lexical only; no ownership inference.'),
 ('Executed semantic model','7 checks passed: 6 case/policy differences plus 1 negative control. Python model, not Go/Rust execution.'),
 ('SSA implementation','Go source provided for loading, SSA construction, site reporting and limited value-origin associations. Uncompiled.'),
 ('Baselines','20 Go2Rust and 20 Rustygo rows blocked by missing tools. No baseline success or failure rate.'),
 ('Research','17 primary-source entries, including 2026 work; inspection depth and applicability recorded.')],[112,416]),
 Spacer(1,12),p('<b>Important boundary:</b> Go, rustc and cargo are unavailable in this environment. Direct download attempts returned HTTP 403. Consequently, no claim in this package depends on an invented Go run, compiler result, translator defect, SSA accuracy score, or speedup.'),
 p('The proposal groups weeks 2 and 3 together (October 20-November 2). This package targets the October 27 checkpoint; it does not claim completion of the entire combined milestone.','SmallRF')])
pages.append(head('01','What the proposal requires')+[
 p('The supplied proposal defines a defer-aware analysis/translation pass, initially for files and potentially transactions. Its October 27 checkpoint is more concrete and smaller than the earlier experiment proposal: 20 small labeled programs, baseline attempts, an SSA demonstration, and limitations.'),
 table(['Commitment','Work delivered','Remaining acceptance step'],[
 ('Week 1: understand baselines','Current documentation compared; runtime choices and defer claims distinguished.','Pin revisions and inspect/run the exact code.'),
 ('Week 1: categories','Local, early-return, loop, capture, transfer, result/error, helper and interface patterns instantiated.','Revise using observed baseline evidence, not assumptions.'),
 ('Week 1: parser/SSA infrastructure','Go packages + x/tools SSA selected; implementation and commands supplied.','Compile and verify dependency/toolchain compatibility.'),
 ('Checkpoint: 20 labeled programs','20 files, 23 inputs, cleanup order and escape labels, review sheet.','Run Go oracles and complete independent teammate review.'),
 ('Checkpoint: baseline results','Preflight actually run; 40 blocked rows recorded.','Run emitted Rust and compare with observed Go.'),
 ('Checkpoint: analysis demo','Executed lexical demo; uncompiled SSA implementation.','Produce SSA output and score source-site associations.'),
 ('Checkpoint: limitations/next rules','Explicit inference limits, translation obligations, planned tests.','Implement and evaluate rules in November.')],[123,205,200]),
 p('Necessary corrections to the framing','SmallHeadRF'),
 p('File descriptors, transaction states, and locks are not interchangeable with heap memory. A resource-lifetime translation can be correct without demonstrating a GC or latency improvement. Safe Rust also does not establish that every cleanup action happens at the desired application-level time.'),
 p('Treat workload-specific migration stories as motivation, not a measured premise of this project. Do not characterize Go unsafe as a resource-leak remedy. Do not equate defer with lexical-block exit. The proposal also contains duplicate and mismatched numeric references; use the named source register in this package.'),
 p('Scope decision: use the promised 20 small checkpoint programs. No real repositories have been mined, and these programs must not be presented as 20 production codebases.')])
pages.append(head('02','The semantic problem to preserve')+[
 p('A translation needs a correspondence between resource events, not a visual resemblance between variables in two intermediate representations. The Go specification determines when a deferred action is registered and invoked; Rust drop scopes provide a different mechanism. [S1, S2]'),
 table(['Distinction','Why it matters','Benchmark'],[
 ('Function vs block exit','A guard inside a nested Rust block can run before a source Go defer would run.','06'),
 ('Dynamic order','Only executed defers are registered; reverse registration order is the relevant order.','05, 07, 08'),
 ('Value vs mutable cell','Saving an argument and retaining a closure cell can select different resources after reassignment.','13-15'),
 ('Cleanup affects result','A deferred closure may change the returned error. Dropping File alone does not preserve observed close errors. [S3]','16'),
 ('Handle can outlive open state','Returning a handle that was closed during return differs from transferring an open resource.','09, 10'),
 ('Abnormal exits','Recovery and unwinding need their own model; os.Exit does not run Go defers. [S4]','17; Exit excluded')],[126,352,50]),
 p('Operational contract for the proposed restricted subset','SmallHeadRF'),
 p('Identify each resource by acquisition site and, during execution, instance number. Record acquisition outcome, use outcome, close outcome, order, function-exit markers, returned values and exit status. For supported inputs, require matching observable sequences after normalizing only documented platform noise.'),
 p('A model of cleanup must preserve source defects when measuring translation fidelity. The return-closed and double-close examples are deliberate tests of this distinction. A future repair mode can change behavior only under a separately stated policy.'),
 p('A useful optimization obligation','SmallHeadRF'),
 p('Before replacing a defer with a guard, establish the same cleanup boundary, registration order, saved receiver, success-path condition, ownership/escape behavior and error/result effects. Merely finding one Open and one Close is insufficient. The current SSA source does not discharge these obligations.')])
pages.append(head('03','Research foundations and what they justify')+[
 table(['Work','Verified finding','RescFlow use / boundary'],[
 ('Oxidizer, PLDI 2025 [S9]','Combines feature mappings, type checks and I/O validation; its paper reports 73% of functions validated.','Borrow modular validation. This is not a cleanup-site accuracy result or proof for all inputs.'),
 ('Resource specification inference, OOPSLA 2023 [S10]','Infers allocation/ownership/release annotations in Java and C#; reports 85.5% of manual annotations recovered.','Use explicit resource summaries. Do not transfer the percentage to Go.'),
 ('Crown, 2023 [S11]','Uses static pointer ownership analysis to guide C-to-Rust translation.','Ownership is a foundation, but Go cleanup scheduling requires additional information.'),
 ('Repairing Leaks in Resource Wrappers, ASE 2025 [S14]','Analyzes wrappers and field containment for resource leak repair.','Add wrapper and field obligations; repair and semantics-preserving translation have different contracts.')],[126,198,204]),
 p('Evidence discipline','SmallHeadRF'),
 p('The full Oxidizer PDF was available and its abstract/design passages were inspected. The other findings here use author abstracts or proceedings metadata, not an exhaustive replication or full proof audit. The research register records the depth of inspection for every entry.'),
 p('Research question, narrowed','SmallHeadRF'),
 p('<b>Which statically evidenced resource/defer patterns admit a simpler Rust representation without changing observable cleanup behavior, and which must remain unresolved?</b> This formulation is testable without assuming that the existing translators are broken.'),
 p('Novelty status','SmallHeadRF'),
 p('The combination of resource obligations, defer timing, and representation classification is a proposed contribution. This focused literature search does not establish that RescFlow is the first such system. A novelty claim requires broader literature and artifact inspection.')])
pages.append(head('04','2026 updates and the baseline reality')+[
 table(['Recent source','Relevance to the checkpoint'],[
 ('External-library translation, Feb 2026 [S13]','Direct Go-to-Rust work on library API mappings and validation with opaque types. File/transaction APIs need a behavioral adapter, not only a type mapping.'),
 ('&inator, Apr 2026 [S12]','Uses constraints to connect correct interfaces with borrowing requirements. Motivates explicit constraints and conservative abstention; its guarantees are not imported into Go.'),
 ('Reboot, Jun 2026 [S15]','Provided-test and held-out validation outcomes differ. Keep independent examples to avoid measuring only the cases used to design the rules.'),
 ('TRACTOR benchmark, Sep 2026 [S16]','Separates correctness, safety, idiomaticity and performance. RescFlow should report these as separate dimensions.')],[150,378]),
 p('Go2Rust [S7]','SmallHeadRF'),
 p('The current README already lists defer, closure and named-return support and describes conservative wrapped representations. Its self-hosting fixture caveat is narrower than a demonstrated resource-cleanup defect in ordinary translation. No failure on this benchmark has been observed.'),
 p('Rustygo [S8]','SmallHeadRF'),
 p('The inspected repository is KarpelesLab/rustygo. Its documentation describes a Go-SSA frontend and a Rust runtime with tracing GC and defer support. Keeping GC is a design choice; it does not establish incorrect resource management. Its documented file/socket platform limitation makes Linux the initial test target.'),
 p('Implication','SmallHeadRF'),
 p('Compare both semantic fidelity and representation cost, but only after running pinned revisions. If both baselines pass every fixture, that is a useful result: shift the contribution toward classification, diagnostics or simpler representations, rather than manufacturing a bug claim.'),
 p('Freshness limit: checked through October 26, 2026 using primary sources and recent-paper searches. “Current” means the retrieved documentation, not a verified HEAD checkout or an exhaustive survey.','SmallRF')])
rows=[]
for c in labels:
 ac=','.join(str(s['line']) for s in c['sites'] if s['kind']=='acquire');ds=','.join(str(s['line']) for s in c['sites'] if s['kind']=='defer')
 rows.append((c['id'],c['name'],ac,ds,c['escape']))
pages.append(head('05','The 20-program labeled corpus')+[
 p('Each directory contains a standalone main.go. The scenario uses real os.Open; instrumentation reports actual Close and Stat outcomes. The JSON manifest contains full expected output for 23 selected inputs and the source-line labels below.'),
 table(['ID','Pattern','Acquire lines','Defer lines','Handle transfer'],rows,[27,145,75,75,206]),
 Spacer(1,8),p('24 static acquisition sites; 20 defer sites, including one recovery defer. Loop case 08 has one static site with three resource instances. IDs 02, 03 and 07 each have a second input. These counts are observed file-inventory results, not execution results.','SmallRF'),
 p('Labels are author-derived and unreviewed by teammates. The explicit failure case is 04. Most other acquisition-failure paths are outside the current oracles; case 14 has a path-specific B association. Do not interpret this corpus as exhaustive branch coverage.','SmallRF')])
pages.append(head('06','What actually ran, and what the numbers mean')+[
 table(['Check','Observed result','Evidence file'],[
 ('Source-manifest audit','20 programs, 23 inputs, 24 acquisitions, 20 defers; zero source-location inconsistencies.','results/corpus_audit.json'),
 ('Lexical acquisition inventory','TP=24, FP=0, FN=0 on the authored corpus.','results/lexical_inventory.json'),
 ('Lexical defer inventory','TP=20, FP=0, FN=0 on the authored corpus.','results/lexical_inventory.json'),
 ('Semantic model','7 checks pass; 6 case/policy pairs differ; 1 negative control agrees.','results/model_results.json'),
 ('Go oracle runner','20 blocked programs; 0 compiled or executed.','results/go_runs.json'),
 ('Baseline preflight','20 Go2Rust + 20 Rustygo rows blocked; 0 translators launched.','results/baselines.json'),
 ('Python syntax check','All supplied Python scripts passed py_compile.','Reproduce command in README context.')],[142,232,154]),
 p('Why the lexical score is deliberately narrow','SmallHeadRF'),
 p('The scanner reads source tokens independently of the RF label comments. It excludes comments and literals, then finds the spelling os.Open (and related constructors) and defer keywords. This verifies useful site-inventory machinery, not Go type resolution or resource ownership.'),
 table(['Control','Observed behavior','Interpretation'],[
 ('Comments / strings containing fake calls','No sites reported','Basic lexical filtering works for this control.'),
 ('A local parameter named os','An Open candidate is reported','Known semantic false positive.'),
 ('An os import aliased to fs','No acquisition candidate reported','Known semantic false negative.')],[178,170,180]),
 p('Do not advertise “100% resource-analysis accuracy.” The precise claim is exact lexical inventory on these 20 authored examples, with known failures outside their naming conventions. Acquisition-defer association accuracy and SSA precision/recall remain unmeasured.')])
pages.append(head('07','SSA implementation: concrete, not yet validated')+[
 p('The Go source at cmd/rescflow/main.go loads packages with syntax/type information, builds SSA, walks package functions and anonymous functions, and emits JSON source locations. The dependency is pinned to x/tools v0.52.0 with Go 1.27.0; this combination still needs a real build. [S5, S6]'),
 table(['Implemented in source','Deliberately unresolved'],[
 ('Recognize os.Open/OpenFile/Create/CreateTemp by static callee package/name.','Success-branch proof and partial-acquisition failure paths.'),
 ('Record Defer, RunDefers, Return, selected Store and explicit-close sites.','Exceptional cleanup coverage, path-dependent order and loop multiplicity proofs.'),
 ('Follow result Extract(0), simple Phi and conversion/interface wrappers.','Heap loads, field aliasing, mutable closure cells, general function summaries.'),
 ('Associate known Close receiver or opt-in benchmark closeFile argument.','Interface-dispatched Close, helper parameter origin and arbitrary wrapper contracts.'),
 ('Emit unknown status rather than claim a unique resource origin.','Proof that any site is safe for direct Rust generation.')],[264,264]),
 p('Important implementation limits','SmallHeadRF'),
 p('The initial traversal does not separately enumerate all method bodies. An unresolved or cyclic provenance path is not evidence of no resource. A store is reported as storage, not automatically an ownership transfer. Nil SSA constants contribute no acquisition origin. Candidate origins at Phi joins are not a path-sensitive association proof.'),
 code('go mod tidy\ngo build -o rescflow ./cmd/rescflow\n./rescflow -benchmark-summaries -ssa-dir results/ssa \\\n  ./benchmarks/... > results/ssa.json\npython3 scripts/evaluate_ssa.py'),
 p('The evaluator separately scores acquisition sites, defer sites, and association edges. Missing associations reduce recall. No results/ssa.json or fabricated SSA transcript is shipped. Dependency download, compilation, execution, and semantic debugging remain required.'),
 p('The working fallback is scripts/lexical_inventory.py. It must never be relabeled as the SSA prototype in the presentation.')])
c=labels[5];src=(R/c['file']).read_text().splitlines();snippet='\n'.join(f'{i+1:2}  {x}' for i,x in enumerate(src) if 6<=i<=16)
lex=json.loads((R/'results/lexical_inventory.json').read_text());sites=next(x['sites'] for x in lex['programs'] if x['id']=='06')
pages.append(head('08','Checkpoint demo: input to actual inventory')+[
 p('Case 06: nested lexical block. File: benchmarks/06_nested_block/main.go. The source intentionally puts defer inside a block and an observable event after that block.'),
 code(snippet),
 p('Actual output from the executed lexical inventory','SmallHeadRF'),code(json.dumps(sites,indent=2)),
 p('Draft expected Go trace (derived, not observed)','SmallHeadRF'),code('\n'.join(c['inputs'][0]['expected_stdout'])),
 p('What to explain aloud','SmallHeadRF'),
 p('“We can identify the acquisition and defer locations in source. The semantic question is whether cleanup happens before or after after-block. Go specifies function-exit cleanup. Our executable model distinguishes the wrong block-exit policy, but the Go SSA and translator executions are still pending.”'),
 p('This is a legitimate demo of the delivered inventory and a specification-backed explanation. It is not a screenshot of executed SSA or proof of a baseline bug.')])
m=json.loads((R/'results/model_results.json').read_text())
pages.append(head('09','Concrete cleanup counterexamples, carefully labeled')+[
 p('These are executed Python-model comparisons against intentionally incorrect translation policies. They are not outputs from Go2Rust, Rustygo, rustc, or Go.'),
 p('Counterexample A: block-local cleanup','SmallHeadRF'),
 table(['Function-exit model','Wrong block-exit policy'],[
 ('acquire A','acquire A'),('inside','inside'),('after-block','close A'),('close A','after-block')],[264,264]),
 p('The same resource is closed once in both traces, but at different observable positions. A count-only cleanup metric would miss the error.'),
 p('Counterexample B: eager argument versus late lookup','SmallHeadRF'),
 table(['Save argument at registration','Wrong lookup at function exit'],[
 ('acquire A','acquire A'),('register close(f), where f=A','register reference to variable f'),('assign f=B','assign f=B'),('close A','close B')],[264,264]),
 p('The converse also matters: a closure that reads a reassigned variable can intentionally close B. The model checks both directions; a blanket “always capture the old value” rule is also wrong.'),
 p('Other model checks','SmallHeadRF'),
 p('Reverse registration order; three unrolled loop iterations; named-result mutation (7 versus an incorrectly snapshotted 0); and a simple local negative control. Six case/policy pairs differ under five distinct bad rules. The local negative control agrees.'),
 p('Reproduce','SmallHeadRF'),code('python3 models/defer_model.py'),
 p('Limits: the model operates on a small hand-authored operation language, not arbitrary Go source. It does not model OS files, failed acquisitions, panic/recover, exceptions during cleanup, concurrency or borrow checking. Its purpose is to test the specification and test-oracle design.')])
pages.append(head('10','Baseline experiment and valid denominators')+[
 p('The supplied harness actually performed preflight. It found no Go, Rust, Cargo or baseline executables. Network probes also returned a denied response. It recorded all 40 rows as blocked_environment. Baseline equivalence is N/A.'),
 p('Execution protocol to finish on Linux','SmallHeadRF'),
 p('1. Clone the two named repositories and record commit SHA, dirty status and tool versions. Build each with its own documented dependencies. Run a hello-world smoke test before testing files.'),
 p('2. Compile and execute the original Go corpus. Reconcile any mismatch with the expected trace before using that trace as an oracle. Save raw stdout, stderr and process exit status.'),
 p('3. Translate without editing output to conceal errors. Save emitted Rust and command logs. Build using the upstream supported layout; unsupported API and adapter-integration problems receive their own labels.'),
 p('4. Run the same 23 input cases and compare with observed Go. Preserve the first divergence and minimize any counterexample. A compilation failure is not a cleanup-timing failure.'),
 table(['Metric','Numerator / denominator'],[
 ('Translation success','Successful emissions / translator invocations actually launched.'),
 ('Compilation success','Supported build successes / emitted programs submitted to build.'),
 ('Conditional behavioral match','Matching observed input pairs / executed Go-Rust pairs.'),
 ('End-to-end coverage','Matching observed input pairs / 23 scheduled inputs per tool; show blockers separately.'),
 ('Site/association quality','TP/(TP+FP), TP/(TP+FN), with exact location/edge definitions.'),
 ('Performance','Only behavior-passing, uninstrumented workloads; no current number.')],[162,366]),
 p('Adapter caveats','SmallHeadRF'),
 p('Go2Rust initially uses stdout-to-rustc single-file mode. Dependency/workspace failures need an upstream build adapter before attributing them to the translator. Rustygo emit and build are invoked separately, but build can regenerate code: the exact saved emitted tree is not independently compiled by this initial adapter. Compiler-dependent paths have not been smoke-tested.')])
pages.append(head('11','Next implementation rules and acceptance tests')+[
 table(['Candidate rule','Evidence required','Acceptance examples'],[
 ('Function-lifetime guard','Known local handle, successful acquisition, saved receiver, compatible error policy, same cleanup boundary and order.','01, 03, 05, 06; negative cases must block promotion.'),
 ('Conditional registration','Guard/stack entry exists only on the executed registration path.','02, 04, 07; add failure before/after each registration.'),
 ('Explicit cleanup stack','Dynamic registrations preserved in LIFO order; loop instances distinguished.','08 with 0, 1, 3 and larger iteration counts.'),
 ('Capture-aware representation','Separate saved value from captured mutable cell; preserve cleanup target.','12-15; add branch joins and reassignment failures.'),
 ('Return-slot epilogue','Cleanup mutates the real result before it is returned; close errors observed faithfully.','16; add a genuinely failing custom closer.'),
 ('Transfer/diagnostic rule','Open-resource return distinguished from returned-closed or stored handles.','09-11, 18-20; use caller contracts and abstain when unknown.')],[131,255,142]),
 p('Prioritized plan for October 27-November 2','SmallHeadRF'),
 p('<b>Moomal:</b> compile the SSA pass, inspect exact baseline implementations, and pin versions. <b>Rama:</b> run Go oracles and baseline adapters, preserve artifacts, and check label consistency. <b>Together:</b> independently review each other\'s labels, inspect unresolved associations, and select the first supported subset from evidence.'),
 p('November 3-9 translation gate','SmallHeadRF'),
 p('Implement the simplest proven normal-return rule first. Require passing Go-oracle validation, correct source-site matching, explicit unsupported diagnostics and matching resource-event traces before widening the subset. Named-result errors and general panic/recover should remain excluded until their contracts are implemented.'),
 p('If baseline translations all pass, report that result and test whether RescFlow can reduce representation machinery while preserving behavior. Do not redefine success to require an existing tool to fail.')])
pages.append(head('12','Claim ledger and presentation boundaries')+[
 table(['Safe to present','Not supported yet'],[
 ('We authored a concrete 20-program, 23-input corpus.','We evaluated 20 real-world codebases.'),
 ('Our lexical inventory found all labeled sites in this corpus.','Our SSA resource analysis has 100% precision/recall.'),
 ('The model exposes wrong cleanup timing and capture rules.','Go2Rust/Rustygo exhibit these failures.'),
 ('SSA source and scoring infrastructure are supplied.','The Go SSA prototype has been compiled or demonstrated.'),
 ('Current baseline documentation already claims defer support.','Existing transpilers generally fail defer or cleanup.'),
 ('We have a proposed ownership/cleanup classification.','We have proved safe or optimal Rust representations.'),
 ('Compiler execution was blocked by the environment.','Blocked executions count as translator failures.'),
 ('Resource-lifetime fidelity is the measured goal we propose.','Rust necessarily reduces GC, memory, CPU or latency here.')],[264,264]),
 p('Limitations that must stay visible','SmallHeadRF'),
 p('Synthetic cases; no independently reviewed labels; no executed Go oracle; no compiler-verified SSA; no baseline checkout pin; limited failure-path coverage; instrumentation changes observability; Stat only tests a narrow form of usability; no database, concurrent or real-project evaluation. A full equivalence proof and broad novelty claim are outside this checkpoint.'),
 p('Recommended checkpoint wording','SmallHeadRF'),
 p('“We have narrowed RescFlow to resource-event fidelity and built a labeled semantic test suite. We ran a source inventory and an executable model that distinguishes cleanup-timing and capture mistakes. We have written the SSA pass, but compiler and baseline runs were blocked in the preparation environment. We therefore report no baseline bugs or translation success rates yet. Our next gate is to compile the pass, validate Go traces, and measure the baselines on pinned revisions.”'),
 p('Submission integrity','SmallHeadRF'),
 p('Human review remains pending. This package does not impersonate completed teammate review, a finished compiler experiment, or observed results. If the checkpoint requires a working SSA demo, that deliverable is still outstanding despite the supplied implementation.')])
for a,b in [(0,9),(9,17)]:
 page=head('REF','Primary-source register'+(' (continued)' if a else ''))
 for s in sources[a:b]:
  page.append(p(f"<b>[{s['id']}] {html.escape(s['title'])}</b>",'SmallRF'))
  page.append(p(f"<link href=\"{html.escape(s['url'],quote=True)}\" color=\"#007E87\">{html.escape(s['url'])}</link>",'SmallRF'))
  page.append(p('Inspection: '+html.escape(s['inspection'])+'.','SmallRF'))
 page.append(p('All sources accessed October 26, 2026. Full finding/implication mapping is in research_register.json. The uploaded project proposal is the source of milestone requirements. External-paper findings are author-reported, not reproduced here.','SmallRF'))
 pages.append(page)
build(out/'checkpoint_report.pdf',pages)
brief=[]
brief.append([p('RescFlow checkpoint','TitleRF'),p('October 27, 2026 | presentation brief','SubRF'),
 p('1. Problem and contribution (about 90 seconds)','SmallHeadRF'),
 p('Go resource cleanup can depend on function-exit timing, dynamic defer order, capture behavior and return-slot mutation. RescFlow asks when ownership-based Rust cleanup preserves those effects, and when explicit state or developer policy is needed.'),
 p('2. Concrete progress (about 90 seconds)','SmallHeadRF'),
 table(['Prepared or executed','Status'],[
 ('20 Go programs / 23 selected inputs','Authored; expected traces are draft labels.'),
 ('24 acquisitions / 20 defers','Source-manifest counts verified.'),
 ('Lexical inventory','24/24 + 20/20 found; no type or ownership claim.'),
 ('Python semantic model','7 checks pass; 6 case/policy distinctions + 1 control.'),
 ('Go SSA implementation','Source supplied; compilation and execution pending.'),
 ('Go2Rust / Rustygo experiments','40 preflight rows blocked; no success/failure rates.')],[270,258]),
 Spacer(1,14),p('<b>Main takeaway:</b> the checkpoint has specific inspectable artifacts and two runnable demonstrations. It does not yet have the compiler-backed SSA and baseline evidence promised in the full plan.'),
 p('Evidence: corpus_audit.json, lexical_inventory.json, model_results.json, go_runs.json and baselines.json. See the full report for commands, caveats and sources.','SmallRF')])
brief.append(head('DEMO','Two minutes of concrete evidence')+[
 p('Show case 06, then run the lexical inventory and the model. Describe each output accurately; no generated or expected transcript should be called an observed Go execution.'),
 code('python3 scripts/lexical_inventory.py\npython3 models/defer_model.py'),
 p('Source pattern','SmallHeadRF'),code('func scenario() {\n    {\n        f, err := os.Open(path)\n        if err != nil { return }\n        defer closeFile("A", f)\n        fmt.Println("inside")\n    }\n    fmt.Println("after-block")\n}'),
 table(['Function-exit model','Wrong block-exit policy'],[
 ('acquire A','acquire A'),('inside','inside'),('after-block','close A'),('close A','after-block')],[264,264]),
 Spacer(1,10),p('Explain: “Both traces close A once. The wrong trace still closes it at an observably different point. We therefore need order and boundary checks, not just counts.”'),
 p('Second example: register close(f) while f=A, then reassign f=B. Saving the argument closes A; incorrectly looking up f at exit closes B. A closure that intentionally reads f has the opposite requirement. [S1]'),
 p('The model demonstrates these distinctions. It does not show a defect in either named baseline. The source excerpt here is simplified for presentation; the full fixture includes outcome tracing.')])
brief.append(head('NEXT','Research update, limits and next gate')+[
 p('Research update (about 60 seconds)','SmallHeadRF'),
 p('Go2Rust and Rustygo already document defer support. Rustygo retains GC by design. The February 2026 external-library translation preprint is directly relevant to opaque file handles; &inator motivates constraints; the September 2026 TRACTOR report motivates separate correctness and performance metrics. [S7, S8, S12, S13, S16]'),
 p('Limitations to state (about 45 seconds)','SmallHeadRF'),
 p('The corpus is synthetic and labels await teammate review. Go/Rust toolchains were absent and downloads denied. The SSA pass is uncompiled; baseline runs were not launched. Lexical results do not measure ownership or cleanup associations. No performance claim is supported.'),
 p('Next acceptance gate (about 45 seconds)','SmallHeadRF'),
 p('On a networked Linux machine: pin repositories and toolchains; compile/run the Go oracles; build and score the SSA pass; smoke-test baseline build adapters; compare observed traces for all 23 inputs; preserve the first concrete mismatch if any. Then choose the first translation rule from the evidence.'),
 p('Likely questions','SmallHeadRF'),
 table(['Question','Defensible answer'],[
 ('Did you find baseline bugs?','No. We have designed counterexamples to wrong policies; baseline experiments remain blocked.'),
 ('Is the SSA prototype working?','Implementation is written, but not compiler-validated here. The demonstrated inventory is lexical.'),
 ('Why not use ordinary Drop everywhere?','The source can depend on boundary, order, capture and error effects. Each replacement needs evidence.'),
 ('What if both baselines pass?','Report the passes; evaluate representation simplification and diagnostics without claiming a semantic advantage.'),
 ('What is the result so far?','A labeled corpus, exact source inventory on it, executable semantic checks, and a concrete analysis/evaluation implementation.')],[178,350]),
 Spacer(1,8),p('Source IDs resolve in checkpoint_report.pdf and docs/research_register.json. This brief is intended as an honest checkpoint script, not a declaration that every planned week-2 experiment is complete.','SmallRF')])
build(out/'checkpoint_brief.pdf',brief)
print('Built',len(pages),'planned report pages and',len(brief),'brief pages')
