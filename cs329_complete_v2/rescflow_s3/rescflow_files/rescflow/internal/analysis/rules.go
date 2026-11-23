package analysis

// Step 3: translation rules. Each resource record is classified A / B / C from the facts the
// analysis recorded, and every decision carries the rule that fired and the facts behind it.
//
//	C  needs review: a fact required for translation is missing, or the program's behaviour
//	   conflicts with Rust ownership (use after close, double close, a path that leaks).
//	B  several valid plans: ownership leaves the function on one path and is cleaned up locally
//	   on another; a documented choice rule picks one.
//	A  one supported plan, chosen from the facts.
//
// The rules only read the record; they never guess. A fact that is not in the record is not used.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Decision is the step-3 output for one resource.
type Decision struct {
	Resource   string   `json:"resource"`
	Var        string   `json:"var"`
	Func       string   `json:"func"`
	Category   string   `json:"category"` // A | B | C
	Plan       string   `json:"plan"`     // template name(s), main one first
	Rust       string   `json:"rust"`     // the Rust shape the plan stands for
	Rule       string   `json:"rule"`     // which rule decided
	Facts      []string `json:"facts"`    // the facts that decided it
	Review     []string `json:"review,omitempty"`
	ChoiceRule string   `json:"choice_rule,omitempty"` // B only
}

// ProgramDecision: a program's category is its worst resource (C > B > A).
type ProgramDecision struct {
	Package   string   `json:"package"`
	Category  string   `json:"category"`
	Resources []string `json:"resources"`
}

var rustShape = map[string]string{
	"OWNED_GUARD":        "let f = DeferClose::new(id, file); // owned; dropped at function exit, in reverse declaration order",
	"OPTION_GUARD":       "let mut g: Option<DeferClose> = None; if cond { g = Some(DeferClose::new(id, file)) }",
	"DEFER_STACK":        "let mut defers = DeferStack::new(); /* in the loop */ defers.push(DeferClose::new(id, file)); // popped LIFO at exit",
	"CELL_CLOSURE_GUARD": "let f = RefCell::new(Some(file)); let _g = Defer::new(|| close_file(f.borrow_mut().take())); // reads the cell at exit",
	"RESULT_SLOT_GUARD":  "let result = RefCell::new(Ok(())); let _g = Defer::new(|| if let Err(e) = close_file(f) { /* write result */ }); // explicit close, error observed",
	"CALLEE_OWNS":        "fn callee(f: File) { let _g = DeferClose::new(id, f); ... } // ownership moves into the callee",
	"DYN_GUARD":          "let c: Box<dyn Closer> = ...; let _g = Defer::new(move || { c.close(); });",
	"OWNERSHIP_RETURN":   "fn producer() -> Option<File> // ownership moves to the caller",
	"GLOBAL_OWNER":       "static SLOT: Mutex<Option<File>> // ownership moves into the global",
	"OUT_PARAM_TRANSFER": "fn producer(out: &mut Option<File>) // ownership moves through the out-parameter",
	"EXPLICIT_CLOSE":     "close_file(f) // consumes the File at the explicit close",
	"CATCH_UNWIND":       "std::panic::catch_unwind around the body; guards drop during unwinding",
	"REVIEW":             "no translation emitted: review report",
}

var reUnresolved = regexp.MustCompile(`^UNRESOLVED (\S+) at line (\d+): (.*)$`)

