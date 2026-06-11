package memopt

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Validation error sentinels. Tests and the loop CLI match against
// these via errors.Is so failure modes can be enumerated in run logs
// and exit codes mapped consistently. Each represents a distinct way
// the materializer's reply violated the protocol.
var (
	// ErrContractIDMismatch is returned when the patch's ContractID
	// does not equal the contract the validator was given.
	ErrContractIDMismatch = errors.New("memopt: patch contractId does not match contract")
	// ErrShapeChange is returned when the patch declares any operation
	// outside the contract's AllowedOps. Names the protocol concept of
	// "shape change" — the materializer changed what kind of edit it
	// is allowed to perform, not just the targets.
	ErrShapeChange = errors.New("memopt: patch contains operations outside contract.AllowedOps")
	// ErrUnknownOp is returned when the patch lists an Op string that
	// is not one of the known Operation constants. Distinct from
	// ErrShapeChange so log messages can distinguish typos
	// ("regroupp") from contract overreach ("merge" on an annotate-
	// only contract).
	ErrUnknownOp = errors.New("memopt: patch contains unknown operation kind")
	// ErrMissingNode is returned when AssignmentValidation does not
	// cover every Selected ID. The materializer must report on each
	// node it was handed; silence is rejected.
	ErrMissingNode = errors.New("memopt: patch assignmentValidation missing entries for selected nodes")
	// ErrExtraNode is returned when AssignmentValidation, Operations,
	// or any other patch field references an ID that is not on the
	// contract's Selected list. The materializer is not allowed to
	// reach beyond the batch.
	ErrExtraNode = errors.New("memopt: patch references nodes outside contract.Selected")
	// ErrForbiddenRemoval is returned when ForbidRemovals=true and the
	// patch contains a removal-shaped operation (OpMerge / OpRegroup
	// with empty SecondaryID).
	ErrForbiddenRemoval = errors.New("memopt: patch contains forbidden removal under contract.ForbidRemovals")
	// ErrMissingRationale is returned when the patch contains a
	// regroup or merge operation but GroupingRationale is empty. The
	// reviewer log requires a justification; silence is not allowed.
	ErrMissingRationale = errors.New("memopt: patch missing groupingRationale")
	// ErrMissingContextSources is returned when the patch carries no
	// ContextSourcesUsed entries. The protocol requires the
	// materializer to report what memory items it consulted; an empty
	// list is treated as a non-response.
	ErrMissingContextSources = errors.New("memopt: patch missing contextSourcesUsed report")
	// ErrUnknownOutcome is returned when an AssignmentResult.Outcome
	// is not one of the known strings.
	ErrUnknownOutcome = errors.New("memopt: patch assignmentValidation has unknown outcome")
	// ErrDuplicateAssignment is returned when AssignmentValidation
	// contains more than one entry for the same NodeID.
	ErrDuplicateAssignment = errors.New("memopt: patch assignmentValidation has duplicate nodeId")
)

// ValidationError wraps a sentinel with the offending IDs so logs can
// surface them. Tests compare via errors.Is on the sentinel.
type ValidationError struct {
	Err     error
	Details string
	IDs     []string
}

func (e *ValidationError) Error() string {
	if len(e.IDs) > 0 {
		return fmt.Sprintf("%s: %s [%s]", e.Err.Error(), e.Details, strings.Join(e.IDs, ", "))
	}
	if e.Details != "" {
		return fmt.Sprintf("%s: %s", e.Err.Error(), e.Details)
	}
	return e.Err.Error()
}

func (e *ValidationError) Unwrap() error { return e.Err }

