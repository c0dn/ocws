// Package hashx ports the setup-manifest hashing from the original TypeScript
// engine byte-for-byte, so manifests written by either implementation agree.
package hashx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// File returns the hex sha256 of a regular file's bytes.
func File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Bytes returns the hex sha256 of an in-memory buffer.
func Bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Path hashes a file, or a directory tree using the TS directory format.
func Path(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return Dir(path)
	}
	return File(path)
}

// Dir reproduces sha256Directory from the TS engine, including its
// localeCompare entry ordering.
func Dir(root string) (string, error) {
	h := sha256.New()
	io.WriteString(h, "directory\n")
	if err := visit(h, root, ""); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func visit(h io.Writer, current, prefix string) error {
	entries, err := os.ReadDir(current)
	if err != nil {
		return err
	}
	sort.SliceStable(entries, func(i, j int) bool { return LocaleCompare(entries[i].Name(), entries[j].Name()) < 0 })
	for _, e := range entries {
		full := filepath.Join(current, e.Name())
		rel := e.Name()
		if prefix != "" {
			rel = prefix + "/" + e.Name()
		}
		typ := e.Type()
		switch {
		case typ.IsDir():
			fmt.Fprintf(h, "dir %s\n", rel)
			if err := visit(h, full, rel); err != nil {
				return err
			}
		case typ.IsRegular():
			sum, err := File(full)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "file %s\x00%s\n", rel, sum)
		case typ&os.ModeSymlink != 0:
			target, err := os.Readlink(full)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "symlink %s\x00%s\n", rel, target)
		default:
			info, err := os.Lstat(full)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "other %s\x00%d\x00%d\n", rel, nodeMode(info), info.Size())
		}
	}
	return nil
}

// nodeMode approximates Node's stat.mode (type bits | permission bits).
func nodeMode(info os.FileInfo) uint32 {
	m := info.Mode()
	perm := uint32(m.Perm())
	switch {
	case m&os.ModeNamedPipe != 0:
		return 0o010000 | perm
	case m&os.ModeSocket != 0:
		return 0o140000 | perm
	case m&os.ModeCharDevice != 0:
		return 0o020000 | perm
	case m&os.ModeDevice != 0:
		return 0o060000 | perm
	}
	return perm
}

// ICU root collation order for printable ASCII, generated from Node's
// String.prototype.localeCompare. Letters share a primary weight per case pair.
const asciiOrder = " _-,;:!?.'\"()[]{}@*/\\&#%`^+<=>|~$0123456789aAbBcCdDeEfFgGhHiIjJkKlLmMnNoOpPqQrRsStTuUvVwWxXyYzZ"

var primary [128]int

func init() {
	for i := range primary {
		primary[i] = -1
	}
	rank := 0
	for i := 0; i < len(asciiOrder); i++ {
		c := asciiOrder[i]
		isUpper := c >= 'A' && c <= 'Z'
		if i > 0 && !isUpper {
			rank++
		}
		primary[c] = rank
	}
}

func weight(r rune) int {
	if r < 128 && primary[r] >= 0 {
		return primary[r]
	}
	// Non-ASCII and control characters: sort after ASCII by code point. This
	// is an approximation of ICU; template names are expected to be ASCII.
	return 1000 + int(r)
}

// LocaleCompare approximates JS localeCompare (ICU root, tertiary strength)
// for ASCII names: primary weights first, then lowercase-before-uppercase.
func LocaleCompare(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	n := min(len(ra), len(rb))
	for i := 0; i < n; i++ {
		wa, wb := weight(ra[i]), weight(rb[i])
		if wa != wb {
			return cmp(wa, wb)
		}
	}
	if len(ra) != len(rb) {
		return cmp(len(ra), len(rb))
	}
	for i := 0; i < n; i++ {
		ua, ub := isUpper(ra[i]), isUpper(rb[i])
		if ua != ub {
			if ua {
				return 1
			}
			return -1
		}
	}
	return cmp(int(0), int(0))
}

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }

func cmp(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
