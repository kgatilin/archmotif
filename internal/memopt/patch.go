package memopt

// Patch is the materializer's reply for one Contract. The shape is
// deliberately flat JSON so the loop CLI (#38) can decode it with the
// stdlib without a code generator.
//
// Field semantics:
//
//   - ContractID echoes Contract.ID so the loop can correlate the
//     reply with the contract that produced it. The validator rejects
//     a mismatched ContractID.
//   - Operations enumerates the structural edits, in order. Each one
//     names a target node ID; the validator rejects targets not on the
//     contract's Selected list and operations not on AllowedOps.
//   - AssignmentValidation records the materializer's per-node check
//     that every Selected node has either been kept-as-is, regrouped,
//     merged, or annotated — exactly one outcome per Selected ID. A
//     Patch missing entries (or carrying entries for non-Selected IDs)
//     is rejected.
//   - GroupingRationale is the human-readable justification for the
//     regrouping. Required by the contract: a Patch with empty
//     rationale on a regroup-bearing operation set is rejected. The
//     loop CLI surfaces this string in run logs so reviewers see why.
//   - ContextSourcesUsed lists the memory items the materializer
//     fetched while producing the patch. Required so the run log can
//     prove the materializer actually used context (not hallucinated)
//     and so a reviewer can trace decisions back to source items.
type Patch struct {
	ContractID           string             `json:"contractId"`
	Operations           []PatchOp          `json:"operations"`
	AssignmentValidation []AssignmentResult `json:"assignmentValidation"`
	GroupingRationale    string             `json:"groupingRationale"`
	ContextSourcesUsed   []ContextSource    `json:"contextSourcesUsed"`
}

// PatchOp is one structural edit. The shape stays uniform across
// operation kinds (a single Target ID) so the validator does one pass.
//
//   - Op names the operation; must be on Contract.AllowedOps.
//   - TargetID is the node ID the op acts on; must be on
//     Contract.Selected.
//   - SecondaryID, when non-empty, names a second node involved in
//     the op (the merge survivor for OpMerge, the new parent for
//     OpRegroup). Tombstoning a node would set it to empty; the
//     validator treats empty Secondary on OpMerge / OpRegroup as a
//     removal and rejects it when Contract.ForbidRemovals is set.
//   - Rationale is the per-op rationale. Empty is allowed for
//     OpAnnotate / OpRetitle (the GroupingRationale carries the
//     overall justification) but populated rationales surface in
//     reviewer logs.
type PatchOp struct {
	Op          Operation `json:"op"`
	TargetID    string    `json:"targetId"`
	SecondaryID string    `json:"secondaryId,omitempty"`
	Rationale   string    `json:"rationale,omitempty"`
}

// AssignmentResult is the materializer's per-Selected-node disposition.
// Exactly one entry per Selected ID, in any order. The Outcome string
// is one of: "kept", "regrouped", "merged", "retitled", "annotated".
type AssignmentResult struct {
	NodeID  string `json:"nodeId"`
	Outcome string `json:"outcome"`
	Note    string `json:"note,omitempty"`
}

// ContextSource is one memory item the materializer fetched. ID is
// required (matches an item the materializer looked up by ID/title);
// Title and Excerpt are informational and surface in reviewer logs.
type ContextSource struct {
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Excerpt string `json:"excerpt,omitempty"`
}

// Known assignment outcomes. The validator rejects any other string.
const (
	OutcomeKept      = "kept"
	OutcomeRegrouped = "regrouped"
	OutcomeMerged    = "merged"
	OutcomeRetitled  = "retitled"
	OutcomeAnnotated = "annotated"
)

// knownOutcomes is the closed set used by the validator.
var knownOutcomes = map[string]struct{}{
	OutcomeKept:      {},
	OutcomeRegrouped: {},
	OutcomeMerged:    {},
	OutcomeRetitled:  {},
	OutcomeAnnotated: {},
}
