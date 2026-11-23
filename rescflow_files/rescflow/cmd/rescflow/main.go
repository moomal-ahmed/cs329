// Command rescflow runs step 1 of RescFlow: load Go packages, build SSA with go/ssa, and run
// the memory-aware resource analysis. Output: one record per resource acquisition site,
// per-function exits (which deferred calls run there and what they act on), function
// summaries, and a detailed explanation of everything that could not be analyzed.
//
//	rescflow [-explain] [-format json|text] [-C dir] packages...
//
// Example: rescflow -format text -explain ./benchmarks/14_late_closure
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"example.com/rescflow/internal/analysis"
)

func main() {
	explainSSA := flag.Bool("explain", false, "include the SSA of each function annotated with the analysis state")
	format := flag.String("format", "json", "json or text")
	dir := flag.String("C", "", "directory to resolve package patterns in (default: current directory)")
	flag.Parse()
	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	rep := analysis.Run(*dir, patterns, *explainSSA)
	if *format == "text" {
		printText(rep)
	} else {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if len(rep.Packages) == 0 {
		os.Exit(2)
	}
	for _, p := range rep.Packages {
		if !p.Analyzed {
			os.Exit(2)
		}
	}
}

func printText(rep *analysis.Report) {
	w := os.Stdout
	fmt.Fprintf(w, "RescFlow analysis (%s) of %s\n", rep.Go, strings.Join(rep.Patterns, " "))
	for _, p := range rep.Packages {
		status := "analyzed"
		if !p.Analyzed {
			status = "NOT ANALYZED"
		}
		fmt.Fprintf(w, "  package %s: %s\n", p.Path, status)
		for _, u := range p.LoadIssue {
			printUnresolved(w, u, "    ")
		}
	}
	for _, f := range rep.Functions {
		interesting := len(f.SSA) > 0 && hasNotes(f.SSA)
		for _, e := range f.Exits {
			interesting = interesting || len(e.Pops) > 0 || len(e.Open) > 0
		}
		if !interesting {
			continue
		}
		fmt.Fprintf(w, "\n== func %s  (%s:%d)\n", f.Name, f.File, f.Line)
		for _, l := range f.SSA {
			fmt.Fprintln(w, l)
		}
		for _, e := range f.Exits {
			fmt.Fprintf(w, "  exit line %d [%s]:", e.Line, e.Kind)
			if len(e.Pops) == 0 {
				fmt.Fprint(w, " no deferred calls")
			}
			for _, p := range e.Pops {
				fmt.Fprintf(w, " defer@%d->%v", p.DeferLine, p.Acts)
			}
			if len(e.Open) > 0 {
				fmt.Fprintf(w, "  STILL OPEN: %v", e.Open)
			}
			fmt.Fprintln(w)
		}
	}
	fmt.Fprintf(w, "\n== resource records (%d)\n", len(rep.Records))
	for _, r := range rep.Records {
		fmt.Fprintf(w, "\n%s  var %s in %s  (loop=%v goroutine=%v)\n", r.ID, r.Var, r.Func, r.InLoop, r.InGoroutine)
		for _, e := range r.Events {
			fmt.Fprintf(w, "  %-8s line %-3d %-9s via %s\n", e.Kind, e.Line, "["+e.State+"]", e.Via)
		}
		for _, d := range r.Defers {
			fmt.Fprintf(w, "  defer@%d (%s) %s\n", d.Line, d.Registration, d.Call)
			for _, c := range d.Captures {
				fmt.Fprintf(w, "      captures %s\n", c)
			}
			for exit, acts := range d.Resolves {
				fmt.Fprintf(w, "      at %s acts on %v\n", exit, acts)
			}
			if d.CloseErrorTo != "" {
				fmt.Fprintf(w, "      close error -> named result %q\n", d.CloseErrorTo)
			}
		}
		for _, e := range r.Escapes {
			fmt.Fprintf(w, "  escape   %s\n", e)
		}
		for _, a := range r.Anomalies {
			fmt.Fprintf(w, "  ANOMALY  %s\n", a)
		}
		for _, n := range r.Notes {
			fmt.Fprintf(w, "  note     %s\n", n)
		}
	}
	if len(rep.Unresolved) > 0 {
		fmt.Fprintf(w, "\n== could not be resolved (%d)\n", len(rep.Unresolved))
		for _, u := range rep.Unresolved {
			printUnresolved(w, u, "  ")
		}
	}
}

func hasNotes(lines []string) bool {
	for _, l := range lines {
		if strings.Contains(l, "⟹") {
			return true
		}
	}
	return false
}

func printUnresolved(w *os.File, u analysis.Unresolved, ind string) {
	fmt.Fprintf(w, "%s[%s] %s:%d %s  resources=%v\n", ind, u.Kind, u.File, u.Line, u.Func, u.Resources)
	fmt.Fprintf(w, "%s  construct:    %s\n", ind, u.Construct)
	fmt.Fprintf(w, "%s  missing fact: %s\n", ind, u.MissingFact)
	fmt.Fprintf(w, "%s  why:          %s\n", ind, u.Why)
	fmt.Fprintf(w, "%s  consequence:  %s\n", ind, u.Consequence)
	fmt.Fprintf(w, "%s  human input:  %s\n", ind, u.HumanInput)
	if u.Detail != "" {
		fmt.Fprintf(w, "%s  detail:       %s\n", ind, u.Detail)
	}
}
