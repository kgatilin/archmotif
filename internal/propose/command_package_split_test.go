package propose_test

import (
	"fmt"
	"strings"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/propose"
)

func TestCommandPackageSplitRule_TriggersOnOversizedCmdPackage(t *testing.T) {
	g, rec := commandPackageFixture("pkg:example.com/tool/cmd/tool", 24)
	p := propose.NewProposerWith(propose.CommandPackageSplitRule{})

	res := p.ProposeFromRecords(g, []metrics.Record{rec})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected proposer errors: %+v", res.Errors)
	}
	if len(res.Proposals) != 1 {
		t.Fatalf("proposals = %d, want 1", len(res.Proposals))
	}
	prop := res.Proposals[0]
	if got := prop.ID; !strings.HasPrefix(got, "command_package_split-") {
		t.Fatalf("proposal ID = %q, want command_package_split-*", got)
	}
	if len(prop.TargetSubgraph.Roles) != 5 {
		t.Fatalf("roles = %d, want 5", len(prop.TargetSubgraph.Roles))
	}
	if len(prop.TargetSubgraph.Edges) != 3 {
		t.Fatalf("edges = %d, want 3", len(prop.TargetSubgraph.Edges))
	}
	if prop.TargetSubgraph.Edges[0].Kind != mgraph.EdgeDependsOn {
		t.Fatalf("edge kind = %q, want dependsOn", prop.TargetSubgraph.Edges[0].Kind)
	}
	if !hasRole(prop.TargetSubgraph.Roles, "OptimizeRun", mgraph.NodeFunction) {
		t.Fatalf("missing OptimizeRun function role in %+v", prop.TargetSubgraph.Roles)
	}
	if len(prop.AffectedFiles) == 0 {
		t.Fatal("AffectedFiles is empty")
	}
}

func TestCommandPackageSplitRule_IgnoresNonCommandPackage(t *testing.T) {
	g, rec := commandPackageFixture("pkg:example.com/tool/internal/service", 24)
	p := propose.NewProposerWith(propose.CommandPackageSplitRule{})

	res := p.ProposeFromRecords(g, []metrics.Record{rec})
	if len(res.Proposals) != 0 {
		t.Fatalf("proposals = %d, want 0", len(res.Proposals))
	}
}

func commandPackageFixture(pkgID string, members int) (*mgraph.Graph, metrics.Record) {
	g := mgraph.New()
	g.AddNode(mgraph.Node{
		ID:    pkgID,
		Kind:  mgraph.NodePackage,
		Name:  "tool",
		QName: trimPkgPrefix(pkgID),
	})
	memberIDs := []string{pkgID}
	for i := 0; i < members; i++ {
		fileID := fmt.Sprintf("%s:file:%03d", pkgID, i)
		funcID := fmt.Sprintf("cmd/tool/file%d.go:%d:1:function:F%d", i, i+1, i)
		g.AddNode(mgraph.Node{
			ID:   fileID,
			Kind: mgraph.NodeFile,
			Name: fmt.Sprintf("file%d.go", i),
			Pos:  mgraph.Position{File: fmt.Sprintf("cmd/tool/file%d.go", i), Line: 1, Col: 1},
		})
		g.AddNode(mgraph.Node{
			ID:   funcID,
			Kind: mgraph.NodeFunction,
			Name: fmt.Sprintf("F%d", i),
			Pos:  mgraph.Position{File: fmt.Sprintf("cmd/tool/file%d.go", i), Line: i + 1, Col: 1},
		})
		_, _ = g.AddEdge(mgraph.Edge{From: pkgID, To: fileID, Kind: mgraph.EdgeContains})
		_, _ = g.AddEdge(mgraph.Edge{From: fileID, To: funcID, Kind: mgraph.EdgeContains})
		memberIDs = append(memberIDs, fileID, funcID)
	}
	return g, metrics.Record{
		Metric: "modularity",
		Scope:  metrics.ScopeRegion,
		Target: pkgID,
		Value:  float64(len(memberIDs)),
		Details: map[string]any{
			"members": memberIDs,
		},
	}
}

func trimPkgPrefix(id string) string {
	if len(id) >= 4 && id[:4] == "pkg:" {
		return id[4:]
	}
	return id
}

func hasRole(roles []propose.Role, name string, kind mgraph.NodeKind) bool {
	for _, role := range roles {
		if role.Name == name && role.Kind == kind {
			return true
		}
	}
	return false
}
