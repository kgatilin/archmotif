package metrics

import (
	"context"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Zero is a sanity metric whose only purpose is to prove the
// "one new file = registered metric" discipline (per ADR-011 and
// issue #4). It always emits exactly one ScopeGraph record with
// value 0. Tests assert this metric is registered and that the
// runner picks it up without any additional wiring.
type Zero struct{}

// Name returns the metric identifier.
func (Zero) Name() string { return "zero" }

// Description returns the metric documentation string.
func (Zero) Description() string {
	return "always-zero sanity metric (registration discipline proof)"
}

// Configurable returns user-tunable knobs (none).
func (Zero) Configurable() map[string]any { return map[string]any{} }

// Compute returns one record with value 0.
func (Zero) Compute(_ context.Context, _ *mgraph.Graph) ([]Record, error) {
	return []Record{{
		Metric: "zero",
		Scope:  ScopeGraph,
		Value:  0,
	}}, nil
}

func init() { Register(Zero{}) }
