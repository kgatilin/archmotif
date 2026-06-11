package mcpserver

import (
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/propose"
	"github.com/kgatilin/archmotif/internal/targetcontract"
)

func TestPutListShowTargetGraph(t *testing.T) {
	svc, _ := mustService(t, "demo")
	contract := targetContractFixture()

	ref, err := svc.PutTargetGraph("demo", "split-optimize", contract, false)
	if err != nil {
		t.Fatalf("PutTargetGraph: %v", err)
	}
	if ref.GraphID != "demo:target-split-optimize" {
		t.Fatalf("graph_id = %q", ref.GraphID)
	}
	if ref.Nodes == 0 || ref.Edges == 0 {
		t.Fatalf("expected populated target graph, got %d/%d", ref.Nodes, ref.Edges)
	}
	if ref.Edges != 7 {
		t.Fatalf("edges = %d, want 7 deduplicated target edges", ref.Edges)
	}
	if _, err := svc.CheckoutGraph(ref.GraphID); err != nil {
		t.Fatalf("checkout target graph: %v", err)
	}

	targets, err := svc.ListTargets("demo")
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if len(targets) != 1 || targets[0].TargetID != "split-optimize" {
		t.Fatalf("targets = %+v", targets)
	}

	shape, err := svc.ShowTarget("demo", "split-optimize")
	if err != nil {
		t.Fatalf("ShowTarget: %v", err)
	}
	if len(shape.Nodes) != ref.Nodes || len(shape.Edges) != ref.Edges {
		t.Fatalf("shape size = %d/%d, want %d/%d", len(shape.Nodes), len(shape.Edges), ref.Nodes, ref.Edges)
	}
	if !targetShapeHasNode(shape, "pkg:example.com/app/internal/optimize") {
		t.Fatalf("target shape missing internal optimize package: %+v", shape.Nodes)
	}
}

func targetContractFixture() targetcontract.Contract {
	return targetcontract.Contract{
		Version:     1,
		ID:          "target-test",
		Kind:        "command_package_split",
		Description: "split optimize orchestration",
		Source:      targetcontract.SourceSpec{ModulePath: "example.com/app", CommandPackage: "example.com/app/cmd/app"},
		Packages: []targetcontract.PackageSpec{
			{Role: "CLIAdapter", ImportPath: "example.com/app/cmd/app", Dir: "cmd/app", Name: "main", Action: "keep"},
			{Role: "OptimizeOrchestration", ImportPath: "example.com/app/internal/optimize", Dir: "internal/optimize", Name: "optimize", Action: "create"},
		},
		Files: []targetcontract.FileSpec{
			{Path: "internal/optimize/run.go", PackageRole: "OptimizeOrchestration", PackageName: "optimize", Action: "create"},
		},
		PublicTypes: []targetcontract.TypeSpec{
			{Name: "Options", Kind: "struct", PackageRole: "OptimizeOrchestration", PackagePath: "example.com/app/internal/optimize", File: "internal/optimize/run.go"},
			{Name: "Result", Kind: "struct", PackageRole: "OptimizeOrchestration", PackagePath: "example.com/app/internal/optimize", File: "internal/optimize/run.go"},
		},
		PublicFunctions: []targetcontract.FunctionSpec{
			{Name: "Run", PackageRole: "OptimizeOrchestration", PackagePath: "example.com/app/internal/optimize", File: "internal/optimize/run.go", Signature: "func Run(ctx context.Context, opts Options) (Result, error)"},
		},
		ExpectedEdges: []targetcontract.EdgeSpec{
			{FromRole: "CLIAdapter", ToRole: "OptimizeOrchestration", From: "pkg:example.com/app/cmd/app", To: "pkg:example.com/app/internal/optimize", Kind: "dependsOn"},
		},
		TargetSubgraph: propose.TargetSubgraph{
			Roles: []propose.Role{
				{Name: "CLIAdapter", Kind: mgraph.NodePackage},
				{Name: "OptimizeOrchestration", Kind: mgraph.NodePackage},
				{Name: "OptimizeOptions", Kind: mgraph.NodeType, Attrs: map[string]any{"typeName": "Options"}},
				{Name: "OptimizeResult", Kind: mgraph.NodeType, Attrs: map[string]any{"typeName": "Result"}},
				{Name: "OptimizeRun", Kind: mgraph.NodeFunction, Attrs: map[string]any{"functionName": "Run"}},
			},
			Edges: []propose.EdgeConstraint{
				{From: "CLIAdapter", To: "OptimizeOrchestration", Kind: mgraph.EdgeDependsOn},
				{From: "OptimizeRun", To: "OptimizeOptions", Kind: mgraph.EdgeUsesType},
				{From: "OptimizeRun", To: "OptimizeResult", Kind: mgraph.EdgeReturns},
			},
		},
	}
}

func targetShapeHasNode(shape TargetShape, id string) bool {
	for _, n := range shape.Nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}