// Classify adds a decision per resource record and a category per program.
func Classify(rep *Report) {
	rep.Decisions = []*Decision{}
	byPkg := map[string]*ProgramDecision{}
	rank := map[string]int{"A": 0, "B": 1, "C": 2}
	for _, r := range rep.Records {
		d := classify(r)
		rep.Decisions = append(rep.Decisions, d)
		pkg := r.Func[:strings.Index(r.Func, ".")]
		pd := byPkg[pkg]
		if pd == nil {
			pd = &ProgramDecision{Package: pkg, Category: "A"}
			byPkg[pkg] = pd
		}
		pd.Resources = append(pd.Resources, fmt.Sprintf("%s@%d=%s", r.Var, r.Line, d.Category))
		if rank[d.Category] > rank[pd.Category] {
			pd.Category = d.Category
		}
	}
	// a package that could not be analyzed, or whose unresolved facts touch no record, is C
	for _, u := range rep.Unresolved {
		if u.Func == "" {
			continue
		}
		pkg := u.Func[:strings.Index(u.Func, ".")]
		if pd := byPkg[pkg]; pd == nil {
			byPkg[pkg] = &ProgramDecision{Package: pkg, Category: "C", Resources: []string{"(no record) " + u.Kind}}
		}
	}
	rep.ProgramDecisions = []*ProgramDecision{}
	for _, pd := range byPkg {
		rep.ProgramDecisions = append(rep.ProgramDecisions, pd)
	}
	sort.Slice(rep.ProgramDecisions, func(i, j int) bool { return rep.ProgramDecisions[i].Package < rep.ProgramDecisions[j].Package })
}