// Validate checks p against c per the issue-#39 protocol. The function
// returns the first protocol violation it encounters; multi-error
// reporting is intentionally out of scope (the loop CLI re-runs
// validation after the materializer corrects its output, so cascade
// errors aren't useful).
//
// The validator answers four questions, each tied to a sentinel:
//
//  1. Did the materializer reply to the right contract?
//     (ErrContractIDMismatch)
//  2. Are the operations within the contract's allow-list?
//     (ErrShapeChange / ErrUnknownOp)
//  3. Do operations and assignments name only Selected nodes?
//     (ErrExtraNode / ErrMissingNode / ErrDuplicateAssignment /
//     ErrUnknownOutcome)
//  4. Are the protocol-mandated narrative fields populated?
//     (ErrMissingRationale / ErrMissingContextSources)
//
// And one safety question:
//
//   - Does any operation amount to a node removal under a
//     ForbidRemovals contract? (ErrForbiddenRemoval)
//
// The contract is assumed to have been validated already (Contract.Validate);
// Validate does not re-check it.
func Validate(c *Contract, p *Patch) error {
	if c == nil {
		return errors.New("memopt: nil contract")
	}
	if p == nil {
		return errors.New("memopt: nil patch")
	}
	if p.ContractID != c.ID {
		return &ValidationError{
			Err:     ErrContractIDMismatch,
			Details: fmt.Sprintf("got %q, want %q", p.ContractID, c.ID),
		}
	}

	allowed := c.AllowedOpSet()
	selectedSet := c.SelectedSet()

	// Pass 1: operations must be on AllowedOps and target Selected.
	for i, op := range p.Operations {
		switch op.Op {
		case OpRegroup, OpMerge, OpRetitle, OpAnnotate:
			// known
		default:
			return &ValidationError{
				Err:     ErrUnknownOp,
				Details: fmt.Sprintf("operations[%d]=%q", i, op.Op),
			}
		}
		if _, ok := allowed[op.Op]; !ok {
			return &ValidationError{
				Err:     ErrShapeChange,
				Details: fmt.Sprintf("operations[%d]=%q not in AllowedOps", i, op.Op),
			}
		}
		if op.TargetID == "" {
			return &ValidationError{
				Err:     ErrExtraNode,
				Details: fmt.Sprintf("operations[%d] has empty targetId", i),
			}
		}
		if _, ok := selectedSet[op.TargetID]; !ok {
			return &ValidationError{
				Err:     ErrExtraNode,
				Details: fmt.Sprintf("operations[%d].targetId=%q not in Selected", i, op.TargetID),
				IDs:     []string{op.TargetID},
			}
		}
		// SecondaryID, when present, may freely reference nodes
		// outside Selected (a regroup target is a parent the
		// materializer found in memory context, not a node from the
		// batch). But on a ForbidRemovals contract a Merge/Regroup
		// with empty SecondaryID is a tombstone and rejected.
		if c.ForbidRemovals {
			if (op.Op == OpMerge || op.Op == OpRegroup) && op.SecondaryID == "" {
				return &ValidationError{
					Err:     ErrForbiddenRemoval,
					Details: fmt.Sprintf("operations[%d]=%s on %q has empty secondaryId", i, op.Op, op.TargetID),
					IDs:     []string{op.TargetID},
				}
			}
		}
	}

	// Pass 2: assignmentValidation must cover every Selected ID exactly
	// once, name only Selected IDs, and carry known outcomes.
	if err := validateAssignments(c, p, selectedSet); err != nil {
		return err
	}

	// Pass 3: protocol-mandated narrative fields.
	hasRegroupOrMerge := false
	for _, op := range p.Operations {
		if op.Op == OpRegroup || op.Op == OpMerge {
			hasRegroupOrMerge = true
			break
		}
	}
	if hasRegroupOrMerge && strings.TrimSpace(p.GroupingRationale) == "" {
		return &ValidationError{
			Err:     ErrMissingRationale,
			Details: "regroup/merge operation requires non-empty groupingRationale",
		}
	}
	if len(p.ContextSourcesUsed) == 0 {
		return &ValidationError{
			Err:     ErrMissingContextSources,
			Details: "patch must list at least one ContextSource",
		}
	}
	for i, src := range p.ContextSourcesUsed {
		if strings.TrimSpace(src.ID) == "" {
			return &ValidationError{
				Err:     ErrMissingContextSources,
				Details: fmt.Sprintf("contextSourcesUsed[%d] has empty id", i),
			}
		}
	}
	return nil
}

// validateAssignments enforces the per-Selected-node assignment
// completeness rule and the closed-set outcome rule.
func validateAssignments(c *Contract, p *Patch, selectedSet map[string]struct{}) error {
	covered := make(map[string]struct{}, len(p.AssignmentValidation))
	for i, a := range p.AssignmentValidation {
		if a.NodeID == "" {
			return &ValidationError{
				Err:     ErrExtraNode,
				Details: fmt.Sprintf("assignmentValidation[%d] has empty nodeId", i),
			}
		}
		if _, ok := selectedSet[a.NodeID]; !ok {
			return &ValidationError{
				Err:     ErrExtraNode,
				Details: fmt.Sprintf("assignmentValidation[%d].nodeId=%q not in Selected", i, a.NodeID),
				IDs:     []string{a.NodeID},
			}
		}
		if _, dup := covered[a.NodeID]; dup {
			return &ValidationError{
				Err:     ErrDuplicateAssignment,
				Details: fmt.Sprintf("nodeId=%q appears twice in assignmentValidation", a.NodeID),
				IDs:     []string{a.NodeID},
			}
		}
		covered[a.NodeID] = struct{}{}
		if _, ok := knownOutcomes[a.Outcome]; !ok {
			return &ValidationError{
				Err:     ErrUnknownOutcome,
				Details: fmt.Sprintf("assignmentValidation[%d].outcome=%q", i, a.Outcome),
			}
		}
	}
	if len(covered) != len(c.Selected) {
		// Find which Selected IDs are missing for the error log.
		missing := make([]string, 0, len(c.Selected)-len(covered))
		for _, s := range c.Selected {
			if _, ok := covered[s.ID]; !ok {
				missing = append(missing, s.ID)
			}
		}
		sort.Strings(missing)
		return &ValidationError{
			Err:     ErrMissingNode,
			Details: fmt.Sprintf("%d/%d Selected nodes missing", len(missing), len(c.Selected)),
			IDs:     missing,
		}
	}
	return nil
}
