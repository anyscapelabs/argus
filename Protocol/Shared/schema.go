package Shared

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// CleanSchemaForGemini returns the schema in the shape Gemini accepts. An
// unreadable or absent schema becomes an open object, because a tool that
// can be called with no arguments still works while a rejected request
// works nowhere. Exported so both the native Gemini adapter and the
// OpenAI adapter (when it fronts a Gemini endpoint) share the logic.
func CleanSchemaForGemini(raw []byte) []byte {
	const open = `{"type":"object","properties":{}}`
	if len(bytes.TrimSpace(raw)) == 0 {
		return []byte(open)
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return []byte(open)
	}
	out, err := json.Marshal(trimSchema(root, &schemaCtx{root: root, live: map[string]bool{}}))
	if err != nil {
		return []byte(open)
	}
	return out
}

// allowedFields is the Schema type's own field list. Keywords a general JSON
// Schema may carry but Gemini has no field for (minLength, pattern,
// additionalProperties, $comment, default, examples) are dropped: losing a
// validation hint is harmless, sending it is fatal.
var allowedFields = map[string]bool{
	"type":             true,
	"format":           true,
	"description":      true,
	"enum":             true,
	"nullable":         true,
	"properties":       true,
	"required":         true,
	"items":            true,
	"anyOf":            true,
	"maxItems":         true,
	"propertyOrdering": true,
}

const maxSchemaDepth = 24

type schemaCtx struct {
	root  any
	live  map[string]bool
	depth int
}

// openNode is what an unresolvable or unrepresentable schema becomes: callable,
// unchecked, and free of fields Gemini would reject.
func openNode() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func trimSchema(v any, c *schemaCtx) any {
	c.depth++
	if c.depth > maxSchemaDepth {
		return openNode()
	}
	switch n := v.(type) {
	case map[string]any:
		return trimObject(n, c)
	case []any:
		// A tuple "items": [{schemaA},{schemaB}] has no Gemini equivalent; the
		// first branch is what any element would have to satisfy anyway.
		if len(n) > 0 {
			return trimSchema(n[0], c)
		}
		return openNode()
	default:
		// A boolean schema (draft 2020's true/false) is not a field set.
		return openNode()
	}
}

func trimObject(n map[string]any, c *schemaCtx) any {
	if ref, ok := n["$ref"].(string); ok {
		return followRef(ref, n, c)
	}
	out := map[string]any{}
	for k, v := range n {
		switch k {
		case "type":
			// A union of types becomes anyOf, with "null" turned into nullable.
			if list, isList := v.([]any); isList {
				branches := []any{}
				for _, t := range list {
					s, ok := t.(string)
					if !ok {
						continue
					}
					if s == "null" {
						out["nullable"] = true
						continue
					}
					branches = append(branches, map[string]any{"type": s})
				}
				switch len(branches) {
				case 0:
				case 1:
					out["type"] = branches[0].(map[string]any)["type"]
				default:
					out["anyOf"] = branches
				}
				continue
			}
			out["type"] = v
		case "properties":
			out["properties"] = trimProperties(v, c)
		case "items":
			out["items"] = trimSchema(v, c)
		case "anyOf", "oneOf", "allOf":
			// Gemini has anyOf. oneOf is the same union, so it becomes one; an
			// allOf of one branch is that branch, and anything richer is dropped
			// rather than sent as a field that does not exist.
			arr, ok := v.([]any)
			if !ok {
				continue
			}
			branches := make([]any, 0, len(arr))
			for _, x := range arr {
				branches = append(branches, trimSchema(x, c))
			}
			switch {
			case len(branches) == 0:
			case len(branches) == 1 && k != "oneOf":
				mergeInto(out, branches[0])
			default:
				out["anyOf"] = branches
			}
		case "const":
			// The only way to say "exactly this value" in Gemini's Schema.
			out["enum"] = []any{v}
		case "enum":
			out["enum"] = v
		case "required":
			out["required"] = v
		case "propertyOrdering":
			out["propertyOrdering"] = v
		case "nullable", "format", "description", "maxItems":
			out[k] = v
		default:
			// $defs, definitions, additionalProperties, x-google-* and every
			// other keyword: dropped, which is the whole point of this file.
		}
	}
	if _, has := out["properties"]; !has {
		if t, ok := out["type"].(string); ok && t == "object" {
			out["properties"] = map[string]any{}
		}
	}
	return out
}

// followRef replaces a pointer with the schema it names. Sibling keywords on the
// reference itself survive when the target does not already carry them, so a
// {$ref, description} pair keeps its description.
func followRef(ref string, node map[string]any, c *schemaCtx) any {
	target, ok := resolvePointer(ref, c.root)
	if !ok || c.live[ref] {
		return openNode()
	}
	c.live[ref] = true
	inner := trimSchema(target, c)
	delete(c.live, ref)
	m, ok := inner.(map[string]any)
	if !ok {
		return inner
	}
	for k, v := range node {
		if k == "$ref" || !allowedFields[k] {
			continue
		}
		if _, exists := m[k]; exists {
			continue
		}
		m[k] = v
	}
	return m
}

// resolvePointer walks a local JSON pointer ("#/$defs/Filters") from the root of
// the tool's own schema. Remote pointers are not fetched, so they open instead of
// breaking the request.
func resolvePointer(ref string, root any) (any, bool) {
	if ref == "#" {
		return root, true
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	node := root
	for _, seg := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		seg = strings.ReplaceAll(seg, "~1", "/")
		seg = strings.ReplaceAll(seg, "~0", "~")
		switch n := node.(type) {
		case map[string]any:
			v, ok := n[seg]
			if !ok {
				return nil, false
			}
			node = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(n) {
				return nil, false
			}
			node = n[i]
		default:
			return nil, false
		}
	}
	return node, true
}

func trimProperties(v any, c *schemaCtx) any {
	props, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	out := make(map[string]any, len(props))
	for name, sub := range props {
		out[name] = trimSchema(sub, c)
	}
	return out
}

func mergeInto(out map[string]any, src any) {
	m, ok := src.(map[string]any)
	if !ok {
		return
	}
	for k, v := range m {
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
}
