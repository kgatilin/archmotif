package memopt_test

import (
	"errors"
	"testing"

	"github.com/kgatilin/archmotif/internal/memopt"
)

// goodPatch returns a Patch that passes Validate against the
// orphan-batch fixture. Tests start from this baseline and mutate one
// field to exercise each rejection path.
func goodPatch(c *memopt.Contract) *memopt.Patch {
	ops := make([]memopt.PatchOp, 0, len(c.Selected))
	av := make([]memopt.AssignmentResult, 0, len(c.Selected))
	for i, s := range c.Selected {
		// Alternate regroup and annotate so the patch exercises both
		// branches of the validator (rationale-required vs not).
		if i%2 == 0 {
			ops = append(ops, memopt.PatchOp{
				Op:          memopt.OpRegroup,
				TargetID:    s.ID,
				SecondaryID: "mem:parent-secrets",
				Rationale:   "fits secrets cluster",
			})
			av = append(av, memopt.AssignmentResult{NodeID: s.ID, Outcome: memopt.OutcomeRegrouped})
		} else {
			ops = append(ops, memopt.PatchOp{
				Op:       memopt.OpAnnotate,
				TargetID: s.ID,
			})
			av = append(av, memopt.AssignmentResult{NodeID: s.ID, Outcome: memopt.OutcomeAnnotated})
		}
	}
	return &memopt.Patch{
		ContractID:           c.ID,
		Operations:           ops,
		AssignmentValidation: av,
		GroupingRationale:    "Selected orphans cluster around secrets/runtime/notes; regrouped two under existing parents, annotated the rest.",
		ContextSourcesUsed: []memopt.ContextSource{
			{ID: "mem:parent-secrets", Title: "secrets parent"},
			{ID: "mem:9d1a", Title: "Service token rotation"},
		},
	}
}

// TestValidate_HappyPath confirms the baseline patch passes when paired
// with the orphan-batch contract.
func TestValidate_HappyPath(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	p := goodPatch(c)
	if err := memopt.Validate(c, p); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestValidate_RejectsContractIDMismatch — patch that replies to the
// wrong contract is rejected with ErrContractIDMismatch.
func TestValidate_RejectsContractIDMismatch(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	p := goodPatch(c)
	p.ContractID = "some-other-contract"
	err := memopt.Validate(c, p)
	if !errors.Is(err, memopt.ErrContractIDMismatch) {
		t.Fatalf("err = %v, want ErrContractIDMismatch", err)
	}
}

// TestValidate_RejectsShapeChange — ops outside AllowedOps fail with
// ErrShapeChange. Orphan contract allows regroup+annotate; we slip in
// a merge.
func TestValidate_RejectsShapeChange(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	p := goodPatch(c)
	p.Operations[0] = memopt.PatchOp{
		Op:          memopt.OpMerge,
		TargetID:    c.Selected[0].ID,
		SecondaryID: "mem:survivor",
	}
	err := memopt.Validate(c, p)
	if !errors.Is(err, memopt.ErrShapeChange) {
		t.Fatalf("err = %v, want ErrShapeChange", err)
	}
}

// TestValidate_RejectsUnknownOp — a typo'd Op string is distinguished
// from a contract overreach.
func TestValidate_RejectsUnknownOp(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	p := goodPatch(c)
	p.Operations[0].Op = memopt.Operation("regroupp")
	err := memopt.Validate(c, p)
	if !errors.Is(err, memopt.ErrUnknownOp) {
		t.Fatalf("err = %v, want ErrUnknownOp", err)
	}
}

// TestValidate_RejectsExtraNode_Op — an operation targeting a node not
// in Selected is rejected.
func TestValidate_RejectsExtraNode_Op(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	p := goodPatch(c)
	p.Operations[0].TargetID = "mem:not-in-batch"
	err := memopt.Validate(c, p)
	if !errors.Is(err, memopt.ErrExtraNode) {
		t.Fatalf("err = %v, want ErrExtraNode", err)
	}
}

// TestValidate_RejectsExtraNode_Assignment — an assignmentValidation
// entry naming a non-Selected ID is rejected.
func TestValidate_RejectsExtraNode_Assignment(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	p := goodPatch(c)
	p.AssignmentValidation[0].NodeID = "mem:rogue"
	err := memopt.Validate(c, p)
	if !errors.Is(err, memopt.ErrExtraNode) {
		t.Fatalf("err = %v, want ErrExtraNode", err)
	}
}

// TestValidate_RejectsMissingNode — assignmentValidation that doesn't
// cover every Selected ID is rejected, and the error names the
// missing IDs.
func TestValidate_RejectsMissingNode(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	p := goodPatch(c)
	// Drop the last assignment, simulating a materializer that
	// produced operations but forgot to mirror them in the validation
	// block.
	p.AssignmentValidation = p.AssignmentValidation[:len(p.AssignmentValidation)-1]
	missingID := c.Selected[len(c.Selected)-1].ID
	err := memopt.Validate(c, p)
	if !errors.Is(err, memopt.ErrMissingNode) {
		t.Fatalf("err = %v, want ErrMissingNode", err)
	}
	var ve *memopt.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, expected *ValidationError", err)
	}
	if len(ve.IDs) != 1 || ve.IDs[0] != missingID {
		t.Errorf("err.IDs = %v, want [%q]", ve.IDs, missingID)
	}
}

