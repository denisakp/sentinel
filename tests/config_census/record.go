package config_census

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Classification says what is known about a configuration path.
type Classification string

const (
	// Connected: code reads the field and acts on it. Requires an evidence anchor.
	Connected Classification = "connected"
	// Inert: parsed and validated, read by nothing that acts. Requires an issue.
	Inert Classification = "inert"
	// Partial: honoured in some contexts and not others. Requires anchor, scope and issue.
	Partial Classification = "partial"
	// Unverified: the census has not yet determined this path.
	//
	// This exists because the alternative is worse. Determining 228 paths in one
	// pass, several of whose Go field names recur across config types, cannot be
	// done reliably by name matching alone. Recording a guess as Connected would
	// put a false claim in the record, and recording it as Inert would cite an
	// issue that may not describe it. Both are lies, and a lie here is worse than
	// an admission: this record's whole purpose is that it can be trusted.
	//
	// Unverified paths are counted and listed, so the outstanding work is visible
	// rather than implied. See ErrUnverifiedBudget.
	Unverified Classification = "unverified"
)

// Anchor is static evidence that code reads a field: a file, and an exact
// expression that must appear in it.
//
// Not a line number: those move on every unrelated edit, and a check that fails
// constantly for no reason is disabled within a week. Not a bare field name
// either: names such as Enabled, Mode and Path recur across many types and match
// anywhere, proving nothing.
type Anchor struct {
	File       string `yaml:"file"`
	Expression string `yaml:"expression"`
}

// Entry accounts for exactly one settable configuration path.
type Entry struct {
	Path           string         `yaml:"path"`
	Classification Classification `yaml:"classification"`
	Evidence       *Anchor        `yaml:"evidence,omitempty"`
	Scope          string         `yaml:"scope,omitempty"`
	Issue          string         `yaml:"issue,omitempty"`
	// BehaviouralProof is "verified" or "pending".
	BehaviouralProof string `yaml:"behavioural_proof,omitempty"`
	Needs            string `yaml:"needs,omitempty"`
	Note             string `yaml:"note,omitempty"`
}

type areaFile struct {
	Area    string  `yaml:"area"`
	Entries []Entry `yaml:"entries"`
}

// Record is the merged reachability record, keyed by path.
type Record struct {
	Entries map[string]Entry
	Sources map[string]string // path -> file it was declared in
}

// LoadRecord reads every area file under dir and merges them into one record.
// The split into area files is an authoring convenience so the areas can be
// populated in parallel; an entry filed under the wrong area is still found.
func LoadRecord(dir string) (*Record, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	rec := &Record{Entries: map[string]Entry{}, Sources: map[string]string{}}
	for _, m := range matches {
		raw, err := os.ReadFile(m)
		if err != nil {
			return nil, err
		}
		var af areaFile
		if err := yaml.Unmarshal(raw, &af); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(m), err)
		}
		for _, e := range af.Entries {
			if prev, dup := rec.Sources[e.Path]; dup {
				return nil, fmt.Errorf(
					"path %q recorded twice, in %s and %s; each path must appear exactly once",
					e.Path, prev, filepath.Base(m))
			}
			rec.Entries[e.Path] = e
			rec.Sources[e.Path] = filepath.Base(m)
		}
	}
	return rec, nil
}

// Validate checks an entry against the per-classification rules in
// contracts/reachability-record.md. It returns every problem found rather than
// the first, so a contributor fixes them in one pass.
func (e Entry) Validate() []string {
	var problems []string
	add := func(f string, a ...any) { problems = append(problems, fmt.Sprintf(f, a...)) }

	if e.Path == "" {
		add("entry has no path")
	}
	switch e.Classification {
	case Connected, Partial:
		if e.Evidence == nil || e.Evidence.File == "" || e.Evidence.Expression == "" {
			add("%s is %q and must carry an evidence anchor with a file and an expression",
				e.Path, e.Classification)
		}
		if e.BehaviouralProof != "verified" && e.BehaviouralProof != "pending" {
			add("%s is %q and must set behavioural_proof to \"verified\" or \"pending\", got %q",
				e.Path, e.Classification, e.BehaviouralProof)
		}
		if e.BehaviouralProof == "pending" && strings.TrimSpace(e.Needs) == "" {
			add("%s has behavioural_proof \"pending\" and must state in needs what would be "+
				"required to observe it, so the outstanding coverage is a list and not an implication",
				e.Path)
		}
		if e.Classification == Partial {
			if strings.TrimSpace(e.Scope) == "" {
				add("%s is \"partial\" and must state its scope: where the key is honoured and "+
					"where it is not", e.Path)
			}
			if strings.TrimSpace(e.Issue) == "" {
				add("%s is \"partial\" and must cite the issue tracking the gap", e.Path)
			}
		}
	case Inert:
		if strings.TrimSpace(e.Issue) == "" {
			add("%s is \"inert\" and must cite the issue tracking it; an inert key with no issue "+
				"is a defect nobody is going to fix", e.Path)
		}
	case Unverified:
		if strings.TrimSpace(e.Note) == "" {
			add("%s is \"unverified\" and must say in note what blocks the determination", e.Path)
		}
	case "":
		add("%s has no classification", e.Path)
	default:
		add("%s has unknown classification %q", e.Path, e.Classification)
	}
	return problems
}

// Counts tallies the record by classification, for reporting.
func (r *Record) Counts() map[Classification]int {
	out := map[Classification]int{}
	for _, e := range r.Entries {
		out[e.Classification]++
	}
	return out
}

// PathsWith returns the recorded paths of a given classification, sorted.
func (r *Record) PathsWith(c Classification) []string {
	var out []string
	for p, e := range r.Entries {
		if e.Classification == c {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
