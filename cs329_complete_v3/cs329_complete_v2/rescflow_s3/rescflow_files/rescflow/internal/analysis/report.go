package analysis

// Report is the analyzer output: one record per acquisition site, plus per-function exits,
// summaries, and everything that could not be analyzed, with an explanation.
type Report struct {
	Schema    string          `json:"schema"`
	Go        string          `json:"go"`
	Patterns  []string        `json:"patterns"`
	Packages  []PackageReport `json:"packages"`
	Records   []*Record       `json:"records"`
	Functions []*FuncReport   `json:"functions"`
	Defers    []*DeferFact    `json:"defers"`
	Traces    []*ProgramTrace `json:"predicted_traces,omitempty"`
	// step 3
	Decisions        []*Decision        `json:"decisions"`
	ProgramDecisions []*ProgramDecision `json:"program_decisions"`
	// step 6
	Reviews    []*ReviewReport `json:"reviews"`
	Generated  []*GenOutput    `json:"generated,omitempty"` // step 5
	Unresolved []Unresolved    `json:"unresolved"`
}

type PackageReport struct {
	Path      string       `json:"path"`
	Analyzed  bool         `json:"analyzed"`
	LoadIssue []Unresolved `json:"load_issues,omitempty"`
}

// Record: everything known about one resource (one acquisition site).
type Record struct {
	ID           string       `json:"id"`
	Var          string       `json:"var"`
	Func         string       `json:"func"`
	File         string       `json:"file"`
	Line         int          `json:"line"`
	Acquire      string       `json:"acquire"`
	InLoop       bool         `json:"in_loop"`
	InGoroutine  bool         `json:"in_goroutine"`
	SuccessGuard string       `json:"success_guard"`
	Events       []Event      `json:"events"`
	Defers       []*DeferFact `json:"defers"`
	Escapes      []string     `json:"escapes"`
	Anomalies    []string     `json:"anomalies"`
	Notes        []string     `json:"notes"`
}

type Event struct {
	Func  string `json:"func"`
	Line  int    `json:"line"`
	Kind  string `json:"kind"`  // acquire | use | release
	Via   string `json:"via"`   // how: direct API call, through a callee summary, by a deferred call at exit
	State string `json:"state"` // resource state just before the event
}

// DeferFact describes one defer statement that may act on a resource.
type DeferFact struct {
	Func         string              `json:"func"`
	Line         int                 `json:"line"`
	Call         string              `json:"call"`
	Targets      []string            `json:"targets"`
	Captures     []string            `json:"captures"`     // per input: value fixed at registration, or cell read at exit
	Registration string              `json:"registration"` // always | conditional | loop
	Resolves     map[string][]string `json:"resolves"`     // exit -> resources this defer acts on there
	CloseErrorTo string              `json:"close_error_to,omitempty"`
}

type FuncReport struct {
	Name    string   `json:"name"`
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Exits   []Exit   `json:"exits"`
	Summary *Summary `json:"summary"`
	SSA     []string `json:"ssa,omitempty"` // annotated SSA listing (-explain)
}

type Exit struct {
	Line int      `json:"line"`
	Kind string   `json:"kind"` // return | panic | panic->recovered
	Pops []Pop    `json:"defers_run"`
	Open []string `json:"still_open,omitempty"`
}

type Pop struct {
	DeferLine int      `json:"defer_line"`
	Acts      []string `json:"acts_on"`
}

// Unresolved explains, in detail, why a fact needed for translation is not available.
type Unresolved struct {
	Kind        string   `json:"kind"`
	Func        string   `json:"func,omitempty"`
	File        string   `json:"file,omitempty"`
	Line        int      `json:"line,omitempty"`
	Resources   []string `json:"resources,omitempty"`
	Construct   string   `json:"construct"`
	MissingFact string   `json:"missing_fact"`
	Why         string   `json:"why"`
	Consequence string   `json:"consequence"`
	HumanInput  string   `json:"human_input"`
	Detail      string   `json:"detail,omitempty"`
}

