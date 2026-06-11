package contracts

import (
	"path/filepath"
	"sort"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// fixtureDir resolves the absolute path to the userstore fixture.
func fixtureDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.Abs("testdata/userstore")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestBuild_FixtureMatchesIssueSpec verifies the issue-#3 fixture flow
// end-to-end: declared contracts are resolved, the contract nodes carry
// IsContract markers, and producers include both implementations and
// the constructor that returns the contract type.
func TestBuild_FixtureMatchesIssueSpec(t *testing.T) {
	dir := fixtureDir(t)
	res, err := Build(BuildOptions{Dir: dir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(res.Resolved) != 3 {
		t.Fatalf("expected 3 resolved contracts, got %d (unresolved=%d)", len(res.Resolved), len(res.Unresolved))
	}
	if len(res.Unresolved) != 0 {
		t.Fatalf("expected 0 unresolved, got %d", len(res.Unresolved))
	}
	if len(res.KindMismatches) != 0 {
		t.Fatalf("expected 0 kind mismatches, got %v", res.KindMismatches)
	}

	// Find UserStore materialisation by QName.
	var us *Materialisation
	for i := range res.Materialisations {
		if res.Materialisations[i].Contract.QName == "userstore/store.UserStore" {
			us = &res.Materialisations[i]
			break
		}
	}
	if us == nil {
		t.Fatal("UserStore materialisation missing")
	}
	if !us.Contract.IsContract() {
		t.Fatal("UserStore node is not marked as contract")
	}
	if us.Contract.ContractKind() != "interface" {
		t.Fatalf("UserStore contractKind = %q, want interface", us.Contract.ContractKind())
	}
	if us.Contract.ContractSource() != "config" {
		t.Fatalf("UserStore contractSource = %q, want config", us.Contract.ContractSource())
	}

	// Producer set: MemStore + SQLStore (Implements), NewMemStore +
	// app.Bootstrap (Returns).
	want := map[string]ProducerKind{
		"userstore/store.MemStore":    ProducerImplements,
		"userstore/store.SQLStore":    ProducerImplements,
		"userstore/store.NewMemStore": ProducerReturns,
		"userstore/app.Bootstrap":     ProducerReturns,
	}
	got := make(map[string]ProducerKind, len(us.Producers))
	for _, p := range us.Producers {
		got[p.Node.QName] = p.Kind
	}
	if len(got) != len(want) {
		t.Fatalf("producer set mismatch:\n got=%v\n want=%v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("producer %q kind = %q, want %q", k, got[k], v)
		}
	}
}

// TestBuild_TypeContract verifies that a non-interface (struct)
// contract picks up its constructor as a Returns producer.
func TestBuild_TypeContract(t *testing.T) {
	dir := fixtureDir(t)
	res, err := Build(BuildOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	var req *Materialisation
	for i := range res.Materialisations {
		if res.Materialisations[i].Contract.QName == "userstore/api.Request" {
			req = &res.Materialisations[i]
			break
		}
	}
	if req == nil {
		t.Fatal("api.Request materialisation missing")
	}
	if req.Contract.ContractKind() != "type" {
		t.Fatalf("Request contractKind = %q, want type", req.Contract.ContractKind())
	}
	names := make([]string, 0, len(req.Producers))
	for _, p := range req.Producers {
		names = append(names, p.Node.QName)
	}
	sort.Strings(names)
	want := []string{"userstore/api.NewRequest", "userstore/app.MakeRequest"}
	if !equalStringSlice(names, want) {
		t.Fatalf("Request producers = %v, want %v", names, want)
	}
}

// TestBuild_EmbeddingPropagation: when only UserStore is declared, the
// AdminStore interface (which embeds UserStore) inherits the marker
// with source="embedded".
func TestBuild_EmbeddingPropagation(t *testing.T) {
	dir := fixtureDir(t)
	tmpCfg := filepath.Join(t.TempDir(), ".archmotif.yaml")
	writeFile(t, tmpCfg, "contracts:\n  - interface: userstore/store.UserStore\n")
	res, err := Build(BuildOptions{Dir: dir, ConfigPath: tmpCfg})
	if err != nil {
		t.Fatal(err)
	}
	// Both UserStore and AdminStore should be marked.
	var user, admin *mgraph.Node
	for _, n := range res.Graph.NodesByKind(mgraph.NodeType) {
		if n.QName == "userstore/store.UserStore" {
			n := n
			user = &n
		}
		if n.QName == "userstore/store.AdminStore" {
			n := n
			admin = &n
		}
	}
	if user == nil || admin == nil {
		t.Fatalf("missing types: user=%v admin=%v", user, admin)
	}
	if !user.IsContract() || user.ContractSource() != "config" {
		t.Fatalf("UserStore not marked from config: %+v", user.Attrs)
	}
	if !admin.IsContract() {
		t.Fatalf("AdminStore should be marked via embedding")
	}
	if admin.ContractSource() != "embedded" {
		t.Fatalf("AdminStore source = %q, want embedded", admin.ContractSource())
	}
	if origin, _ := admin.Attrs[mgraph.AttrContractEmbeds].(string); origin != user.ID {
		t.Fatalf("AdminStore embeds origin = %q, want %q", origin, user.ID)
	}
}

// TestBuild_UnknownIdentifierIsUnresolved: a config entry pointing at
// a type that doesn't exist must surface as Unresolved, not crash.
func TestBuild_UnknownIdentifierIsUnresolved(t *testing.T) {
	dir := fixtureDir(t)
	tmpCfg := filepath.Join(t.TempDir(), ".archmotif.yaml")
	writeFile(t, tmpCfg, "contracts:\n  - interface: userstore/store.GhostStore\n  - type: not/a/pkg.Thing\n")
	res, err := Build(BuildOptions{Dir: dir, ConfigPath: tmpCfg})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Resolved) != 0 {
		t.Fatalf("nothing should resolve; got %d resolved", len(res.Resolved))
	}
	if len(res.Unresolved) != 2 {
		t.Fatalf("want 2 unresolved, got %d", len(res.Unresolved))
	}
}

// TestBuild_KindMismatchSurfaces: declaring an interface as `type:`
// (or vice-versa) records a KindMismatch warning but still resolves.
func TestBuild_KindMismatchSurfaces(t *testing.T) {
	dir := fixtureDir(t)
	tmpCfg := filepath.Join(t.TempDir(), ".archmotif.yaml")
	writeFile(t, tmpCfg, "contracts:\n  - type: userstore/store.UserStore\n")
	res, err := Build(BuildOptions{Dir: dir, ConfigPath: tmpCfg})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Resolved) != 1 {
		t.Fatalf("want 1 resolved, got %d", len(res.Resolved))
	}
	if len(res.KindMismatches) != 1 {
		t.Fatalf("want 1 kind mismatch, got %d (%v)", len(res.KindMismatches), res.KindMismatches)
	}
}

// TestProducers_ResolverRoundTrip is the unit-level resolver
// round-trip the issue calls out: string identifier → declared graph
// node, idempotent across repeated calls.
func TestProducers_ResolverRoundTrip(t *testing.T) {
	dir := fixtureDir(t)
	res, err := Build(BuildOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res.Resolved {
		if !res.Graph.HasNode(r.NodeID) {
			t.Fatalf("resolved %s.%s -> %q but graph has no such node", r.PkgPath, r.TypeName, r.NodeID)
		}
		n, _ := res.Graph.Node(r.NodeID)
		if n.QName != r.PkgPath+"."+r.TypeName {
			t.Fatalf("resolved node QName mismatch: got %q want %q", n.QName, r.PkgPath+"."+r.TypeName)
		}
	}
}
