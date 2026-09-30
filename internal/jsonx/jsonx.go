// Package jsonx is a small order-preserving JSON model. Config files such as
// opencode.json are user-owned, so merges must keep key order and number text.
package jsonx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Object is an insertion-ordered JSON object.
type Object struct {
	Keys   []string
	Values map[string]any
}

func NewObject() *Object { return &Object{Values: map[string]any{}} }

func (o *Object) Get(k string) (any, bool) { v, ok := o.Values[k]; return v, ok }

func (o *Object) Set(k string, v any) {
	if _, ok := o.Values[k]; !ok {
		o.Keys = append(o.Keys, k)
	}
	o.Values[k] = v
}

func (o *Object) Delete(k string) {
	if _, ok := o.Values[k]; !ok {
		return
	}
	delete(o.Values, k)
	for i, key := range o.Keys {
		if key == k {
			o.Keys = append(o.Keys[:i], o.Keys[i+1:]...)
			break
		}
	}
}

// Parse decodes JSON into *Object, []any, string, json.Number, bool, or nil.
func Parse(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected trailing data")
	}
	return v, nil
}

func parseValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := NewObject()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := kt.(string)
				val, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				obj.Set(key, val)
			}
			_, err := dec.Token()
			return obj, err
		case '[':
			arr := []any{}
			for dec.More() {
				val, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			_, err := dec.Token()
			return arr, err
		}
		return nil, fmt.Errorf("unexpected delimiter %v", t)
	default:
		return tok, nil
	}
}

// ReadObjectFile parses a file that must contain a JSON object.
func ReadObjectFile(path string) (*Object, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	v, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	obj, ok := v.(*Object)
	if !ok {
		return nil, fmt.Errorf("expected JSON object at %s", path)
	}
	return obj, nil
}

// Marshal renders like JSON.stringify(value, null, 2).
func Marshal(v any) []byte {
	var b bytes.Buffer
	write(&b, v, 0)
	return b.Bytes()
}

func write(b *bytes.Buffer, v any, depth int) {
	indent := strings.Repeat("  ", depth+1)
	closing := strings.Repeat("  ", depth)
	switch t := v.(type) {
	case *Object:
		if len(t.Keys) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, k := range t.Keys {
			b.WriteString(indent)
			writeString(b, k)
			b.WriteString(": ")
			write(b, t.Values[k], depth+1)
			if i < len(t.Keys)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(closing + "}")
	case []any:
		if len(t) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, e := range t {
			b.WriteString(indent)
			write(b, e, depth+1)
			if i < len(t)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(closing + "]")
	case string:
		writeString(b, t)
	case json.Number:
		b.WriteString(t.String())
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case nil:
		b.WriteString("null")
	case int:
		b.WriteString(strconv.Itoa(t))
	case float64:
		b.WriteString(strconv.FormatFloat(t, 'g', -1, 64))
	default:
		enc, _ := json.Marshal(t)
		b.Write(enc)
	}
}

// writeString quotes like JSON.stringify: only '"', '\\' and control
// characters are escaped; U+2028/U+2029 and HTML characters stay raw.
func writeString(b *bytes.Buffer, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if c < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0xf])
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
}

// Clone deep-copies a parsed value.
func Clone(v any) any {
	switch t := v.(type) {
	case *Object:
		o := NewObject()
		for _, k := range t.Keys {
			o.Set(k, Clone(t.Values[k]))
		}
		return o
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = Clone(e)
		}
		return out
	}
	return v
}

// Equal compares two parsed values structurally (object key order ignored).
func Equal(a, b any) bool {
	return string(canonical(a)) == string(canonical(b))
}

func canonical(v any) []byte {
	switch t := v.(type) {
	case *Object:
		m := map[string]json.RawMessage{}
		for _, k := range t.Keys {
			m[k] = canonical(t.Values[k])
		}
		out, _ := json.Marshal(m)
		return out
	case []any:
		parts := make([]json.RawMessage, len(t))
		for i, e := range t {
			parts[i] = canonical(e)
		}
		out, _ := json.Marshal(parts)
		return out
	}
	out, _ := json.Marshal(v)
	return out
}

// DeepMerge ports deepMergeJson: objects merge recursively, anything else is
// replaced by the incoming value.
func DeepMerge(existing, incoming any) any {
	eo, eok := existing.(*Object)
	io, iok := incoming.(*Object)
	if eok && iok {
		result := Clone(eo).(*Object)
		for _, k := range io.Keys {
			if cur, ok := result.Values[k]; ok {
				result.Set(k, DeepMerge(cur, io.Values[k]))
			} else {
				result.Set(k, Clone(io.Values[k]))
			}
		}
		return result
	}
	return Clone(incoming)
}

