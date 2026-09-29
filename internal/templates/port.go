package templates

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/c0dn/ocws/internal/harness"
	"github.com/c0dn/ocws/internal/jsonx"
	"github.com/c0dn/ocws/internal/model"
	"github.com/c0dn/ocws/internal/registry"
	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"
)

// PortResult summarizes generated harness targets for one pack.
type PortResult struct {
	PackID   string   `json:"packId"`
	Harness  string   `json:"harness"`
	Files    int      `json:"files"`
	Created  []string `json:"created"`
	Warnings []string `json:"warnings"`
	Skipped  bool     `json:"skipped"`
}

// Port generates Claude Code / Codex targets for an OpenCode pack: header stubs
// for agents and command-skills, skill directory copies, and MCP fragment
// translations. OpenCode permissions and custom .ts tools are never
// translated; the headers say so, so you can tighten them by hand.
func Port(manifestPath string, harnesses []string, force bool) ([]PortResult, error) {
	pm, err := registry.LoadPack(manifestPath)
	if err != nil {
		return nil, err
	}
	src, err := pm.FilesFor("opencode")
	if err != nil {
		return nil, fmt.Errorf("pack %s has no OpenCode files to port from: %w", pm.ID, err)
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	doc, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	obj := doc.(*jsonx.Object)
	targets, _ := obj.Values["targets"].(*jsonx.Object)
	if targets == nil {
		targets = jsonx.NewObject()
	}

	var results []PortResult
	for _, h := range harnesses {
		if h == "opencode" {
			continue
		}
		res := PortResult{PackID: pm.ID, Harness: h, Created: []string{}, Warnings: []string{}}
		if _, exists := targets.Values[h]; exists && !force {
			res.Skipped = true
			res.Warnings = append(res.Warnings, "target already exists (use --force to regenerate)")
			results = append(results, res)
			continue
		}
		files, warns, created, err := portFiles(pm, src, h)
		if err != nil {
			return nil, err
		}
		res.Warnings, res.Created = append(res.Warnings, warns...), created
		if files == nil {
			res.Skipped = true
			results = append(results, res)
			continue
		}
		res.Files = len(files)
		t := jsonx.NewObject()
		t.Set("files", jsonx.FromGo(files))
		targets.Set(h, t)
		results = append(results, res)
	}
	if len(targets.Keys) > 0 {
		obj.Set("targets", targets)
		if sv, ok := obj.Values["schemaVersion"]; !ok || fmt.Sprint(sv) != "3" {
			obj.Set("schemaVersion", 3)
		}
		if _, ok := obj.Values["harnesses"]; !ok && len(pm.Files) > 0 {
			obj.Set("harnesses", []any{"opencode"})
		}
		if err := jsonx.WriteJSONAtomic(manifestPath, obj); err != nil {
			return nil, err
		}
	}
	return results, nil
}

type portFile struct {
	Source       string   `json:"source"`
	Destination  string   `json:"destination"`
	Managed      *bool    `json:"managed,omitempty"`
	Role         string   `json:"role,omitempty"`
	InstallMode  string   `json:"installMode,omitempty"`
	JSONPointers []string `json:"jsonPointers,omitempty"`
	Render       string   `json:"render,omitempty"`
	Header       string   `json:"header,omitempty"`
}

func portFiles(pm *registry.PackManifest, files []model.FilePlan, h string) ([]portFile, []string, []string, error) {
	var out []portFile
	var warns, created []string
	for _, f := range files {
		if strings.HasPrefix(f.Destination, ".opencode/tools/") || f.Role == "tool" {
			return nil, []string{fmt.Sprintf("pack ships OpenCode custom tools (%s); %s has no equivalent, so the pack stays OpenCode-only", f.Destination, h)}, nil, nil
		}
	}
	write := func(rel string, data []byte) error {
		full := filepath.Join(pm.Dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		created = append(created, rel)
		return os.WriteFile(full, data, 0o644)
	}
	for _, f := range files {
		dest := f.Destination
		switch {
		case strings.HasPrefix(dest, ".opencode/agents/") && strings.HasSuffix(dest, ".md"):
			name := strings.TrimSuffix(path.Base(dest), ".md")
			front := readFront(filepath.Join(pm.Dir, f.Source))
			desc, _ := front["description"].(string)
			if mode, _ := front["mode"].(string); mode == "primary" {
				warns = append(warns, fmt.Sprintf("agent %s is an OpenCode primary agent; %s installs it as a subagent", name, h))
			}
			if h == "claude" {
				hdr := fmt.Sprintf("harness/claude/agents/%s.yaml", name)
				if err := write(hdr, claudeAgentHeader(name, desc)); err != nil {
					return nil, nil, nil, err
				}
				out = append(out, portFile{Source: f.Source, Destination: "{agents}/" + name + ".md", Managed: f.Managed, Render: harness.RenderFrontmatter, Header: hdr})
			} else {
				hdr := fmt.Sprintf("harness/codex/agents/%s.toml", name)
				if err := write(hdr, codexAgentHeader(name, desc)); err != nil {
					return nil, nil, nil, err
				}
				out = append(out, portFile{Source: f.Source, Destination: "{agents}/" + name + ".toml", Managed: f.Managed, Render: harness.RenderCodexAgent, Header: hdr})
			}
		case strings.HasPrefix(dest, ".opencode/commands/") && strings.HasSuffix(dest, ".md"):
			name := strings.TrimSuffix(path.Base(dest), ".md")
			front := readFront(filepath.Join(pm.Dir, f.Source))
			desc, _ := front["description"].(string)
			body, _ := os.ReadFile(filepath.Join(pm.Dir, f.Source))
			if positional.Match(body) {
				if h == "claude" {
					warns = append(warns, fmt.Sprintf("command %s uses $1-style arguments; Claude Code numbers them from $0, so review the body", name))
				} else {
					warns = append(warns, fmt.Sprintf("command %s uses $ARGUMENTS/$1 placeholders; Codex skills receive arguments as plain text, so review the body", name))
				}
			}
			hdr := fmt.Sprintf("harness/%s/commands/%s.yaml", h, name)
			if err := write(hdr, commandSkillHeader(name, desc, h == "claude")); err != nil {
				return nil, nil, nil, err
			}
			out = append(out, portFile{Source: f.Source, Destination: "{skills}/" + name + "/SKILL.md", Managed: f.Managed, Render: harness.RenderFrontmatter, Header: hdr})
			if h == "codex" {
				pol := fmt.Sprintf("harness/codex/commands/%s.openai.yaml", name)
				if err := write(pol, []byte("# Command-style skill: run only when invoked explicitly ($"+name+").\npolicy:\n  allow_implicit_invocation: false\n")); err != nil {
					return nil, nil, nil, err
				}
				out = append(out, portFile{Source: pol, Destination: "{skills}/" + name + "/agents/openai.yaml", Managed: f.Managed, Role: "support"})
			}
		case strings.HasPrefix(dest, ".opencode/skills/"):
			rest := strings.TrimPrefix(dest, ".opencode/skills/")
			if st, err := os.Stat(filepath.Join(pm.Dir, f.Source)); err == nil && st.IsDir() {
				front := readFront(filepath.Join(pm.Dir, f.Source, "SKILL.md"))
				if front["name"] == nil || front["description"] == nil {
					warns = append(warns, fmt.Sprintf("skill %s lacks name/description frontmatter required by %s", rest, h))
				}
			}
			out = append(out, portFile{Source: f.Source, Destination: "{skills}/" + rest, Managed: f.Managed, Role: f.Role})
		case f.InstallMode == "merge" && (dest == "opencode.json" || dest == "opencode.jsonc"):
			pf, w, c, err := portMCP(pm, f, h)
			if err != nil {
				return nil, nil, nil, err
			}
			out, warns, created = append(out, pf...), append(warns, w...), append(created, c...)
		case strings.HasPrefix(dest, ".opencode/"):
			out = append(out, portFile{Source: f.Source, Destination: "{dir}/" + strings.TrimPrefix(dest, ".opencode/"), Managed: f.Managed, Role: f.Role})
		case f.InstallMode != "" && f.InstallMode != "copy":
			warns = append(warns, fmt.Sprintf("%s (installMode=%s) has no %s translation; skipped", dest, f.InstallMode, h))
		default:
			out = append(out, portFile{Source: f.Source, Destination: dest, Managed: f.Managed, Role: f.Role})
		}
	}
	if len(out) == 0 {
		return nil, append(warns, "nothing portable in this pack"), created, nil
	}
	return out, warns, created, nil
}

var positional = regexp.MustCompile(`\$([1-9]|ARGUMENTS)`)

func readFront(p string) map[string]any {
	data, err := os.ReadFile(p)
	if err != nil {
		return map[string]any{}
	}
	front, _ := harness.SplitFrontmatter(data)
	m := map[string]any{}
	yaml.Unmarshal(front, &m)
	return m
}

func yamlScalar(key, value string) string {
	out, _ := yaml.Marshal(map[string]string{key: strings.TrimSpace(value)})
	return string(out)
}

func claudeAgentHeader(name, desc string) []byte {
	var b strings.Builder
	b.WriteString("# Claude Code subagent header generated by `ocws templates port`.\n")
	b.WriteString("# OpenCode permissions were NOT translated. Restrict with `tools`,\n")
	b.WriteString("# `disallowedTools`, or `permissionMode` if this agent should be narrower.\n")
	b.WriteString(yamlScalar("name", name))
	b.WriteString(yamlScalar("description", desc))
	b.WriteString("model: inherit\n")
	return []byte(b.String())
}

func codexAgentHeader(name, desc string) []byte {
	var b strings.Builder
	b.WriteString("# Codex custom agent header generated by `ocws templates port`.\n")
	b.WriteString("# OpenCode permissions were NOT translated; set sandbox_mode to narrow it,\n")
	b.WriteString("# e.g. sandbox_mode = \"read-only\" for review-only agents.\n")
	kv, _ := toml.Marshal(struct {
		Name        string `toml:"name"`
		Description string `toml:"description"`
	}{name, strings.TrimSpace(desc)})
	b.Write(kv)
	return []byte(b.String())
}

func commandSkillHeader(name, desc string, claude bool) []byte {
	var b strings.Builder
	b.WriteString("# Command ported to a skill by `ocws templates port`.\n")
	b.WriteString(yamlScalar("name", name))
	b.WriteString(yamlScalar("description", desc))
	if claude {
		b.WriteString("disable-model-invocation: true\n")
	}
	return []byte(b.String())
}

var envRef = regexp.MustCompile(`\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

func portMCP(pm *registry.PackManifest, f model.FilePlan, h string) ([]portFile, []string, []string, error) {
	var warns, created []string
	data, err := os.ReadFile(filepath.Join(pm.Dir, f.Source))
	if err != nil {
		return nil, nil, nil, err
	}
	root, err := jsonx.Parse(data)
	if err != nil {
		return nil, nil, nil, err
	}
	servers := map[string]*jsonx.Object{}
	var names []string
	for _, p := range f.JSONPointers {
		segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
		if len(segs) < 2 || segs[0] != "mcp" {
			warns = append(warns, fmt.Sprintf("%s pointer %s is OpenCode-specific config; not ported to %s", f.Source, p, h))
			continue
		}
		v, err := jsonx.GetPointer(root, p)
		if err != nil {
			return nil, nil, nil, err
		}
		def, ok := v.(*jsonx.Object)
		if !ok {
			continue
		}
		name := segs[len(segs)-1]
		servers[name] = def
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, warns, nil, nil
	}
	sort.Strings(names)
	base := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(f.Source), ".json"), ".opencode.fragment")
	if h == "claude" {
		frag := jsonx.NewObject()
		mcpServers := jsonx.NewObject()
		var ptrs []string
		for _, n := range names {
			mcpServers.Set(n, claudeServer(servers[n]))
			ptrs = append(ptrs, "/mcpServers/"+n)
		}
		frag.Set("mcpServers", mcpServers)
		rel := "harness/claude/" + base + ".mcp.json"
		if err := os.MkdirAll(filepath.Join(pm.Dir, "harness/claude"), 0o755); err != nil {
			return nil, nil, nil, err
		}
		if err := jsonx.WriteJSONAtomic(filepath.Join(pm.Dir, rel), frag); err != nil {
			return nil, nil, nil, err
		}
		created = append(created, rel)
		return []portFile{{Source: rel, Destination: "{mcp}", Managed: f.Managed, Role: "merged-fragment", InstallMode: "merge", JSONPointers: ptrs}}, warns, created, nil
	}
	var b strings.Builder
	b.WriteString("# Generated by `ocws templates port` from " + f.Source + ".\n")
	for _, n := range names {
		tbl, w := codexServer(servers[n])
		warns = append(warns, w...)
		var buf strings.Builder
		enc := toml.NewEncoder(&buf)
		enc.SetTablesInline(true)
		if err := enc.Encode(tbl); err != nil {
			return nil, nil, nil, err
		}
		b.WriteString("[mcp_servers." + tomlKey(n) + "]\n" + buf.String())
	}
	rel := "harness/codex/" + base + ".mcp.toml"
	if err := os.MkdirAll(filepath.Join(pm.Dir, "harness/codex"), 0o755); err != nil {
		return nil, nil, nil, err
	}
	if err := os.WriteFile(filepath.Join(pm.Dir, rel), []byte(b.String()), 0o644); err != nil {
		return nil, nil, nil, err
	}
	created = append(created, rel)
	return []portFile{{Source: rel, Destination: "{mcp}", Managed: f.Managed, Role: "merged-fragment", InstallMode: "toml-merge"}}, warns, created, nil
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func tomlKey(k string) string {
	if bareKey.MatchString(k) {
		return k
	}
	return fmt.Sprintf("%q", k)
}

func str(o *jsonx.Object, k string) string {
	s, _ := o.Values[k].(string)
	return s
}

func claudeServer(def *jsonx.Object) *jsonx.Object {
	out := jsonx.NewObject()
	sub := func(s string) string { return envRef.ReplaceAllString(s, `$${$1}`) }
	if str(def, "type") == "remote" || str(def, "url") != "" {
		out.Set("type", "http")
		out.Set("url", sub(str(def, "url")))
		if hdrs, ok := def.Values["headers"].(*jsonx.Object); ok {
			o := jsonx.NewObject()
			for _, k := range hdrs.Keys {
				o.Set(k, sub(fmt.Sprint(hdrs.Values[k])))
			}
			out.Set("headers", o)
		}
		return out
	}
	if cmd, ok := def.Values["command"].([]any); ok && len(cmd) > 0 {
		out.Set("command", sub(fmt.Sprint(cmd[0])))
		var args []any
		for _, a := range cmd[1:] {
			args = append(args, sub(fmt.Sprint(a)))
		}
		if len(args) > 0 {
			out.Set("args", args)
		}
	}
	if env, ok := def.Values["environment"].(*jsonx.Object); ok {
		o := jsonx.NewObject()
		for _, k := range env.Keys {
			o.Set(k, sub(fmt.Sprint(env.Values[k])))
		}
		out.Set("env", o)
	}
	return out
}

func codexServer(def *jsonx.Object) (map[string]any, []string) {
	var warns []string
	out := map[string]any{}
	if b, ok := def.Values["disabled"].(bool); ok && b {
		out["enabled"] = false
	}
	if b, ok := def.Values["enabled"].(bool); ok && !b {
		out["enabled"] = false
	}
	if str(def, "type") == "remote" || str(def, "url") != "" {
		out["url"] = str(def, "url")
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
