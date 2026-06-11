package metrics_test

import (
	"context"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

func TestZero_AlwaysZero(t *testing.T) {
	g := mgraph.New()
	recs, err := metrics.Zero{}.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0].Value != 0 {
		t.Fatalf("expected 0, got %v", recs[0].Value)
	}
	if recs[0].Scope != metrics.ScopeGraph {
		t.Fatalf("expected graph scope, got %s", recs[0].Scope)
	}
}

func TestZero_AutoRegistered(t *testing.T) {
	if _, ok := metrics.Lookup("zero"); !ok {
		t.Fatal("zero metric not auto-registered via init()")
	}
}
