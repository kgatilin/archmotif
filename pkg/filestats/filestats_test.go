package filestats

import (
	"testing"

	archmotifimport "github.com/kgatilin/archmotif/pkg/archmotifimport"
)

// build a graph: package p with a fat file (25 types) and two small files.
func buildGraph(t *testing.T) *Graph {
	t.Helper()
	b := archmotifimport.NewBuilder()
	if err := b.AddPackage("pkg:p", "", ""); err != nil {
		t.Fatal(err)
	}
	addFile := func(file string, nTypes int) {
		fid := "file:p/" + file
		if err := b.AddFile(fid, "pkg:p"); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < nTypes; i++ {
			tid := "type:p." + file + "_" + string(rune('A'+i))
			if err := b.AddType(tid, "pkg:p", false, ""); err != nil {
				t.Fatal(err)
			}
			if err := b.AddContains(fid, tid); err != nil {
				t.Fatal(err)
			}
		}
	}
	addFile("fat.go", 25)
	addFile("a.go", 3)
	addFile("b.go", 2)
	g, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestAnalyze_FlagsOverloadedFile(t *testing.T) {
	g := buildGraph(t)
	r := Analyze(g, Options{})

	if r.FileCount != 3 {
		t.Fatalf("file_count = %d, want 3", r.FileCount)
	}
	if r.MaxSymbols != 25 {
		t.Errorf("max = %d, want 25", r.MaxSymbols)
	}
	if r.MedianSymbols != 3 {
		t.Errorf("median = %v, want 3", r.MedianSymbols)
	}
	// Files sorted desc: fat.go first.
	if r.Files[0].File != "file:p/fat.go" || r.Files[0].SymbolCount != 25 {
		t.Errorf("top file = %+v, want fat.go/25", r.Files[0])
	}
	if !r.Files[0].Outlier {
		t.Error("fat.go should be flagged as outlier")
	}
	if r.OutlierCount != 1 {
		t.Errorf("outlier_count = %d, want 1", r.OutlierCount)
	}
	for _, f := range r.Files[1:] {
		if f.Outlier {
			t.Errorf("%s wrongly flagged as outlier", f.File)
		}
	}
}

func TestAnalyze_NodeIDScope(t *testing.T) {
	g := buildGraph(t)
	// Restrict to the two small files only.
	r := Analyze(g, Options{NodeIDs: []string{"file:p/a.go", "file:p/b.go"}})
	if r.FileCount != 2 {
		t.Fatalf("file_count = %d, want 2", r.FileCount)
	}
	if r.MaxSymbols != 3 {
		t.Errorf("max = %d, want 3 (fat.go excluded)", r.MaxSymbols)
	}
	if r.OutlierCount != 0 {
		t.Errorf("outlier_count = %d, want 0", r.OutlierCount)
	}
}
