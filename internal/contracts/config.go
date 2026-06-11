// Package contracts implements Stage 2 of the archmotif pipeline:
// loading user-declared contracts from `.archmotif.yaml`, marking the
// corresponding nodes in the typed graph as contracts, propagating
// contract markers through interface embedding, and discovering the
// one-hop set of code locations that produce contract-typed values.
//
// See docs/decisions/008-contract-config.md, 009-contract-attributes.md,
// and 010-producer-discovery.md for the design rationale.
package contracts

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigFileName is the filename archmotif looks for at the module
// root. Stable so users can put it under version control without
// tooling having to discover variants.
const ConfigFileName = ".archmotif.yaml"

// EntryKind is the discriminator for a contract config entry.
type EntryKind string

const (
	// EntryInterface declares an interface contract.
	EntryInterface EntryKind = "interface"
	// EntryType declares a non-interface (struct, alias, …) contract.
	EntryType EntryKind = "type"
)

// Entry is a single contract declaration read from `.archmotif.yaml`.
// One of Interface or Type is set; the other is empty.
type Entry struct {
	// Interface holds the qualified identifier (`pkg/path.Name`) when
	// the user wrote `interface: pkg/path.Name`.
	Interface string `yaml:"interface,omitempty"`
	// Type holds the qualified identifier when the user wrote
	// `type: pkg/path.Name`.
	Type string `yaml:"type,omitempty"`
}

// Kind reports whether the entry is an interface or a type
// declaration. Returns "" for malformed entries.
func (e Entry) Kind() EntryKind {
	switch {
	case e.Interface != "" && e.Type == "":
		return EntryInterface
	case e.Type != "" && e.Interface == "":
		return EntryType
	default:
		return ""
	}
}

// Identifier returns the qualified `pkg/path.Name` string carried by
// the entry, regardless of kind.
func (e Entry) Identifier() string {
	if e.Interface != "" {
		return e.Interface
	}
	return e.Type
}

// Config is the deserialised `.archmotif.yaml` payload.
type Config struct {
	Contracts []Entry `yaml:"contracts"`
	Graph     Graph   `yaml:"graph,omitempty"`
	// Roles holds architecture role metadata declarations. See
	// internal/roles for selector semantics and resolution.
	Roles Roles `yaml:"roles,omitempty"`
	// Coupling holds layer-aware coupling-report configuration:
	// forbidden-edge declarations and the per-pair evidence cap. See
	// internal/coupling and ADR-030.
	Coupling Coupling `yaml:"coupling,omitempty"`
}

// Coupling configures the layer-aware coupling report (ADR-030).
type Coupling struct {
	// Forbidden enumerates directed (from, to) role pairs whose edges
	// are architectural violations. Both From and To must be one of
	// the allowed package roles.
	Forbidden []ForbiddenRule `yaml:"forbidden,omitempty"`
	// EvidenceCap caps the per-pair evidence list length in the
	// rendered report. Default 5; values < 0 are rejected at load.
	EvidenceCap int `yaml:"evidence_cap,omitempty"`
}

// ForbiddenRule is one entry in coupling.forbidden. Reason is the
// human-readable rationale rendered alongside the violation list;
// empty reasons get a canonical default at render time.
type ForbiddenRule struct {
	From   string `yaml:"from"`
	To     string `yaml:"to"`
	Reason string `yaml:"reason,omitempty"`
}

// Roles is the top-level container for role declarations in
// `.archmotif.yaml`. Selectors are matched in two passes: package-scoped
// rules first, then type/symbol-scoped rules (which override).
type Roles struct {
	Packages []RoleSelector `yaml:"packages,omitempty"`
	Types    []RoleSelector `yaml:"types,omitempty"`
}

// RoleSelector matches one or more graph nodes and tags them with a
// role. Exactly one of Pattern or Qualified must be set. Pattern is a
// glob (`*`, `**`) on the package import path, file path, or qualified
// node name; Qualified is an exact `pkg/path.Name` match.
type RoleSelector struct {
	Pattern   string `yaml:"pattern,omitempty"`
	Qualified string `yaml:"qualified,omitempty"`
	Role      string `yaml:"role"`
}

// Graph contains graph export/view options from `.archmotif.yaml`.
type Graph struct {
	Exclude Exclude `yaml:"exclude,omitempty"`
}

// Exclude declares nodes that should be omitted from generated graphs.
// This is intended for architecture-noise sinks such as `fmt.Errorf`,
// not for domain-specific filtering after export.
type Exclude struct {
	// Dirs skips source directories before package loading. Entries without
	// slashes match any directory segment, for example `tests`.
	Dirs []string `yaml:"dirs,omitempty"`
	// QNames drops nodes whose qname exactly matches one of these values.
	QNames []string `yaml:"qnames,omitempty"`
	// QNamePrefixes drops nodes whose qname starts with one of these
	// prefixes. Useful for broad utility package views.
	QNamePrefixes []string `yaml:"qname_prefixes,omitempty"`
	// Packages drops nodes that belong to these package paths. Method
	// qnames with receivers such as `(*pkg.Type).Method` are handled.
	Packages []string `yaml:"packages,omitempty"`
	// Kinds drops nodes by graph kind, for example `branch` or `loop`.
	Kinds []string `yaml:"kinds,omitempty"`
}

