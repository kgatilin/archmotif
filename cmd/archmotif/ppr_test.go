package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const pprTestGraph = `{"version":1,
 "nodes":[{"id":"a"},{"id":"b"},{"id":"c"},{"id":"d"},{"id":"e"}],
 "edges":[{"from":"a","to":"b"},{"from":"b","to":"c"},{"from":"c","to":"a"},
          {"from":"d","to":"a"},{"from":"a","to":"e"}]}`

func writePPRGraph(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "g.json")
	if err := os.WriteFile(path, []byte(pprTestGraph), 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	return path
}

func runPPRJSON(t *testing.T, args ...string) pprReport {
	t.Helper()
	var out, errBuf bytes.Buffer
	if code := runPPR(args, &out, &errBuf); code != 0 {
		t.Fatalf("runPPR %v: exit %d, stderr=%s", args, code, errBuf.String())
	}
	var rep pprReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("decode report: %v\noutput: %s", err, out.String())
	}
	return rep
}

func TestRunPPRSeededRanking(t *testing.T) {
	path := writePPRGraph(t)
	rep := runPPRJSON(t, "--seeds", "a", path)

	if rep.N != 5 {
		t.Errorf("n = %d, want 5", rep.N)
	}
	if len(rep.Ranking) != 5 {
		t.Fatalf("ranking len = %d, want 5", len(rep.Ranking))
	}
	if rep.Ranking[0].Name != "a" {
		t.Errorf("top = %q, want \"a\"", rep.Ranking[0].Name)
	}
	if last := rep.Ranking[len(rep.Ranking)-1]; last.Name != "d" || last.Score > 1e-9 {
		t.Errorf("bottom = %+v, want d≈0", last)
	}
}

func TestRunPPRTopTruncates(t *testing.T) {
	path := writePPRGraph(t)
	rep := runPPRJSON(t, "--seeds", "a", "--top", "2", path)
	if len(rep.Ranking) != 2 {
		t.Errorf("ranking len = %d, want 2 (top)", len(rep.Ranking))
	}
}

func TestRunPPRUnknownSeedsReported(t *testing.T) {
	path := writePPRGraph(t)
	rep := runPPRJSON(t, "--seeds", "a,ghost", path)
	if len(rep.UnknownSeeds) != 1 || rep.UnknownSeeds[0] != "ghost" {
		t.Errorf("unknown_seeds = %v, want [ghost]", rep.UnknownSeeds)
	}
}

func TestRunPPRBadRestartIsArgError(t *testing.T) {
	path := writePPRGraph(t)
	var out, errBuf bytes.Buffer
	if code := runPPR([]string{"--restart", "1.5", path}, &out, &errBuf); code != 2 {
		t.Errorf("restart=1.5: exit %d, want 2", code)
	}
}

func TestRunPPRMissingArgIsUsageError(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := runPPR(nil, &out, &errBuf); code != 2 {
		t.Errorf("no args: exit %d, want 2", code)
	}
}