func classify(r *Record) *Decision {
	d := &Decision{Resource: r.ID, Var: r.Var, Func: r.Func, Facts: []string{}}
	fact := func(f string, a ...any) { d.Facts = append(d.Facts, fmt.Sprintf(f, a...)) }

	// ---- C: missing facts or conflicts with ownership (all reasons are listed) ----
	var review []string
	if r.InGoroutine {
		review = append(review, "acquired inside a goroutine: cleanup order relative to the caller is unknown")
	}
	for _, n := range r.Notes {
		if m := reUnresolved.FindStringSubmatch(n); m != nil {
			review = append(review, fmt.Sprintf("fact unavailable (%s, line %s): %s", m[1], m[2], m[3]))
		}
	}
	for _, a := range r.Anomalies {
		switch {
		case strings.HasPrefix(a, "use after close"):
			review = append(review, "uses the file after it is closed ("+a+"); Rust ownership rejects this. Which side should own the file?")
		case strings.HasPrefix(a, "double close"):
			review = append(review, "closes the file twice ("+a+"); which close is the real one?")
		case strings.HasPrefix(a, "possible use after close"), strings.HasPrefix(a, "possible double close"):
			review = append(review, "on some path: "+a)
		case strings.HasPrefix(a, "may still be open"):
			review = append(review, "a path leaves the file open ("+a+"); is that intended?")
		case strings.HasPrefix(a, "Close of a failed acquisition"):
			review = append(review, a)
		case strings.HasPrefix(a, "released by some but not all"):
			review = append(review, a)
		}
	}
	for _, e := range r.Escapes {
		low := strings.ToLower(e)
		for _, k := range []string{"map", "channel", "slice", "goroutine", "function value", "unknown", "no analyzable body", "recursion"} {
			if strings.Contains(low, k) {
				review = append(review, "escapes where ownership cannot be followed: "+e)
				break
			}
		}
	}
	if len(review) > 0 {
		d.Category, d.Plan, d.Rule, d.Review = "C", "REVIEW", "C: missing fact or ownership conflict", dedupe(review)
		for _, x := range d.Review {
			fact("%s", x)
		}
		d.Rust = rustShape["REVIEW"]
		return d
	}

	// ---- facts used by the A/B rules ----
	var returned, global, outParam bool
	for _, e := range r.Escapes {
		switch {
		case strings.Contains(e, "returned to the caller"):
			returned = true
			fact("escape: %s", e)
		case strings.Contains(e, "stored in global"):
			global = true
			fact("escape: %s", e)
		case strings.Contains(e, "stored into memory reachable from input"):
			outParam = true
			fact("escape: %s", e)
		}
	}
	var own, other []*DeferFact // defers in the acquiring function / elsewhere
	for _, df := range r.Defers {
		if df.Func == r.Func {
			own = append(own, df)
		} else {
			other = append(other, df)
		}
	}
	explicit := false
	for _, ev := range r.Events {
		if ev.Kind == "release" && !strings.HasPrefix(ev.Via, "defer@") {
			explicit = true
		}
	}

	// ---- B: ownership leaves on one path, local cleanup on another ----
	if (returned || outParam || global) && len(own) > 0 {
		d.Category, d.Rule = "B", "B: transfer on one path, local defer on another"
		how := map[bool]string{true: "OWNERSHIP_RETURN"}[returned]
		if outParam {
			how = "OUT_PARAM_TRANSFER"
		} else if global && !returned {
			how = "GLOBAL_OWNER"
		}
		d.Plan = how + " | OWNED_GUARD"
		for _, df := range own {
			fact("defer@%d in %s (%s) acts on it", df.Line, df.Func, df.Registration)
		}
		d.ChoiceRule = "the defer is registered only on paths that keep the file, so: move the file out (" + how +
			") on the transferring path, and create the guard on the others; the producer's result becomes Option<File>"
		d.Rust = rustShape[how] + " + " + rustShape["OWNED_GUARD"]
		return d
	}

	// ---- A: one plan ----
	d.Category, d.Rule = "A", "A: single plan from the facts"
	var plan []string
	add := func(p string) {
		for _, x := range plan {
			if x == p {
				return
			}
		}
		plan = append(plan, p)
	}
	switch {
	case returned:
		add("OWNERSHIP_RETURN")
	case outParam:
		add("OUT_PARAM_TRANSFER")
	case global:
		add("GLOBAL_OWNER")
	}
	unwind := false
	for _, df := range append(append([]*DeferFact{}, own...), other...) {
		fact("defer@%d in %s: registration %s; %s", df.Line, df.Func, df.Registration, strings.Join(df.Captures, "; "))
		cell := false
		for _, c := range df.Captures {
			cell = cell || strings.Contains(c, "read at exit")
		}
		sets := map[string]bool{}
		for exit, acts := range df.Resolves {
			if strings.HasPrefix(exit, "exit of ") {
				continue // the caller's view of a callee defer
			}
			if strings.HasPrefix(exit, "panic") {
				unwind = true
			}
			sets[strings.Join(acts, ",")] = true
			if len(acts) > 1 {
				sets["multi"] = true
			}
		}
		pathDependent := len(sets) > 1
		switch {
		case df.Func != r.Func && !returned:
			add("CALLEE_OWNS")
			fact("cleanup happens inside callee %s", df.Func)
		case df.Registration == "loop":
			add("DEFER_STACK")
		case df.Registration == "conditional":
			add("OPTION_GUARD")
		case df.CloseErrorTo != "":
			add("RESULT_SLOT_GUARD")
			fact("close error flows into named result %q", df.CloseErrorTo)
		case cell && pathDependent:
			add("CELL_CLOSURE_GUARD")
			fact("the deferred closure reads a variable at exit and acts on different resources on different paths")
		case strings.HasPrefix(df.Call, "invoke "):
			add("DYN_GUARD")
		default:
			add("OWNED_GUARD")
		}
	}
	if len(r.Defers) == 0 && explicit && !returned && !global && !outParam {
		add("EXPLICIT_CLOSE")
		fact("released by an explicit close, no defer")
	}
	if len(plan) == 0 {
		// nothing releases it and nothing owns it afterwards: should have been a leak anomaly
		d.Category, d.Plan, d.Rule = "C", "REVIEW", "C: no cleanup and no owner found"
		d.Review = []string{"no release and no ownership transfer was found for this resource"}
		d.Rust = rustShape["REVIEW"]
		return d
	}
	if unwind {
		add("CATCH_UNWIND")
		fact("a deferred cleanup runs on a recovered panic path")
	}
	d.Plan = strings.Join(plan, " + ")
	var shapes []string
	for _, p := range plan {
		shapes = append(shapes, rustShape[p])
	}
	d.Rust = strings.Join(shapes, " | ")
	return d
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
