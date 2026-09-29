package hashx

import (
	"os"
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

// TestPathGolden pins the file and directory hash format. Changing it would
// make every recorded installedSha256 look locally modified.
func TestPathGolden(t *testing.T) {
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
	for p, want := range map[string]string{
		root:                             "e7d9d3ef54b84f7a0fccb29e82c27aa0f29c3c041581a2a27a17f9a9e7e506fe",
		filepath.Join(root, "README.md"): "454349e422f05297191ead13e21d3db520e5abef52055e4964b82fb213f593a1",
	} {
		got, err := Path(p)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: got %s, want %s", p, got, want)
		}
	}
}
