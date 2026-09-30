package harness

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/c0dn/ocws/internal/jsonx"
	"github.com/pelletier/go-toml/v2"
)

// MCPStyle describes how a harness spells one MCP server in JSON.
type MCPStyle struct {
	// Key is the top-level object holding servers, e.g. "mcpServers".
	Key string
	// LocalType / RemoteType set a "type" field ("" = omit).
	LocalType, RemoteType string
	// URLKey names the remote URL field ("url" or "httpUrl").
	URLKey string
	// EnvFormat renders an env reference, e.g. "${%s}".
	EnvFormat string
	// DisabledKey carries a disabled server ("" = disabled servers are skipped).
	DisabledKey string
}

var envRef = regexp.MustCompile(`\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

// OpenCodeServers extracts MCP server definitions from an OpenCode fragment,
// V2 (mcp.servers.x) or V1 (mcp.x) shaped, sorted by name.
func OpenCodeServers(fragment []byte) ([]string, map[string]*jsonx.Object, error) {
	v, err := jsonx.Parse(fragment)
	if err != nil {
		return nil, nil, fmt.Errorf("parse MCP fragment: %w", err)
	}
	root, ok := v.(*jsonx.Object)
	if !ok {
		return nil, nil, fmt.Errorf("MCP fragment must be a JSON object")
	}
	mcp, _ := root.Values["mcp"].(*jsonx.Object)
	if mcp == nil {
		return nil, nil, nil
	}
	if s, ok := mcp.Values["servers"].(*jsonx.Object); ok {
		mcp = s
	}
	out := map[string]*jsonx.Object{}
	var names []string
	for _, k := range mcp.Keys {
		if def, ok := mcp.Values[k].(*jsonx.Object); ok {
			out[k] = def
			names = append(names, k)
		}
	}
	sort.Strings(names)
	return names, out, nil
}

func isDisabled(def *jsonx.Object) bool {
	if b, ok := def.Values["disabled"].(bool); ok && b {
		return true
	}
	b, ok := def.Values["enabled"].(bool)
	return ok && !b
}

func isRemote(def *jsonx.Object) bool {
	t, _ := def.Values["type"].(string)
	_, hasURL := def.Values["url"]
	return t == "remote" || hasURL
}

// JSONServer translates one OpenCode server definition for a JSON style.
func (s MCPStyle) JSONServer(def *jsonx.Object) (*jsonx.Object, []string) {
	var warns []string
	sub := func(v any) string {
		return envRef.ReplaceAllStringFunc(fmt.Sprint(v), func(m string) string {
			return fmt.Sprintf(s.EnvFormat, envRef.FindStringSubmatch(m)[1])
		})
	}
	out := jsonx.NewObject()
	strMap := func(k, dest string) {
		if o, ok := def.Values[k].(*jsonx.Object); ok && len(o.Keys) > 0 {
			m := jsonx.NewObject()
			for _, kk := range o.Keys {
				m.Set(kk, sub(o.Values[kk]))
			}
			out.Set(dest, m)
		}
	}
	if isRemote(def) {
		if s.RemoteType != "" {
			out.Set("type", s.RemoteType)
		}
		out.Set(orStr(s.URLKey, "url"), sub(def.Values["url"]))
		strMap("headers", "headers")
		if _, ok := def.Values["oauth"]; ok {
			warns = append(warns, "OAuth settings are not translated; the harness will prompt or use its own auth flow")
		}
	} else {
		if s.LocalType != "" {
			out.Set("type", s.LocalType)
		}
		if cmd, ok := def.Values["command"].([]any); ok && len(cmd) > 0 {
			out.Set("command", sub(cmd[0]))
			var args []any
			for _, a := range cmd[1:] {
				args = append(args, sub(a))
			}
			if len(args) > 0 {
				out.Set("args", args)
			}
		}
		strMap("environment", "env")
	}
	if isDisabled(def) && s.DisabledKey != "" {
		out.Set(s.DisabledKey, true)
	}
	return out, warns
}

func orStr(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// JSONFragment renders an OpenCode MCP fragment for a JSON style and returns
// the JSON pointers it defines.
func (s MCPStyle) JSONFragment(fragment []byte) ([]byte, []string, []string, error) {
	names, defs, err := OpenCodeServers(fragment)
	if err != nil {
		return nil, nil, nil, err
	}
	var warns, ptrs []string
	servers := jsonx.NewObject()
	for _, n := range names {
		if isDisabled(defs[n]) && s.DisabledKey == "" {
			warns = append(warns, fmt.Sprintf("MCP server %s is disabled in the template and this harness has no disabled flag; skipped", n))
			continue
		}
		srv, w := s.JSONServer(defs[n])
		warns = append(warns, w...)
		servers.Set(n, srv)
		ptrs = append(ptrs, "/"+escapePointer(s.Key)+"/"+escapePointer(n))
	}
	root := jsonx.NewObject()
	root.Set(s.Key, servers)
	return append(jsonx.Marshal(root), '\n'), ptrs, warns, nil
}

func escapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// CodexServer translates one OpenCode server definition to a Codex
// [mcp_servers.x] table.
func CodexServer(def *jsonx.Object) (map[string]any, []string) {
	var warns []string
	out := map[string]any{}
	if isDisabled(def) {
		out["enabled"] = false
	}
	str := func(k string) string { s, _ := def.Values[k].(string); return s }
	if isRemote(def) {
		out["url"] = str("url")
		if hdrs, ok := def.Values["headers"].(*jsonx.Object); ok {
			lit, envH := map[string]any{}, map[string]any{}
			for _, k := range hdrs.Keys {
				v := fmt.Sprint(hdrs.Values[k])
				if m := envRef.FindStringSubmatch(v); m != nil {
					if strings.EqualFold(k, "Authorization") && strings.HasPrefix(v, "Bearer ") {
						out["bearer_token_env_var"] = m[1]
					} else {
						envH[k] = m[1]
					}
					continue
				}
				lit[k] = v
			}
			if len(lit) > 0 {
				out["http_headers"] = lit
			}
			if len(envH) > 0 {
				out["env_http_headers"] = envH
			}
		}
		return out, warns
	}
	if cmd, ok := def.Values["command"].([]any); ok && len(cmd) > 0 {
		out["command"] = fmt.Sprint(cmd[0])
		var args []string
		for _, a := range cmd[1:] {
			args = append(args, fmt.Sprint(a))
		}
		if len(args) > 0 {
			out["args"] = args
		}
	}
	if env, ok := def.Values["environment"].(*jsonx.Object); ok {
		lit := map[string]any{}
		var pass []string
		for _, k := range env.Keys {
			v := fmt.Sprint(env.Values[k])
			if m := envRef.FindStringSubmatch(v); m != nil {
				if m[0] != v || m[1] != k {
					warns = append(warns, fmt.Sprintf("env %s=%s cannot be interpolated by Codex; passing %s through via env_vars instead", k, v, m[1]))
				}
				pass = append(pass, m[1])
				continue
			}
			lit[k] = v
		}
		if len(lit) > 0 {
			out["env"] = lit
		}
		if len(pass) > 0 {
			out["env_vars"] = pass
		}
	}
	return out, warns
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// TOMLKey quotes a TOML key when needed.
func TOMLKey(k string) string {
	if bareKey.MatchString(k) {
		return k
	}
	return fmt.Sprintf("%q", k)
}

// CodexFragment renders an OpenCode MCP fragment as Codex TOML.
func CodexFragment(fragment []byte, header string) ([]byte, []string, error) {
	names, defs, err := OpenCodeServers(fragment)
	if err != nil {
		return nil, nil, err
	}
	var warns []string
	var b bytes.Buffer
	b.WriteString(header)
	for _, n := range names {
		tbl, w := CodexServer(defs[n])
		warns = append(warns, w...)
		var buf strings.Builder
		enc := toml.NewEncoder(&buf)
		enc.SetTablesInline(true)
		if err := enc.Encode(tbl); err != nil {
			return nil, nil, err
		}
		b.WriteString("[mcp_servers." + TOMLKey(n) + "]\n" + buf.String())
	}
	return b.Bytes(), warns, nil
}

// CrushrcFragment renders an OpenCode MCP fragment as crushrc lines
// (`mcp add <name> --type ... `), Crush's Bash-based config.
func CrushrcFragment(fragment []byte) ([]byte, []string, error) {
	names, defs, err := OpenCodeServers(fragment)
	if err != nil {
		return nil, nil, err
	}
	var warns []string
	var b strings.Builder
	for _, n := range names {
		def := defs[n]
		args := []string{"mcp", "add", bashWord(n)}
		if isRemote(def) {
			args = append(args, "--type", "http", "--url", bashWord(fmt.Sprint(def.Values["url"])))
			if hdrs, ok := def.Values["headers"].(*jsonx.Object); ok {
				for _, k := range hdrs.Keys {
					args = append(args, "--header", bashWord(k), bashWord(fmt.Sprint(hdrs.Values[k])))
				}
			}
			if _, ok := def.Values["oauth"]; ok {
				warns = append(warns, fmt.Sprintf("MCP server %s: OAuth settings are not translated for Crush", n))
			}
		} else {
			cmd, _ := def.Values["command"].([]any)
			if len(cmd) == 0 {
				warns = append(warns, fmt.Sprintf("MCP server %s has no command; skipped", n))
				continue
			}
			args = append(args, "--type", "stdio", "--command", bashWord(fmt.Sprint(cmd[0])))
			for _, a := range cmd[1:] {
				args = append(args, "--args", bashWord(fmt.Sprint(a)))
			}
			if env, ok := def.Values["environment"].(*jsonx.Object); ok {
				for _, k := range env.Keys {
					args = append(args, "--env", bashWord(k), bashWord(fmt.Sprint(env.Values[k])))
				}
			}
		}
		if isDisabled(def) {
			args = append(args, "--disabled", "true")
		}
		b.WriteString(strings.Join(args, " ") + "\n")
	}
	return []byte(b.String()), warns, nil
}

// bashWord quotes s for Bash, turning OpenCode {env:X} references into "${X}".
func bashWord(s string) string {
	var b strings.Builder
	rest := s
	for rest != "" {
		loc := envRef.FindStringSubmatchIndex(rest)
		lit := rest
		if loc != nil {
			lit = rest[:loc[0]]
		}
		if lit != "" {
			b.WriteString("'" + strings.ReplaceAll(lit, "'", `'\''`) + "'")
		}
		if loc == nil {
			break
		}
		b.WriteString(`"${` + rest[loc[2]:loc[3]] + `}"`)
		rest = rest[loc[1]:]
	}
	if b.Len() == 0 {
		return "''"
	}
	return b.String()
}
