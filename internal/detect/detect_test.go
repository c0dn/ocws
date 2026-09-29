package detect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/c0dn/ocws/internal/registry"
)

func TestProfileDetectionAndFallback(t *testing.T) {
	reg, err := registry.Load("../../testdata/templates")
	if err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	if d := Profiles(Scan(ws), reg); d.Best != "blank" {
		t.Errorf("empty dir: %+v", d)
	}
	os.WriteFile(filepath.Join(ws, "go.mod"), nil, 0o644)
	os.MkdirAll(filepath.Join(ws, "node_modules/x"), 0o755)
	os.WriteFile(filepath.Join(ws, "node_modules/x/package.json"), nil, 0o644)
	d := Profiles(Scan(ws), reg)
	if d.Best != "dev" || d.Scores[0].Score != 1 {
		t.Errorf("go repo: %+v (node_modules must be skipped)", d)
	}
	os.MkdirAll(filepath.Join(ws, "nb"), 0o755)
	os.WriteFile(filepath.Join(ws, "nb/a.ipynb"), nil, 0o644)
	p, _ := reg.Profile("dev")
	if rec := Capabilities(Scan(ws), p, nil); rec["docs-mcp"] == "" {
		t.Errorf("recommendation missing: %v", rec)
	}
	os.MkdirAll(filepath.Join(ws, ".claude"), 0o755)
	if h := Harnesses(ws); len(h) != 1 || h[0] != "claude" {
		t.Errorf("harnesses = %v", h)
	}
}