var catalog = map[string]Unresolved{
	"load-list": {
		Construct:   "package could not be loaded",
		MissingFact: "the package's files and dependencies",
		Why:         "the go command could not resolve the package: a missing module or dependency, a bad import path, build constraints that exclude every file, or a go.mod that requires a newer Go than the analyzer's toolchain",
		Consequence: "no syntax tree, types or SSA exist for this package, so none of its resources can be analyzed",
		HumanInput:  "make `go build` succeed for the package with the same toolchain (fix go.mod / vendor dependencies / Go version)",
	},
	"load-parse": {
		Construct:   "source does not parse",
		MissingFact: "a syntax tree",
		Why:         "the file is not syntactically valid Go; SSA is built from typed syntax trees, so nothing downstream can run",
		Consequence: "the package is skipped entirely",
		HumanInput:  "fix the syntax error at the reported position",
	},
	"load-type": {
		Construct:   "source does not type-check",
		MissingFact: "complete type information",
		Why:         "go/ssa only builds well-typed packages: every call target, method set and conversion must be known. An ill-typed package cannot be lowered to SSA soundly",
		Consequence: "the package is skipped entirely",
		HumanInput:  "fix the type error (or supply the missing dependency) so that `go vet` passes",
	},
	"ssa-build": {
		Construct:   "SSA construction failed",
		MissingFact: "the SSA form of the package",
		Why:         "go/ssa panicked while lowering the package (usually a language feature newer than the x/tools version in use)",
		Consequence: "the package is skipped entirely",
		HumanInput:  "use a golang.org/x/tools release that supports the package's Go version",
	},
	"goroutine": {
		Construct:   "resource acquired, used or released inside a goroutine (`go` statement)",
		MissingFact: "the happens-before order between the goroutine's cleanup and the rest of the program",
		Why:         "the analysis models a single sequential call stack. A defer inside a goroutine runs when that goroutine's function returns; that moment is ordered with the parent only through synchronization (WaitGroup, channels, mutexes) whose semantics RescFlow does not interpret",
		Consequence: "the cleanup point relative to the caller is unknown; a Rust translation would also need Send/'static ownership (e.g. move into the thread, Arc<Mutex<..>>) that the analysis cannot justify",
		HumanInput:  "state the synchronization contract (e.g. `wg.Wait()` joins the goroutine before the caller continues) and whether the resource may move to another thread",
	},
	"dispatch-multi": {
		Construct:   "interface method call with several possible implementations",
		MissingFact: "which concrete type receives the call at run time",
		Why:         "the interface value is built from different concrete types on different paths, and the implementations do not have the same effect on the resource (one releases it, another does not)",
		Consequence: "release is path-dependent: on at least one path the resource is never closed by this call. No single ownership plan covers both",
		HumanInput:  "decide which implementation is intended to own cleanup, or make the close explicit on every path",
	},
	"dispatch-unknown": {
		Construct:   "interface method call whose concrete receiver types are unknown",
		MissingFact: "the set of implementations that may run",
		Why:         "the interface value comes from a parameter, a load, or a call the analysis cannot see into, so no method body can be chosen",
		Consequence: "the call may release, retain or leak the resource; it is treated as an escape",
		HumanInput:  "name the implementations that can reach this call site (or restrict the parameter type)",
	},
	"dynamic-call": {
		Construct:   "call through a function value",
		MissingFact: "which function body runs",
		Why:         "the callee is a variable (parameter, field, map entry), not a statically known function or a closure created in the same function",
		Consequence: "the call may release or keep the resource; it is treated as an escape",
		HumanInput:  "identify the possible callees, or pass the resource only to statically known functions",
	},
	"unknown-callee": {
		Construct:   "resource passed to a function without an analyzable body",
		MissingFact: "what the callee does with the resource",
		Why:         "the callee lives outside the analyzed packages (standard library without a model, assembly, cgo) so no summary exists",
		Consequence: "the resource is treated as escaping at this call: ownership after the call is unknown",
		HumanInput:  "add a summary for the callee to the API model (does it close, use, store or return the resource?)",
	},
	"recursion": {
		Construct:   "recursive call cycle",
		MissingFact: "a finished summary for the callee",
		Why:         "summaries are computed bottom-up over the call graph; a cycle has no bottom, and this prototype does not iterate summaries to a fixpoint",
		Consequence: "the recursive call is treated as unknown; resources passed through it escape",
		HumanInput:  "confirm the recursion's resource contract (e.g. each level closes what it opens)",
	},
	"collection": {
		Construct:   "resource stored in a slice, array element, map or channel",
		MissingFact: "which element holds which resource",
		Why:         "the analysis does not distinguish elements of collections, so every element is one abstract cell and updates are never exact",
		Consequence: "ownership is shared by the whole collection; cleanup order of individual elements is unknown",
		HumanInput:  "state whether the collection owns its resources (Vec<File> dropped together) and in which order they must be closed",
	},
	"exit-bypass": {
		Construct:   "os.Exit / log.Fatal while a deferred cleanup or open resource is pending",
		MissingFact: "none: the behavior is known, Go skips deferred calls",
		Why:         "os.Exit terminates immediately without running defers; Rust's std::process::exit also skips destructors, but RescFlow's initial scope excludes abrupt exit",
		Consequence: "the pending cleanup never runs; the OS reclaims the descriptor at process exit",
		HumanInput:  "confirm that skipping cleanup on this path is intended",
	},
	"unsafe": {
		Construct:   "resource converted to unsafe.Pointer or passed to reflect",
		MissingFact: "all aliases of the resource",
		Why:         "unsafe and reflect create aliases the type system cannot see",
		Consequence: "ownership and release facts for this resource cannot be trusted",
		HumanInput:  "remove the unsafe/reflect use or describe the aliasing",
	},
	"closure-escape": {
		Construct:   "closure capturing a resource escapes the function",
		MissingFact: "when (or whether) the closure runs",
		Why:         "the closure is returned or stored instead of being called or deferred here, so its effects happen at an unknown later time",
		Consequence: "the captured resource's lifetime extends beyond the function; treated as an escape",
		HumanInput:  "state who calls the closure and whether it must own the resource",
	},
}

func explain(kind string) Unresolved {
	u := catalog[kind]
	u.Kind = kind
	return u
}
