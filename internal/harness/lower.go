package harness

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/c0dn/ocws/internal/jsonx"
	"go.yaml.in/yaml/v3"
)

// OpenCode V2 -> V1 lowering, following the OpenCode team's compatibility
// table: V2 is the authoring format; V1 files are produced from it.

// v1Rename maps V2 top-level config keys to their V1 spelling.
var v1Rename = map[string]string{
	"agents": "agent", "commands": "command", "plugins": "plugin", "permissions": "permission",
	"snapshots": "snapshot", "media": "attachment", "providers": "provider",
}

// v1Action maps V2 permission action names to V1 tool keys.
var v1Action = map[string]string{"shell": "bash", "subagent": "task"}

// LowerPointer maps a V2 config JSON pointer to its V1 location.
func LowerPointer(p string) string {
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	if len(segs) == 0 || segs[0] == "" {
		return p
	}
	if segs[0] == "mcp" && len(segs) >= 2 && segs[1] == "servers" {
		segs = append([]string{"mcp"}, segs[2:]...)
	} else if r, ok := v1Rename[segs[0]]; ok {
		segs[0] = r
	}
	return "/" + strings.Join(segs, "/")
}

// LowerConfig rewrites a V2 opencode.json document (or fragment) as V1.
func LowerConfig(data []byte) ([]byte, error) {
	v, err := jsonx.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse OpenCode config: %w", err)
	}
	root, ok := v.(*jsonx.Object)
	if !ok {
		return nil, fmt.Errorf("OpenCode config must be a JSON object")
	}
	return append(jsonx.Marshal(lowerConfigObject(root)), '\n'), nil
}

func lowerConfigObject(in *jsonx.Object) *jsonx.Object {
	out := jsonx.NewObject()
	for _, k := range in.Keys {
		v := in.Values[k]
		switch k {
		case "mcp":
			out.Set("mcp", lowerMCP(v))
		case "permissions":
			if rules, ok := v.([]any); ok {
				out.Set("permission", lowerPermissions(rules))
				continue
			}
			out.Set("permission", v)
		case "agents":
			if o, ok := v.(*jsonx.Object); ok {
				agents := jsonx.NewObject()
				for _, name := range o.Keys {
					if a, ok := o.Values[name].(*jsonx.Object); ok {
						agents.Set(name, lowerAgentObject(a))
					} else {
						agents.Set(name, o.Values[name])
					}
				}
				out.Set("agent", agents)
				continue
			}
			out.Set("agent", v)
		case "commands":
			if o, ok := v.(*jsonx.Object); ok {
				cmds := jsonx.NewObject()
				for _, name := range o.Keys {
					if c, ok := o.Values[name].(*jsonx.Object); ok {
						cmds.Set(name, lowerModelField(c))
					} else {
						cmds.Set(name, o.Values[name])
					}
				}
				out.Set("command", cmds)
				continue
			}
			out.Set("command", v)
		case "plugins":
			if list, ok := v.([]any); ok {
				var pl []any
				for _, p := range list {
					if o, ok := p.(*jsonx.Object); ok {
						if pkg, ok := o.Values["package"]; ok {
							if opts, ok := o.Values["options"]; ok {
								pl = append(pl, []any{pkg, opts})
							} else {
								pl = append(pl, pkg)
							}
							continue
						}
					}
					pl = append(pl, p)
				}
				out.Set("plugin", pl)
				continue
			}
			out.Set("plugin", v)
		case "skills":
			if list, ok := v.([]any); ok {
				sk := jsonx.NewObject()
				var paths, urls []any
				for _, s := range list {
					if str, ok := s.(string); ok && (strings.HasPrefix(str, "http://") || strings.HasPrefix(str, "https://")) {
						urls = append(urls, s)
					} else {
						paths = append(paths, s)
					}
				}
				if paths != nil {
					sk.Set("paths", paths)
				}
				if urls != nil {
					sk.Set("urls", urls)
				}
				out.Set("skills", sk)
				continue
			}
			out.Set("skills", v)
		case "model":
			if o, ok := v.(*jsonx.Object); ok {
				if id, ok := o.Values["id"].(string); ok {
					out.Set("model", id)
					continue
				}
			}
			m, _ := splitVariant(v)
			out.Set("model", m)
		default:
			if r, ok := v1Rename[k]; ok {
				out.Set(r, v)
			} else {
				out.Set(k, v)
			}
		}
	}
	return out
}

// lowerMCP turns {servers: {x: {..., disabled}}} into a flat {x: {..., enabled}}.
func lowerMCP(v any) any {
	o, ok := v.(*jsonx.Object)
	if !ok {
		return v
	}
	servers, ok := o.Values["servers"].(*jsonx.Object)
	if !ok {
		return v // already V1-shaped
	}
	out := jsonx.NewObject()
	for _, name := range servers.Keys {
		def, ok := servers.Values[name].(*jsonx.Object)
		if !ok {
			out.Set(name, servers.Values[name])
			continue
		}
		out.Set(name, lowerMCPServer(def))
	}
	return out
}

var oauthCamel = map[string]string{"client_id": "clientId", "client_secret": "clientSecret", "callback_port": "callbackPort", "redirect_uri": "redirectUri"}

