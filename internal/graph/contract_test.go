package graph

import "testing"

func TestMarkContract_AppliesAttrs(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "t", Kind: NodeType, Name: "T"})
	if !g.MarkContract("t", "interface", "config", nil) {
		t.Fatal("expected MarkContract to insert")
	}
	got, _ := g.Node("t")
	if !got.IsContract() {
		t.Fatal("IsContract should be true after MarkContract")
	}
	if got.ContractKind() != "interface" {
		t.Fatalf("ContractKind = %q, want interface", got.ContractKind())
	}
	if got.ContractSource() != "config" {
		t.Fatalf("ContractSource = %q, want config", got.ContractSource())
	}
}

func TestMarkContract_Idempotent(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "t", Kind: NodeType, Name: "T"})
	if !g.MarkContract("t", "interface", "config", nil) {
		t.Fatal("first MarkContract should insert")
	}
	if g.MarkContract("t", "interface", "embedded", nil) {
		t.Fatal("second MarkContract should report no change")
	}
	got, _ := g.Node("t")
	// First-write-wins: source should still be "config".
	if got.ContractSource() != "config" {
		t.Fatalf("expected first marker to win; source=%q", got.ContractSource())
	}
}

func TestMarkContract_UnknownNodeIsNoOp(t *testing.T) {
	g := New()
	if g.MarkContract("missing", "interface", "config", nil) {
		t.Fatal("MarkContract on missing node should return false")
	}
}

func TestNodeAccessors_DefaultZero(t *testing.T) {
	n := Node{ID: "x", Kind: NodeType, Name: "X"}
	if n.IsContract() {
		t.Fatal("default Node should not be a contract")
	}
	if n.ContractKind() != "" {
		t.Fatalf("default ContractKind should be empty; got %q", n.ContractKind())
	}
}