// TestValidate_RejectsForbiddenRemoval — a regroup with empty
// secondaryId on a ForbidRemovals contract is rejected.
func TestValidate_RejectsForbiddenRemoval(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	p := goodPatch(c)
	p.Operations[0].SecondaryID = ""
	err := memopt.Validate(c, p)
	if !errors.Is(err, memopt.ErrForbiddenRemoval) {
		t.Fatalf("err = %v, want ErrForbiddenRemoval", err)
	}
}

// TestValidate_AllowsRemoval_WhenContractAllows — same patch shape
// passes when ForbidRemovals=false.
func TestValidate_AllowsRemoval_WhenContractAllows(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	c.ForbidRemovals = false
	p := goodPatch(c)
	p.Operations[0].SecondaryID = ""
	if err := memopt.Validate(c, p); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestValidate_RejectsMissingRationale — a regroup-bearing patch with
// empty groupingRationale fails.
func TestValidate_RejectsMissingRationale(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	p := goodPatch(c)
	p.GroupingRationale = "   "
	err := memopt.Validate(c, p)
	if !errors.Is(err, memopt.ErrMissingRationale) {
		t.Fatalf("err = %v, want ErrMissingRationale", err)
	}
}

// TestValidate_AllowsEmptyRationale_WhenAnnotateOnly — annotate-only
// patches don't require a grouping rationale.
func TestValidate_AllowsEmptyRationale_WhenAnnotateOnly(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	p := &memopt.Patch{
		ContractID: c.ID,
	}
	for _, s := range c.Selected {
		p.Operations = append(p.Operations, memopt.PatchOp{
			Op:       memopt.OpAnnotate,
			TargetID: s.ID,
		})
		p.AssignmentValidation = append(p.AssignmentValidation, memopt.AssignmentResult{
			NodeID:  s.ID,
			Outcome: memopt.OutcomeAnnotated,
		})
	}
	p.ContextSourcesUsed = []memopt.ContextSource{{ID: "mem:9d1a"}}
	if err := memopt.Validate(c, p); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestValidate_RejectsMissingContextSources — empty ContextSourcesUsed
// is treated as a non-response.
func TestValidate_RejectsMissingContextSources(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	p := goodPatch(c)
	p.ContextSourcesUsed = nil
	err := memopt.Validate(c, p)
	if !errors.Is(err, memopt.ErrMissingContextSources) {
		t.Fatalf("err = %v, want ErrMissingContextSources", err)
	}
}

// TestValidate_RejectsContextSource_EmptyID — a context source entry
// with an empty id is rejected (still counts as "no source").
func TestValidate_RejectsContextSource_EmptyID(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	p := goodPatch(c)
	p.ContextSourcesUsed = append(p.ContextSourcesUsed, memopt.ContextSource{ID: ""})
	err := memopt.Validate(c, p)
	if !errors.Is(err, memopt.ErrMissingContextSources) {
		t.Fatalf("err = %v, want ErrMissingContextSources", err)
	}
}

// TestValidate_RejectsUnknownOutcome — a typo'd outcome string fails.
func TestValidate_RejectsUnknownOutcome(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	p := goodPatch(c)
	p.AssignmentValidation[0].Outcome = "deleted"
	err := memopt.Validate(c, p)
	if !errors.Is(err, memopt.ErrUnknownOutcome) {
		t.Fatalf("err = %v, want ErrUnknownOutcome", err)
	}
}

// TestValidate_RejectsDuplicateAssignment — two entries for the same
// nodeId is treated as a malformed patch even if other entries are
// fine.
func TestValidate_RejectsDuplicateAssignment(t *testing.T) {
	c := loadContract(t, "orphan_batch.json")
	p := goodPatch(c)
	// Replace the second entry with a duplicate of the first.
	p.AssignmentValidation[1] = p.AssignmentValidation[0]
	err := memopt.Validate(c, p)
	if !errors.Is(err, memopt.ErrDuplicateAssignment) {
		t.Fatalf("err = %v, want ErrDuplicateAssignment", err)
	}
}
