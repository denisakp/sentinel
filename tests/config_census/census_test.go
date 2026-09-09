package config_census

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
)

// recordDir is where the per-area record files live.
const recordDir = "reachability"

// configPkgDir is the package this census polices.
const configPkgDir = "internal/config"

// deref unwraps pointers, slices and maps to reach the underlying type. Maps
// matter here: `databases` is a map of job structs, and its values are as
// settable as any named field.
func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	return t
}

// walk performs the dynamic pass: it reflects from a root type and records every
// addressable path, separating settable values from containers.
//
// It records PATHS, not field definitions. The same nested type is reached
// through more than one parent: compression.enabled exists under `defaults` and
// under every entry of `databases`, and each is independently settable and so
// independently able to be ignored. Deduplicating by field would hide exactly the
// "wired on one axis, forgotten on the other" pattern the batch plan names as a
// root cause.
func walk(t reflect.Type, prefix string, seen map[reflect.Type]bool, leaves, containers *[]string, types map[string]bool) {
	t = deref(t)
	if t.Kind() != reflect.Struct || seen[t] {
		return
	}
	seen[t] = true
	defer delete(seen, t)
	if t.Name() != "" {
		types[t.Name()] = true
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if deref(f.Type).Kind() == reflect.Struct {
			*containers = append(*containers, path)
			walk(f.Type, path, seen, leaves, containers, types)
		} else {
			*leaves = append(*leaves, path)
		}
	}
}

// reachable runs the dynamic pass from the root configuration type.
func reachable() (leaves, containers []string, types map[string]bool) {
	types = map[string]bool{}
	walk(reflect.TypeOf(config.Configuration{}), "", map[reflect.Type]bool{}, &leaves, &containers, types)
	sort.Strings(leaves)
	sort.Strings(containers)
	return
}

// declaredConfigTypes runs the static pass: it parses the configuration package
// and returns every type that declares schema-tagged fields.
//
// This exists because reflection cannot see a type that nothing references. A
// reflection-only census reports a clean result while orphaned types sit
// unreachable, which is the exact shape of issue #144, so the census would be
// blind to the very thing it is supposed to notice.
func declaredConfigTypes(repoRoot string) (map[string]int, error) {
	dir := filepath.Join(repoRoot, configPkgDir)
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				ts, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					return true
				}
				count := 0
				for _, fld := range st.Fields.List {
					if fld.Tag == nil {
						continue
					}
					if strings.Contains(fld.Tag.Value, `yaml:"`) &&
						!strings.Contains(fld.Tag.Value, `yaml:"-"`) {
						count++
					}
				}
				if count > 0 {
					out[ts.Name.Name] = count
				}
				return true
			})
		}
	}
	return out, nil
}

func loadAll(t *testing.T) (root string, leaves []string, rec *Record) {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("locating module root: %v", err)
	}
	leaves, _, _ = reachable()
	rec, err = LoadRecord(filepath.Join(root, "tests", "config_census", recordDir))
	if err != nil {
		t.Fatalf("loading reachability record: %v", err)
	}
	return root, leaves, rec
}

