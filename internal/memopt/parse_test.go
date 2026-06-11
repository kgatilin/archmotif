package memopt_test

import (
	"errors"
	"testing"

	"github.com/kgatilin/archmotif/internal/memopt"
)

const goodReply = "Here is the patch:\n\n```json\n" +
	`{
  "contractId": "orphan-2026-05-06",
  "operations": [
    {"op": "annotate", "targetId": "mem:9d1a"}
  ],
  "assignmentValidation": [
    {"nodeId": "mem:9d1a", "outcome": "annotated"},
    {"nodeId": "mem:7b22", "outcome": "annotated"},
    {"nodeId": "mem:3c81", "outcome": "annotated"},
    {"nodeId": "mem:5d04", "outcome": "annotated"}
  ],
  "groupingRationale": "",
  "contextSourcesUsed": [
    {"id": "mem:9d1a"}
  ]
}` + "\n```\n"

// TestParsePatch_HappyPath — a well-formed reply round-trips.
func TestParsePatch_HappyPath(t *testing.T) {
	p, err := memopt.ParsePatch(goodReply)
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}
	if p.ContractID != "orphan-2026-05-06" {
		t.Errorf("ContractID = %q, want orphan-2026-05-06", p.ContractID)
	}
	if len(p.AssignmentValidation) != 4 {
		t.Errorf("AssignmentValidation = %d entries, want 4", len(p.AssignmentValidation))
	}
}

// TestParsePatch_NoFence pins the wire-shape contract: no ```json
// block at all.
func TestParsePatch_NoFence(t *testing.T) {
	_, err := memopt.ParsePatch("I cannot do that.")
	if !errors.Is(err, memopt.ErrNoFencedJSON) {
		t.Fatalf("err = %v, want ErrNoFencedJSON", err)
	}
}

// TestParsePatch_MultipleFences — more than one ```json block is
// rejected even if both decode cleanly.
func TestParsePatch_MultipleFences(t *testing.T) {
	twin := goodReply + "\nand again:\n\n" + goodReply
	_, err := memopt.ParsePatch(twin)
	if !errors.Is(err, memopt.ErrMultipleFencedJSON) {
		t.Fatalf("err = %v, want ErrMultipleFencedJSON", err)
	}
}

// TestParsePatch_BadJSON — the fence is present but the body is not
// valid JSON.
func TestParsePatch_BadJSON(t *testing.T) {
	bad := "```json\n{this is not valid}\n```\n"
	_, err := memopt.ParsePatch(bad)
	if !errors.Is(err, memopt.ErrJSONDecode) {
		t.Fatalf("err = %v, want ErrJSONDecode", err)
	}
}

// TestParsePatch_UnknownField — the wire shape is closed; a field
// outside the documented schema fails decode under DisallowUnknownFields.
func TestParsePatch_UnknownField(t *testing.T) {
	bad := "```json\n" + `{"contractId":"x","unexpectedField":42}` + "\n```\n"
	_, err := memopt.ParsePatch(bad)
	if !errors.Is(err, memopt.ErrJSONDecode) {
		t.Fatalf("err = %v, want ErrJSONDecode", err)
	}
}