func lowerMCPServer(def *jsonx.Object) *jsonx.Object {
	out := jsonx.NewObject()
	for _, k := range def.Keys {
		v := def.Values[k]
		switch k {
		case "disabled":
			if b, ok := v.(bool); ok {
				out.Set("enabled", !b)
			}
		case "codemode":
			// V2-only; V1 has no Code Mode.
		case "oauth":
			if o, ok := v.(*jsonx.Object); ok {
				oo := jsonx.NewObject()
				for _, ok := range o.Keys {
					if c, has := oauthCamel[ok]; has {
						oo.Set(c, o.Values[ok])
					} else {
						oo.Set(ok, o.Values[ok])
					}
				}
				out.Set("oauth", oo)
				continue
			}
			out.Set(k, v)
		case "timeout":
			if _, ok := v.(*jsonx.Object); ok {
				continue // per-phase V2 timeouts have no V1 equivalent
			}
			out.Set(k, v)
		default:
			out.Set(k, v)
		}
	}
	return out
}

// lowerPermissions turns V2 [{action, resource, effect}] rules into the V1
// permission map. Later rules win in both formats, so order is kept.
func lowerPermissions(rules []any) *jsonx.Object {
	out := jsonx.NewObject()
	for _, r := range rules {
		o, ok := r.(*jsonx.Object)
		if !ok {
			continue
		}
		action, _ := o.Values["action"].(string)
		effect, _ := o.Values["effect"].(string)
		resource, _ := o.Values["resource"].(string)
		if action == "" || effect == "" {
			continue
		}
		if a, ok := v1Action[action]; ok {
			action = a
		}
		if resource == "" {
			resource = "*"
		}
		cur, has := out.Values[action]
		switch {
		case resource == "*" && (!has || isString(cur)):
			out.Set(action, effect)
		default:
			m, ok := cur.(*jsonx.Object)
			if !ok {
				m = jsonx.NewObject()
				if s, isStr := cur.(string); isStr {
					m.Set("*", s)
				}
			}
			m.Delete(resource) // re-append so later rules stay later
			m.Set(resource, effect)
			out.Set(action, m)
		}
	}
	return out
}

func isString(v any) bool { _, ok := v.(string); return ok }

func lowerAgentObject(a *jsonx.Object) *jsonx.Object {
	out := jsonx.NewObject()
	for _, k := range a.Keys {
		v := a.Values[k]
		switch k {
		case "system":
			out.Set("prompt", v)
		case "disabled":
			out.Set("disable", v)
		case "permissions":
			if rules, ok := v.([]any); ok {
				out.Set("permission", lowerPermissions(rules))
			} else {
				out.Set("permission", v)
			}
		case "model":
			m, variant := splitVariant(v)
			out.Set("model", m)
			if variant != "" {
				out.Set("variant", variant)
			}
		default:
			out.Set(k, v)
		}
	}
	return out
}

func lowerModelField(c *jsonx.Object) *jsonx.Object {
	out := jsonx.NewObject()
	for _, k := range c.Keys {
		if k == "model" {
			m, variant := splitVariant(c.Values[k])
			out.Set("model", m)
			if variant != "" {
				out.Set("variant", variant)
			}
			continue
		}
		out.Set(k, c.Values[k])
	}
	return out
}

// splitVariant turns "provider/model#variant" into model and variant.
func splitVariant(v any) (any, string) {
	s, ok := v.(string)
	if !ok {
		return v, ""
	}
	if i := strings.LastIndex(s, "#"); i > 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// LowerMarkdown lowers V2 agent/command frontmatter (permissions list,
// model#variant, disabled) to V1, keeping key order and the body.
func LowerMarkdown(data []byte) ([]byte, error) {
	front, body := SplitFrontmatter(data)
	if front == nil {
		return data, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(front, &doc); err != nil {
		return nil, fmt.Errorf("frontmatter is not valid YAML: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return data, nil
	}
	m := doc.Content[0]
	var content []*yaml.Node
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		switch k.Value {
		case "model":
			if v.Kind == yaml.ScalarNode {
				if mm, variant := splitVariant(v.Value); variant != "" {
					v.Value = mm.(string)
					content = append(content, k, v, scalar("variant"), scalar(variant))
					continue
				}
			}
		case "disabled":
			k.Value = "disable"
		case "permissions":
			if v.Kind == yaml.SequenceNode {
				var rules []any
				if err := v.Decode(&rules); err == nil {
					lowered := lowerPermissions(toJSONRules(rules))
					content = append(content, scalar("permission"), orderedNode(lowered))
					continue
				}
			}
		}
		content = append(content, k, v)
	}
	m.Content = content
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	enc.Close()
	var out bytes.Buffer
	out.WriteString("---\n")
	out.Write(b.Bytes())
	out.WriteString("---\n\n")
	out.Write(body)
	return out.Bytes(), nil
}

func scalar(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }

// orderedNode renders a jsonx object as an insertion-ordered YAML mapping.
func orderedNode(o *jsonx.Object) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, k := range o.Keys {
		var v *yaml.Node
		switch t := o.Values[k].(type) {
		case *jsonx.Object:
			v = orderedNode(t)
		case string:
			v = scalar(t)
			v.Style = yaml.DoubleQuotedStyle
		default:
			v = &yaml.Node{}
			v.Encode(t)
		}
		kn := scalar(k)
		if strings.ContainsAny(k, "*:{}[],&#?|<>=!%@` ") {
			kn.Style = yaml.DoubleQuotedStyle
		}
		n.Content = append(n.Content, kn, v)
	}
	return n
}

func toJSONRules(rules []any) []any {
	var out []any
	for _, r := range rules {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		o := jsonx.NewObject()
		for _, k := range []string{"action", "resource", "effect"} {
			if v, ok := m[k]; ok {
				o.Set(k, fmt.Sprint(v))
			}
		}
		out = append(out, o)
	}
	return out
}