// TestEveryReachablePathIsRecorded is the forward half of the reconciliation: a
// settable path the schema exposes but the record does not mention fails, naming
// it. Adding a configuration key therefore fails this test until the key is
// accounted for, which is the point.
func TestEveryReachablePathIsRecorded(t *testing.T) {
	_, leaves, rec := loadAll(t)
	var missing []string
	for _, p := range leaves {
		if _, ok := rec.Entries[p]; !ok {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d settable configuration path(s) are not accounted for in the reachability "+
			"record.\nEach must be classified connected, inert, partial or unverified before it can "+
			"be trusted.\nSee tests/config_census/reachability/README.md.\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// TestNoStaleRecordEntries is the backward half: an entry naming a path the
// schema no longer exposes fails, naming it. Without this the record decays into
// a list nobody has revisited, which is how a control of this kind usually dies.
func TestNoStaleRecordEntries(t *testing.T) {
	_, leaves, rec := loadAll(t)
	live := map[string]bool{}
	for _, p := range leaves {
		live[p] = true
	}
	var stale []string
	for p := range rec.Entries {
		if !live[p] {
			stale = append(stale, fmt.Sprintf("%s (recorded in %s)", p, rec.Sources[p]))
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("%d record entry/entries name a path the schema no longer exposes. Remove them:\n  %s",
			len(stale), strings.Join(stale, "\n  "))
	}
}

// TestEntriesAreInternallyValid enforces the per-classification rules: an inert
// key must cite its issue, a connected key must carry an anchor, and so on.
func TestEntriesAreInternallyValid(t *testing.T) {
	_, _, rec := loadAll(t)
	var problems []string
	for _, p := range sortedKeys(rec.Entries) {
		problems = append(problems, rec.Entries[p].Validate()...)
	}
	if len(problems) > 0 {
		t.Errorf("%d record entry problem(s):\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
}

// TestEvidenceAnchorsResolve is what makes a connected claim falsifiable.
// Deleting the code that reads a key breaks its anchor and fails here, rather
// than the key quietly becoming inert while the record still says otherwise.
func TestEvidenceAnchorsResolve(t *testing.T) {
	root, _, rec := loadAll(t)
	var broken []string
	for _, p := range sortedKeys(rec.Entries) {
		e := rec.Entries[p]
		if e.Classification != Connected && e.Classification != Partial {
			continue
		}
		if e.Evidence == nil {
			continue // reported by TestEntriesAreInternallyValid
		}
		if err := VerifyAnchor(root, e.Evidence); err != nil {
			broken = append(broken, fmt.Sprintf("%s: %v", p, err))
		}
	}
	if len(broken) > 0 {
		t.Errorf("%d evidence anchor(s) no longer resolve:\n  %s",
			len(broken), strings.Join(broken, "\n  "))
	}
}

// TestNoUnrecordedOrphanedConfigTypes reports types that declare schema keys but
// are unreachable from the root.
//
// This is deliberately a separate finding from an inert key, and the distinction
// is operational rather than pedantic. An inert key can be set and is ignored. An
// orphaned type's keys cannot be set at all, so a config file naming them is
// rejected or silently dropped rather than merely ineffective. Different symptom,
// different fix.
func TestNoUnrecordedOrphanedConfigTypes(t *testing.T) {
	root, _, _ := loadAll(t)
	_, _, reachableTypes := reachable()
	declared, err := declaredConfigTypes(root)
	if err != nil {
		t.Fatalf("static pass over %s: %v", configPkgDir, err)
	}

	// Types known to be orphaned, with the issue that tracks them. An orphan not
	// on this list fails; an entry here that is no longer orphaned also fails.
	//
	// This census reproduced #143 independently: RestoreConfiguration is a
	// parallel "extends Configuration" struct that nothing constructs, and it is
	// the only route to RestoreDefaults. Every key under either is unsettable.
	known := map[string]string{
		"AzureConfig":          "#144",
		"AzureAuthConfig":      "#144",
		"RestoreConfiguration": "#143",
		"RestoreDefaults":      "#143",
	}

	// Types that describe a DIFFERENT file, and so are legitimately unreachable
	// from the main configuration root. They are not defects and must not be
	// recorded as orphans, or the finding stops meaning anything.
	//
	// Kept separate from `known` deliberately. Collapsing the two lists would
	// force a choice between hiding a real defect and accusing correct code.
	separateFileSchemas := map[string]string{
		"mongoSecrets": "the MongoDB secrets file (spec 057), parsed by readMongoSecretsFile " +
			"from its own path; never part of the main config tree",
	}

	var unexpected, resolved []string
	for name, fields := range declared {
		if reachableTypes[name] {
			if _, listed := known[name]; listed {
				resolved = append(resolved, name)
			}
			continue
		}
		if _, separate := separateFileSchemas[name]; separate {
			continue
		}
		if _, listed := known[name]; !listed {
			unexpected = append(unexpected, fmt.Sprintf(
				"%s declares %d schema key(s) but is unreachable from the root configuration type, "+
					"so no operator can set them", name, fields))
		}
	}
	sort.Strings(unexpected)
	sort.Strings(resolved)
	if len(unexpected) > 0 {
		t.Errorf("%d unrecorded orphaned configuration type(s):\n  %s",
			len(unexpected), strings.Join(unexpected, "\n  "))
	}
	if len(resolved) > 0 {
		t.Errorf("%d type(s) listed as orphaned are now reachable; remove them from the known list "+
			"in this test:\n  %s", len(resolved), strings.Join(resolved, "\n  "))
	}
}

// TestCensusSummary reports the shape of the record. It does not fail on
// unverified paths: they are honest admissions rather than defects, and the
// budget below is what stops them growing.
func TestCensusSummary(t *testing.T) {
	_, leaves, rec := loadAll(t)
	c := rec.Counts()
	t.Logf("settable paths reachable from root: %d", len(leaves))
	t.Logf("recorded: %d (connected %d, partial %d, inert %d, unverified %d)",
		len(rec.Entries), c[Connected], c[Partial], c[Inert], c[Unverified])

	// The unverified budget, set to the count this census shipped with. It exists
	// so an admission cannot quietly become the default: lowering this number is
	// the ratchet that finishes the census, and it must never be raised without
	// saying why in the commit. Adding a new key wired to nothing therefore fails
	// here rather than passing as one more admission.
	const unverifiedBudget = 151
	if c[Unverified] > unverifiedBudget {
		t.Errorf("unverified paths (%d) exceed the budget of %d; determine them rather than "+
			"raising the budget", c[Unverified], unverifiedBudget)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
