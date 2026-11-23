package analysis

// Step 6: review reports. Every category-C decision becomes a report a developer can act on:
// where it is, the source lines involved, what the analysis found, and the realistic ways to
// resolve it, each with the Rust it leads to and how the program's observable behaviour changes.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ReviewReport is the step-6 output for one category-C resource.
type ReviewReport struct {
	Program  string         `json:"program"`
	Resource string         `json:"resource"`
	Var      string         `json:"var"`
	Question string         `json:"question"` // what the developer must decide
	Source   []string       `json:"source"`   // the lines involved, "line N: code"
	Findings []string       `json:"findings"` // what the analysis established
	Options  []ReviewOption `json:"options"`
	Meantime string         `json:"meantime"` // what RescFlow emits until someone decides
}

// ReviewOption is one way to resolve a review.
type ReviewOption struct {
	Choice string `json:"choice"`
	Rust   string `json:"rust"`
	Effect string `json:"effect"` // what changes in the program's observable behaviour
}

var reLine = regexp.MustCompile(`line (\d+)`)

// BuildReviews writes a report for every C decision. root resolves the records' file paths.
func BuildReviews(rep *Report, root string) {
	rep.Reviews = []*ReviewReport{}
	recs := map[string]*Record{}
	for _, r := range rep.Records {
		recs[r.ID] = r
	}
	for _, d := range rep.Decisions {
		if d.Category != "C" {
			continue
		}
		r := recs[d.Resource]
		rr := &ReviewReport{Program: r.Func[:strings.Index(r.Func, ".")], Resource: d.Resource, Var: r.Var,
			Findings: append([]string{}, d.Review...)}
		rr.Source = sourceLines(root, r)
		kinds := map[string]bool{}
		for _, x := range d.Review {
			switch {
			case strings.Contains(x, "after it is closed"):
				kinds["use-after-close"] = true
			case strings.Contains(x, "closes the file twice"):
				kinds["double-close"] = true
			case strings.Contains(x, "leaves the file open"):
				kinds["leak"] = true
			case strings.HasPrefix(x, "fact unavailable"):
				if m := regexp.MustCompile(`fact unavailable \((\S+),`).FindStringSubmatch(x); m != nil {
					kinds["fact:"+m[1]] = true
				}
			case strings.Contains(x, "goroutine"):
				kinds["fact:goroutine"] = true
			default:
				kinds["other"] = true
			}
		}
		addOptions(rr, r, kinds)
		rep.Reviews = append(rep.Reviews, rr)
	}
}

