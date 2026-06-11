package metrics_test

import (
	"context"
	"strings"
	"testing"

	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/metrics/metricstest"
)

func TestLocalSymmetry_FanoutMethodsScoreTwo(t *testing.T) {
	// Type T contains M1, M2, M3. Each method has identical role
	// signature (kind=method, no outbound, one inbound contains).
	// At ≤2 hops from any method we reach: T (via in:contains) and
	// then via T's outbound contains the other two methods. So the
	// matching set for M1 is {M2, M3} → score 2.
	g := metricstest.SymmetricFanout()
	recs, err := metrics.LocalSymmetry{}.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	methods := 0
	for _, r := range recs {
		if r.Scope != metrics.ScopeNode {
			continue
		}
		if !strings.Contains(r.Target, ":method:") {
			continue
		}
		methods++
		if r.Value != 2 {
			t.Fatalf("method %s symmetry = %v, want 2", r.Target, r.Value)
		}
	}
	if methods != 3 {
		t.Fatalf("got %d method records, want 3", methods)
	}
}

func TestLocalSymmetry_PurePathInteriorMatchesInterior(t *testing.T) {
	// Pure 4-path N0→N1→N2→N3. Endpoints (N0, N3) have asymmetric
	// in/out signatures (only one of in or out is non-empty). The two
	// interior nodes (N1, N2) share the signature
	// "function|out:calls:1|in:calls:1" and are within 2 hops of
	// each other → each scores 1. The endpoints score 0.
	g := buildPurePath(4)
	recs, err := metrics.LocalSymmetry{}.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	scores := map[string]float64{}
	for _, r := range recs {
		if r.Scope != metrics.ScopeNode {
			continue
		}
		scores[r.Target] = r.Value
	}
	if scores["purepath/main.go:1:1:function:N0"] != 0 {
		t.Fatalf("N0 expected 0, got %v", scores["purepath/main.go:1:1:function:N0"])
	}
	if scores["purepath/main.go:1:1:function:N3"] != 0 {
		t.Fatalf("N3 expected 0, got %v", scores["purepath/main.go:1:1:function:N3"])
	}
	if scores["purepath/main.go:1:1:function:N1"] != 1 {
		t.Fatalf("N1 expected 1, got %v", scores["purepath/main.go:1:1:function:N1"])
	}
	if scores["purepath/main.go:1:1:function:N2"] != 1 {
		t.Fatalf("N2 expected 1, got %v", scores["purepath/main.go:1:1:function:N2"])
	}
}