// LoadConfig reads `.archmotif.yaml` from dir (the module root). When
// the file is absent it returns an empty Config and a nil error —
// missing config is a valid state ("no contracts declared yet").
//
// Malformed YAML or entries that have neither `interface:` nor `type:`
// (or have both) return a wrapped error so the CLI can surface them.
func LoadConfig(dir string) (Config, error) {
	path := filepath.Join(dir, ConfigFileName)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("contracts: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	cfg, err := readConfig(f)
	if err != nil {
		return Config{}, fmt.Errorf("contracts: parse %s: %w", path, err)
	}
	return cfg, nil
}

// readConfig parses YAML from r. Exposed for tests.
func readConfig(r io.Reader) (Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, err
	}
	for i, e := range cfg.Contracts {
		if e.Kind() == "" {
			return Config{}, fmt.Errorf("entry %d: must specify exactly one of `interface:` or `type:`", i)
		}
		if !strings.Contains(e.Identifier(), ".") {
			return Config{}, fmt.Errorf("entry %d: identifier %q must be `<import-path>.<TypeName>`", i, e.Identifier())
		}
	}
	if err := validateExclude(cfg.Graph.Exclude); err != nil {
		return Config{}, err
	}
	if err := validateRoles(cfg.Roles); err != nil {
		return Config{}, err
	}
	if err := validateCoupling(cfg.Coupling); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// allowedPackageRoles and allowedTypeRoles enumerate the role strings
// accepted in `.archmotif.yaml`. Kept here (not in internal/graph) so
// the config layer can reject unknown roles at load time without
// pulling internal/graph into the YAML schema. Mirrored against
// graph.AllPackageRoles / graph.AllTypeRoles by tests.
var allowedPackageRoles = map[string]struct{}{
	"domain":           {},
	"application":      {},
	"inbound_adapter":  {},
	"outbound_adapter": {},
	"infrastructure":   {},
	"shared":           {},
}

var allowedTypeRoles = map[string]struct{}{
	"domain_entity":   {},
	"value_object":    {},
	"port":            {},
	"adapter_dto":     {},
	"config_contract": {},
	"external_noise":  {},
}

func validateRoles(r Roles) error {
	for i, sel := range r.Packages {
		if err := validateSelector("roles.packages", i, sel, allowedPackageRoles); err != nil {
			return err
		}
	}
	for i, sel := range r.Types {
		if err := validateSelector("roles.types", i, sel, allowedTypeRoles); err != nil {
			return err
		}
	}
	return nil
}

func validateSelector(field string, idx int, sel RoleSelector, allowed map[string]struct{}) error {
	hasPattern := strings.TrimSpace(sel.Pattern) != ""
	hasQualified := strings.TrimSpace(sel.Qualified) != ""
	switch {
	case hasPattern && hasQualified:
		return fmt.Errorf("%s[%d]: must specify exactly one of `pattern:` or `qualified:`", field, idx)
	case !hasPattern && !hasQualified:
		return fmt.Errorf("%s[%d]: must specify one of `pattern:` or `qualified:`", field, idx)
	}
	if strings.TrimSpace(sel.Role) == "" {
		return fmt.Errorf("%s[%d]: `role:` must not be empty", field, idx)
	}
	if _, ok := allowed[sel.Role]; !ok {
		return fmt.Errorf("%s[%d]: role %q is not an allowed value", field, idx, sel.Role)
	}
	return nil
}

// validateCoupling enforces the coupling.forbidden grammar: from / to
// are non-empty and resolvable to allowed package roles, and
// evidence_cap is non-negative. ADR-030 §5 records the schema.
func validateCoupling(c Coupling) error {
	if c.EvidenceCap < 0 {
		return fmt.Errorf("coupling.evidence_cap: must be >= 0, got %d", c.EvidenceCap)
	}
	for i, rule := range c.Forbidden {
		from := strings.TrimSpace(rule.From)
		to := strings.TrimSpace(rule.To)
		if from == "" {
			return fmt.Errorf("coupling.forbidden[%d]: `from:` must not be empty", i)
		}
		if to == "" {
			return fmt.Errorf("coupling.forbidden[%d]: `to:` must not be empty", i)
		}
		if _, ok := allowedPackageRoles[from]; !ok {
			return fmt.Errorf("coupling.forbidden[%d]: from %q is not an allowed package role", i, from)
		}
		if _, ok := allowedPackageRoles[to]; !ok {
			return fmt.Errorf("coupling.forbidden[%d]: to %q is not an allowed package role", i, to)
		}
	}
	return nil
}

func validateExclude(ex Exclude) error {
	for field, values := range map[string][]string{
		"graph.exclude.dirs":           ex.Dirs,
		"graph.exclude.qnames":         ex.QNames,
		"graph.exclude.qname_prefixes": ex.QNamePrefixes,
		"graph.exclude.packages":       ex.Packages,
		"graph.exclude.kinds":          ex.Kinds,
	} {
		for i, v := range values {
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("%s[%d]: value must not be empty", field, i)
			}
		}
	}
	return nil
}

// openIfExists opens the file at path. Returns (nil, nil) when the
// file is absent so callers can treat absence as a non-error.
func openIfExists(path string) (*os.File, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("contracts: open %s: %w", path, err)
	}
	return f, nil
}

// SplitIdentifier separates a `pkg/path.Name` string into the import
// path and the type name. The split point is the *last* dot (import
// paths frequently contain dots — `example.com/foo`). Returns the
// empty strings when the identifier is malformed.
func SplitIdentifier(id string) (pkgPath, typeName string) {
	i := strings.LastIndex(id, ".")
	if i < 0 {
		return "", ""
	}
	pkgPath = id[:i]
	typeName = id[i+1:]
	if pkgPath == "" || typeName == "" {
		return "", ""
	}
	return pkgPath, typeName
}
