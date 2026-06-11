package contracts

import (
	"os"
	"path/filepath"
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/parser"
)

// BuildOptions controls Build. Mirrors parser.Options where applicable.
type BuildOptions struct {
	// Dir is the working directory for `go/packages`. Patterns are
	// resolved relative to this directory. The `.archmotif.yaml` file
	// is looked up in the loaded module's root (ModuleRoot in the
	// parser result), falling back to Dir.
	Dir string
	// Patterns are passed to `go/packages.Load`. Defaults to "./...".
	Patterns []string
	// Tests, when true, also loads `_test.go` files.
	Tests bool
	// ConfigPath, when non-empty, overrides the default lookup of
	// `.archmotif.yaml` at the module root. Useful for tests.
	ConfigPath string
	// ExcludeDirs skips source directories before package loading. It is
	// combined with graph.exclude.dirs from .archmotif.yaml.
	ExcludeDirs []string
}

// Materialisation bundles a contract with its discovered producers.
type Materialisation struct {
	Contract  mgraph.Node
	Source    string // "config" or "embedded"
	Producers []Producer
}

// Result is the output of Build. Carries everything the CLI needs to
// render output.
type Result struct {
	Graph            *mgraph.Graph
	ModuleRoot       string
	LoadErrors       []string
	Config           Config
	ConfigPath       string
	Resolved         []Resolved
	Unresolved       []Unresolved
	Materialisations []Materialisation
	// KindMismatches collects the human-readable warnings produced by
	// Resolve when an entry's declared kind disagrees with the actual
	// underlying kind (e.g. `type:` for an interface).
	KindMismatches []string
}

// Build is the Stage 2 entry point: it loads the typed graph (Stage 1),
// reads `.archmotif.yaml`, resolves contracts, marks the graph, and
// computes per-contract producers.
//
// Errors during package loading do not abort: they're surfaced via
// Result.LoadErrors mirroring parser.Build's behaviour.
func Build(opts BuildOptions) (*Result, error) {
	if opts.Dir == "" {
		opts.Dir = "."
	}
	scopeCfg, err := loadBuildScopeConfig(opts)
	if err != nil {
		return nil, err
	}
	parserOpts := parser.Options{
		Dir:         opts.Dir,
		Patterns:    opts.Patterns,
		Tests:       opts.Tests,
		ExcludeDirs: append(append([]string{}, opts.ExcludeDirs...), scopeCfg.Graph.Exclude.Dirs...),
	}
	pres, err := parser.Build(parserOpts)
	if err != nil {
		return nil, err
	}

	configPath := opts.ConfigPath
	if configPath == "" {
		configPath = filepath.Join(pres.ModuleRoot, ConfigFileName)
	}
	cfg, err := loadConfigAt(configPath)
	if err != nil {
		return nil, err
	}

	resolution := Resolve(cfg, pres.Packages, pres.Graph)
	mismatches := make([]string, 0)
	for _, r := range resolution.Resolved {
		if r.UnderError != "" {
			mismatches = append(mismatches, r.UnderError)
		}
	}

	_ = Mark(pres.Graph, resolution.Resolved)
	pres.Graph = ApplyExcludes(pres.Graph, cfg.Graph.Exclude)

	// Materialisations: every marked contract gets its producer list.
	contracts := AllContracts(pres.Graph)
	mats := make([]Materialisation, 0, len(contracts))
	for _, c := range contracts {
		mats = append(mats, Materialisation{
			Contract:  c,
			Source:    c.ContractSource(),
			Producers: Producers(pres.Graph, c.ID),
		})
	}
	sort.SliceStable(mats, func(i, j int) bool {
		return mats[i].Contract.ID < mats[j].Contract.ID
	})

	return &Result{
		Graph:            pres.Graph,
		ModuleRoot:       pres.ModuleRoot,
		LoadErrors:       pres.LoadErrors,
		Config:           cfg,
		ConfigPath:       configPath,
		Resolved:         resolution.Resolved,
		Unresolved:       resolution.Unresolved,
		Materialisations: mats,
		KindMismatches:   mismatches,
	}, nil
}

func loadBuildScopeConfig(opts BuildOptions) (Config, error) {
	if opts.ConfigPath != "" {
		return loadConfigAt(opts.ConfigPath)
	}
	dir := opts.Dir
	if root, ok := findGoModRoot(dir); ok {
		dir = root
	}
	return LoadConfig(dir)
}

func findGoModRoot(dir string) (string, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return abs, true
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", false
		}
		abs = parent
	}
}

// loadConfigAt reads cfg from a specific file path. Honours the
// "missing file → empty config" rule from LoadConfig.
func loadConfigAt(path string) (Config, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if base == ConfigFileName {
		return LoadConfig(dir)
	}
	// Custom path — open directly.
	f, err := openIfExists(path)
	if err != nil {
		return Config{}, err
	}
	if f == nil {
		return Config{}, nil
	}
	defer func() { _ = f.Close() }()
	return readConfig(f)
}
