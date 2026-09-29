package hashx

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestLocaleCompareMatchesNode(t *testing.T) {
	in := []string{"a-b", "a_b", "ab", "a.b", "aB", "Ab", "ab1", "a1", "a10", "a2", "README.md", "exploit.py", "_x", "-x"}
	want := []string{"_x", "-x", "a_b", "a-b", "a.b", "a1", "a10", "a2", "ab", "aB", "Ab", "ab1", "exploit.py", "README.md"}
	sort.SliceStable(in, func(i, j int) bool { return LocaleCompare(in[i], in[j]) < 0 })
	if strings.Join(in, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v\nwant %v", in, want)
	}
}

func TestDirMatchesTypeScriptReference(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun not installed")
	}
	root := t.TempDir()
	files := map[string]string{
		"README.md": "r", "exploit.py": "e", "_private/x.txt": "x", "a-b/c": "c", "a_b/c": "c2",
		"Zeta/A.md": "z", "zeta2/b.md": "b", "scripts/run.sh": "#!/bin/sh\n", "a.b": "dot",
	}
	for p, c := range files {
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(c), 0o644)
	}
	os.Symlink("README.md", filepath.Join(root, "link"))
	targets := []string{root, filepath.Join(root, "README.md")}
	if extra := os.Getenv("OCWS_HASH_EXTRA"); extra != "" {
		targets = append(targets, strings.Split(extra, ":")...)
	}
	out, err := exec.Command(bun, append([]string{"run", "testdata/hash_ref.ts"}, targets...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Fields(string(out))
	for i, p := range targets {
		got, err := Path(p)
		if err != nil {
			t.Fatal(err)
		}
		if got != want[i] {
			t.Errorf("%s: go=%s ts=%s", p, got, want[i])
		}
	}
}