// FillMissing adds keys from incoming that are absent in existing, recursing
// into objects and never replacing existing values. Returns conflicting paths.
func FillMissing(existing, incoming *Object, prefix string) []string {
	var conflicts []string
	for _, k := range incoming.Keys {
		iv := incoming.Values[k]
		ev, ok := existing.Values[k]
		if !ok {
			existing.Set(k, Clone(iv))
			continue
		}
		eo, eok := ev.(*Object)
		io, iok := iv.(*Object)
		if eok && iok {
			conflicts = append(conflicts, FillMissing(eo, io, prefix+"/"+k)...)
			continue
		}
		if !Equal(ev, iv) {
			conflicts = append(conflicts, prefix+"/"+k)
		}
	}
	return conflicts
}

func pointerSegments(pointer string) ([]string, error) {
	p := strings.TrimSpace(pointer)
	if p == "" {
		return nil, fmt.Errorf("jsonPointers entries must not be empty")
	}
	if p == "/" {
		return nil, nil
	}
	if !strings.HasPrefix(p, "/") {
		return nil, fmt.Errorf("jsonPointers entries must start with '/'. Received: %s", pointer)
	}
	segs := strings.Split(p[1:], "/")
	for i, s := range segs {
		segs[i] = strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
	}
	return segs, nil
}

// GetPointer resolves an RFC 6901 pointer.
func GetPointer(root any, pointer string) (any, error) {
	segs, err := pointerSegments(pointer)
	if err != nil {
		return nil, err
	}
	cur := root
	for _, s := range segs {
		switch t := cur.(type) {
		case *Object:
			v, ok := t.Values[s]
			if !ok {
				return nil, fmt.Errorf("JSON pointer %s is missing from the source fragment", pointer)
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(s)
			if err != nil || i < 0 || i >= len(t) {
				return nil, fmt.Errorf("JSON pointer %s is missing from the source fragment", pointer)
			}
			cur = t[i]
		default:
			return nil, fmt.Errorf("JSON pointer %s does not resolve inside the source fragment", pointer)
		}
	}
	return cur, nil
}

// MergePointer ports mergeJsonPointerValue, including the /permissions
// rule-list special case (pack rules go first so workspace rules still win).
func MergePointer(root *Object, pointer string, value any) (*Object, error) {
	segs, err := pointerSegments(pointer)
	if err != nil {
		return nil, err
	}
	if len(segs) == 0 {
		merged, ok := DeepMerge(root, value).(*Object)
		if !ok {
			return nil, fmt.Errorf("merging at pointer %s must produce a JSON object root", pointer)
		}
		return merged, nil
	}
	next := Clone(root).(*Object)
	cur := next
	for _, s := range segs[:len(segs)-1] {
		child, ok := cur.Values[s].(*Object)
		if !ok {
			child = NewObject()
			cur.Set(s, child)
		}
		cur = child
	}
	last := segs[len(segs)-1]
	existing, has := cur.Values[last]
	earr, eIsArr := existing.([]any)
	sarr, sIsArr := value.([]any)
	if strings.TrimSpace(pointer) == "/permissions" && has && eIsArr && sIsArr {
		var additions []any
		for _, rule := range sarr {
			dup := false
			for _, e := range earr {
				if Equal(e, rule) {
					dup = true
					break
				}
			}
			if !dup {
				additions = append(additions, Clone(rule))
			}
		}
		cur.Set(last, append(additions, earr...))
	} else {
		cur.Set(last, DeepMerge(existing, value))
	}
	return next, nil
}

// MergeFragmentBytes merges pointers from a source fragment into destination
// bytes (nil = missing file) and returns the rendered result.
func MergeFragmentBytes(source []byte, dest []byte, pointers []string) ([]byte, error) {
	sv, err := Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse fragment: %w", err)
	}
	src, ok := sv.(*Object)
	if !ok {
		return nil, fmt.Errorf("fragment must be a JSON object")
	}
	merged := NewObject()
	if dest != nil {
		dv, err := Parse(dest)
		if err != nil {
			return nil, fmt.Errorf("parse destination: %w", err)
		}
		if merged, ok = dv.(*Object); !ok {
			return nil, fmt.Errorf("destination must be a JSON object")
		}
	}
	if len(pointers) == 0 {
		pointers = []string{"/"}
	}
	for _, p := range pointers {
		v, err := GetPointer(src, p)
		if err != nil {
			return nil, err
		}
		if merged, err = MergePointer(merged, p, v); err != nil {
			return nil, err
		}
	}
	return append(Marshal(merged), '\n'), nil
}