func addOptions(rr *ReviewReport, r *Record, kinds map[string]bool) {
	v := r.Var
	var questions []string
	// missing facts first: until they are supplied, the other questions may not even apply
	var facts []string
	for k := range kinds {
		if strings.HasPrefix(k, "fact:") {
			facts = append(facts, strings.TrimPrefix(k, "fact:"))
		}
	}
	sort.Strings(facts)
	if len(facts) > 0 {
		questions = append(questions, fmt.Sprintf("A fact needed to translate `%s` is unavailable (%s).", v, strings.Join(facts, ", ")))
		for _, k := range facts {
			u := explain(k)
			if u.HumanInput == "" {
				continue
			}
			rr.Findings = appendUnique(rr.Findings, "why: "+u.Why)
			rr.Options = append(rr.Options, ReviewOption{"Provide: " + u.HumanInput,
				"re-run RescFlow with that fact (a model row or a summary), then the normal rules apply",
				"none expected; the missing fact decides the plan"})
		}
	}
	if kinds["use-after-close"] {
		questions = append(questions, fmt.Sprintf("`%s` is used after it is closed. Which side owns the file?", v))
		rr.Options = append(rr.Options,
			ReviewOption{"The code that closes it owns it; the later use is a bug",
				"the closer takes `File` by value (`DeferClose` there); the later use is deleted",
				"the later use and its output disappear"},
			ReviewOption{"The later user owns it; the closer only borrows",
				"the closer takes `&File` and does not close; the owner keeps a `DeferClose` and closes after its last use",
				"the file closes later; the later use now succeeds"},
			ReviewOption{"Keep Go's behaviour exactly",
				"`rescflow_rt::OsFile` (shared, run-time-checked handle)",
				"none: the use after close still fails at run time, as in Go"})
	}
	if kinds["double-close"] {
		questions = append(questions, fmt.Sprintf("`%s` is closed twice. Which close is the real one?", v))
		rr.Options = append(rr.Options,
			ReviewOption{"The first (explicit) close is the real one",
				"close explicitly with `rescflow_rt::close(f)` (consumes the file); drop the deferred close",
				"the second close and the error it returns disappear; anything that reads that error (a named result, a printed status) changes"},
			ReviewOption{"The deferred close is the real one",
				"remove the explicit close; keep a `DeferClose` guard",
				"the file closes at function exit instead of earlier; the second close's error disappears"},
			ReviewOption{"Keep Go's behaviour exactly",
				"`rescflow_rt::OsFile`; the second `close()` returns \"file already closed\"",
				"none: output identical to Go"})
	}
	if kinds["leak"] {
		questions = append(questions, fmt.Sprintf("On at least one path `%s` is never closed. Is that intended?", v))
		rr.Options = append(rr.Options,
			ReviewOption{"No, it must be closed on every path",
				"an owned `DeferClose` guard created right after the acquisition, replacing the path-dependent cleanup",
				"the file is also closed on the path that missed it (one more close in the trace)"},
			ReviewOption{"Yes, ownership moves somewhere else on that path",
				"name the new owner; it then follows the transfer rules (return, global, out-parameter)",
				"none, once the owner is stated"})
	}
	if kinds["other"] && len(questions) == 0 {
		questions = append(questions, fmt.Sprintf("The analysis found a path-dependent conflict for `%s`; see the findings.", v))
	}
	rr.Question = strings.Join(questions, " ")
	rr.Meantime = "no Rust is generated for this function; the review report is the output"
}

// sourceLines quotes the acquisition and every line the decision mentions.
func sourceLines(root string, r *Record) []string {
	data, err := os.ReadFile(filepath.Join(root, r.File))
	if err != nil {
		return []string{fmt.Sprintf("(source unavailable: %v)", err)}
	}
	text := strings.Split(string(data), "\n")
	lines := map[int]bool{r.Line: true}
	for _, e := range r.Events {
		if e.Func == r.Func || strings.HasPrefix(e.Via, "call ") {
			lines[e.Line] = true
		}
	}
	for _, d := range r.Defers {
		lines[d.Line] = true
	}
	for _, a := range r.Anomalies {
		for _, m := range reLine.FindAllStringSubmatch(a, -1) {
			if n, err := strconv.Atoi(m[1]); err == nil {
				lines[n] = true
			}
		}
	}
	var ns []int
	for n := range lines {
		if n > 0 && n <= len(text) {
			ns = append(ns, n)
		}
	}
	sort.Ints(ns)
	var out []string
	for _, n := range ns {
		out = append(out, fmt.Sprintf("line %d: %s", n, strings.TrimSpace(text[n-1])))
	}
	return out
}

// Markdown renders the reports for people.
func (rep *Report) ReviewMarkdown() string {
	var b strings.Builder
	for _, rr := range rep.Reviews {
		fmt.Fprintf(&b, "## Review: %s, `%s` (%s)\n\n**Question:** %s\n\n**Source:**\n\n```go\n%s\n```\n\n**Findings:**\n\n",
			rr.Program, rr.Var, rr.Resource, rr.Question, strings.Join(rr.Source, "\n"))
		for _, f := range rr.Findings {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		fmt.Fprintf(&b, "\n| Option | Rust | What changes |\n|---|---|---|\n")
		for _, o := range rr.Options {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", o.Choice, o.Rust, o.Effect)
		}
		fmt.Fprintf(&b, "\n**Until decided:** %s.\n\n", rr.Meantime)
	}
	return b.String()
}
