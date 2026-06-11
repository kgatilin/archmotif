package targetcontract_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/propose"
	"github.com/kgatilin/archmotif/internal/targetcontract"
)

func TestBuildFromOptimizeEnvelope_CommandPackageSplit(t *testing.T) {
	env := targetcontract.OptimizeEnvelope{
		Contracts: []targetcontract.OptimizeContract{{
			ID:          "optimization-command_package_split-pkg:example.com/app/cmd/app",
			Kind:        "command_package_split",
			Rule:        "command_package_split",
			ProposalID:  "command_package_split-pkg:example.com/app/cmd/app",
			Description: "split command package",
			Objective: targetcontract.OptimizeObjective{
				Target: "pkg:example.com/app/cmd/app",
			},
			Target: targetcontract.OptimizeTarget{
				Subgraph: propose.TargetSubgraph{
					Roles: []propose.Role{
						{Name: "CLIAdapter", Kind: mgraph.NodePackage, Cardinality: 1, Attrs: map[string]any{"packageAction": "keep", "packageRole": "cmd_adapter"}},
						{Name: "OptimizeOrchestration", Kind: mgraph.NodePackage, Cardinality: 1, Attrs: map[string]any{"packageAction": "create", "packageRole": "internal_orchestration", "packagePath": "internal/optimize", "packageName": "optimize"}},
						{Name: "OptimizeOptions", Kind: mgraph.NodeType, Cardinality: 1, Attrs: map[string]any{"packageRole": "internal_orchestration", "typeName": "Options", "typeKind": "struct", "file": "internal/optimize/run.go"}},
						{Name: "OptimizeResult", Kind: mgraph.NodeType, Cardinality: 1, Attrs: map[string]any{"packageRole": "internal_orchestration", "typeName": "Result", "typeKind": "struct", "file": "internal/optimize/run.go"}},
						{Name: "OptimizeRun", Kind: mgraph.NodeFunction, Cardinality: 1, Attrs: map[string]any{"packageRole": "internal_orchestration", "functionName": "Run", "signature": "func Run(ctx context.Context, opts Options) (Result, error)", "file": "internal/optimize/run.go"}},
					},
					Edges: []propose.EdgeConstraint{
						{From: "CLIAdapter", To: "OptimizeOrchestration", Kind: mgraph.EdgeDependsOn},
					},
				},
			},
		}},
	}

	c, err := targetcontract.BuildFromOptimizeEnvelope(env, "")
	if err != nil {
		t.Fatalf("BuildFromOptimizeEnvelope: %v", err)
	}
	if c.Source.ModulePath != "example.com/app" {
		t.Fatalf("module = %q, want example.com/app", c.Source.ModulePath)
	}
	if len(c.Packages) != 2 {
		t.Fatalf("packages = %d, want 2", len(c.Packages))
	}
	if len(c.PublicTypes) != 2 {
		t.Fatalf("public types = %d, want 2", len(c.PublicTypes))
	}
	if len(c.PublicFunctions) != 1 || c.PublicFunctions[0].Name != "Run" {
		t.Fatalf("public functions = %+v, want Run", c.PublicFunctions)
	}
	if len(c.ExpectedEdges) != 1 {
		t.Fatalf("expected edges = %d, want 1", len(c.ExpectedEdges))
	}
}

func TestScaffoldAndVerifyTargetContract(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	c := targetcontract.Contract{
		Version: 1,
		ID:      "target-test",
		Source:  targetcontract.SourceSpec{ModulePath: "example.com/app"},
		Packages: []targetcontract.PackageSpec{{
			Role:       "OptimizeOrchestration",
			ImportPath: "example.com/app/internal/optimize",
			Dir:        "internal/optimize",
			Name:       "optimize",
			Action:     "create",
		}},
		Files: []targetcontract.FileSpec{{
			Path:        "internal/optimize/run.go",
			PackageRole: "OptimizeOrchestration",
			PackageName: "optimize",
			Action:      "create",
		}},
		PublicTypes: []targetcontract.TypeSpec{
			{Name: "Options", Kind: "struct", PackageRole: "OptimizeOrchestration", PackagePath: "example.com/app/internal/optimize", File: "internal/optimize/run.go"},
			{Name: "Result", Kind: "struct", PackageRole: "OptimizeOrchestration", PackagePath: "example.com/app/internal/optimize", File: "internal/optimize/run.go"},
		},
		PublicFunctions: []targetcontract.FunctionSpec{{
			Name:        "Run",
			PackageRole: "OptimizeOrchestration",
			PackagePath: "example.com/app/internal/optimize",
			File:        "internal/optimize/run.go",
			Signature:   "func Run(ctx context.Context, opts Options) (Result, error)",
		}},
	}

	scaffold, err := targetcontract.Scaffold(c, dir, false)
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if len(scaffold.Created) != 1 {
		t.Fatalf("created = %+v, want one file", scaffold.Created)
	}
	res, err := targetcontract.Verify(context.Background(), c, dir, "./...", false)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.Match {
		t.Fatalf("Verify mismatch: %+v", res)
	}
}