// ValueSha256 hashes a JSON value independent of key order and formatting.
func ValueSha256(v any) string {
	sum := sha256.Sum256(canonical(v))
	return hex.EncodeToString(sum[:])
}

// PointerHashes returns ValueSha256 of each pointer whose value in dest is
// exactly the fragment's, i.e. owned wholly by the pack rather than merged
// into values the user already had.
func PointerHashes(fragment, dest []byte, pointers []string) map[string]string {
	root, err := Parse(dest)
	if err != nil {
		return nil
	}
	src, err := Parse(fragment)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, p := range pointers {
		if segs, err := pointerSegments(p); err != nil || len(segs) == 0 {
			continue
		}
		v, err := GetPointer(root, p)
		if err != nil {
			continue
		}
		if w, err := GetPointer(src, p); err == nil && Equal(v, w) {
			out[p] = ValueSha256(v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// UnmergeFragmentBytes backs a merged fragment out of destination bytes: each
// pointer is deleted when it still equals the fragment value (source nil =
// fragment unavailable) or its installed hash, or regardless when force is
// set. /permissions rule lists lose only the pack's rules. Objects emptied by
// a removal are dropped.
func UnmergeFragmentBytes(source, dest []byte, pointers []string, installed map[string]string, force bool) ([]byte, error) {
	dv, err := Parse(dest)
	if err != nil {
		return nil, fmt.Errorf("parse destination: %w", err)
	}
	root, ok := dv.(*Object)
	if !ok {
		return nil, fmt.Errorf("destination must be a JSON object")
	}
	var src any
	if source != nil {
		if src, err = Parse(source); err != nil {
			return nil, fmt.Errorf("parse fragment: %w", err)
		}
	}
	for _, p := range pointers {
		segs, err := pointerSegments(p)
		if err != nil {
			return nil, err
		}
		if len(segs) == 0 {
			return nil, fmt.Errorf("fragment was merged at the document root; remove its keys by hand")
		}
		chain := []*Object{root}
		for _, s := range segs[:len(segs)-1] {
			next, ok := chain[len(chain)-1].Values[s].(*Object)
			if !ok {
				chain = nil
				break
			}
			chain = append(chain, next)
		}
		if chain == nil {
			continue
		}
		parent, last := chain[len(chain)-1], segs[len(segs)-1]
		cur, has := parent.Values[last]
		if !has {
			continue
		}
		var want any
		haveWant := false
		if src != nil {
			if want, err = GetPointer(src, p); err == nil {
				haveWant = true
			}
		}
		carr, cIsArr := cur.([]any)
		warr, wIsArr := want.([]any)
		isPerms := strings.TrimSpace(p) == "/permissions"
		unchanged := installed[p] != "" && installed[p] == ValueSha256(cur) && !isPerms
		switch {
		case isPerms && haveWant && cIsArr && wIsArr:
			kept := []any{}
			for _, rule := range carr {
				pack := false
				for _, w := range warr {
					if Equal(rule, w) {
						pack = true
						break
					}
				}
				if !pack {
					kept = append(kept, rule)
				}
			}
			if len(kept) > 0 {
				parent.Set(last, kept)
				continue
			}
			parent.Delete(last)
		case force || unchanged || (haveWant && Equal(cur, want)):
			parent.Delete(last)
		case !haveWant:
			return nil, fmt.Errorf("%s is not in the pack's current fragment (missing or changed since install), so local edits cannot be ruled out", p)
		default:
			return nil, fmt.Errorf("%s was edited since install", p)
		}
		for i := len(chain) - 1; i > 0 && len(chain[i].Keys) == 0; i-- {
			chain[i-1].Delete(segs[i-1])
		}
	}
	return append(Marshal(root), '\n'), nil
}

// WriteFileAtomic writes via temp file + rename.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	os.Chmod(tmp.Name(), perm)
	return os.Rename(tmp.Name(), path)
}

// WriteJSONAtomic writes JSON.stringify(v, null, 2) + "\n".
func WriteJSONAtomic(path string, v any) error {
	return WriteFileAtomic(path, append(Marshal(v), '\n'), 0o644)
}

// FromGo converts a plain Go value (via encoding/json) into the ordered model,
// preserving struct field order.
func FromGo(v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	out, err := Parse(data)
	if err != nil {
		panic(err)
	}
	return out
}
