package main

import "time"

// OptimizeLoopBatchReport records the outcome of one optimization batch.
// The explicit type name is part of the graph substrate: graph-only planning
// agents can search for "batch report" and find the run-level semantics.
type OptimizeLoopBatchReport struct {
	Index       int    `json:"index"`
	Dir         string `json:"dir"`
	ProposalID  string `json:"proposal_id"`
	Description string `json:"description"`
	RuleKind    string `json:"rule_kind"`
	NodeCount   int    `json:"node_count"`
	EdgeCount   int    `json:"edge_count"`
	Outcome     string `json:"outcome"`
	Reason      string `json:"reason,omitempty"`
}

// OptimizeLoopRunReport is the run-summary aggregate written to summary.json
// and rendered in the human-readable summary line. It is intentionally
// command-local for now; promote it only when a second caller needs it.
type OptimizeLoopRunReport struct {
	StartedAt    string                        `json:"started_at"`
	FinishedAt   string                        `json:"finished_at"`
	RepoDir      string                        `json:"repo_dir"`
	RunDir       string                        `json:"run_dir"`
	Materializer string                        `json:"materializer"`
	DryRun       bool                          `json:"dry_run"`
	Apply        bool                          `json:"apply"`
	MaxBatches   int                           `json:"max_batches"`
	BeforeNodes  int                           `json:"before_nodes"`
	BeforeEdges  int                           `json:"before_edges"`
	AfterNodes   int                           `json:"after_nodes"`
	AfterEdges   int                           `json:"after_edges"`
	Batches      []*OptimizeLoopBatchReport    `json:"batches"`
	Convergence  OptimizeLoopConvergenceReport `json:"convergence"`
	StoppedBy    string                        `json:"stopped_by"`
}

// OptimizeLoopConvergenceReport explains why a repeated optimization run
// stopped. This is the graph-visible run-semantic concept that E2 found
// missing from the substrate.
type OptimizeLoopConvergenceReport struct {
	Status               string `json:"status"`
	StopReason           string `json:"stop_reason"`
	IterationsRun        int    `json:"iterations_run"`
	Improved             bool   `json:"improved"`
	LastImprovementBatch int    `json:"last_improvement_batch,omitempty"`
	ScoreBefore          int    `json:"score_before"`
	ScoreAfter           int    `json:"score_after"`
	ScoreDelta           int    `json:"score_delta"`
}

func buildOptimizeLoopConvergence(r *OptimizeLoopRunReport) OptimizeLoopConvergenceReport {
	if r == nil {
		return OptimizeLoopConvergenceReport{Status: "error", StopReason: "missing run report"}
	}
	status := "error"
	switch r.StoppedBy {
	case "max-batches":
		status = "max_batches"
	case outcomeNoBatch:
		status = "no_candidates"
	case outcomeDryRun:
		status = "dry_run"
	case outcomeOK:
		status = "converged"
	case outcomeNoDiff:
		status = "no_improvement"
	case outcomeArtifactErr, outcomePipelineErr, outcomeMaterializerErr, outcomeValidateFail, outcomeApplyFail:
		status = "error"
	case "":
		status = "unknown"
	}

	scoreBefore := r.BeforeNodes + r.BeforeEdges
	scoreAfter := r.AfterNodes + r.AfterEdges
	improved := scoreAfter > 0 && scoreBefore > 0 && scoreAfter < scoreBefore
	lastImprovement := 0
	if improved {
		for i := len(r.Batches) - 1; i >= 0; i-- {
			if r.Batches[i].Outcome == outcomeOK {
				lastImprovement = r.Batches[i].Index
				break
			}
		}
	}

	return OptimizeLoopConvergenceReport{
		Status:               status,
		StopReason:           r.StoppedBy,
		IterationsRun:        len(r.Batches),
		Improved:             improved,
		LastImprovementBatch: lastImprovement,
		ScoreBefore:          scoreBefore,
		ScoreAfter:           scoreAfter,
		ScoreDelta:           scoreAfter - scoreBefore,
	}
}

func finalizeOptimizeLoopRunReport(r *OptimizeLoopRunReport, now func() time.Time) {
	if r == nil {
		return
	}
	if now == nil {
		now = time.Now
	}
	r.FinishedAt = now().UTC().Format(time.RFC3339)
	r.Convergence = buildOptimizeLoopConvergence(r)
}
