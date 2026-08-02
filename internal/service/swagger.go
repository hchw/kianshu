package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi3"
)

// ErrNeedConfirmation indicates the swagger document is not standard enough to
// import as-is; the caller must pause and ask the user how to proceed.
var ErrNeedConfirmation = errors.New("swagger 不标准,需用户确认")

// ErrInvalidDocument indicates the swagger document cannot be parsed into any
// importable operations at all; asking for confirmation cannot help.
var ErrInvalidDocument = errors.New("swagger 文档无法解析导入")

// ParsedOperation is a normalized view of one swagger operation.
type ParsedOperation struct {
	Method      string
	Path        string
	Tag         string
	Name        string
	Params      string // JSON array
	RequestBody string // JSON (may be "null")
	Responses   string // JSON
	Security    string // JSON (may be "null")
	Spec        string // full redundant operation JSON
}

// ParsedDoc is the result of a strict swagger parse.
type ParsedDoc struct {
	Operations []ParsedOperation
	BasePath   string
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// ParseSwagger parses a swagger/openapi document. It supports OpenAPI 2.0 and
// 3.x via strict parsers, and falls back to a lenient generic extraction when
// the version is unrecognized or strict parsing fails.
//
// Returns the parsed doc, a list of non-standard issues found (empty when
// fully standard), and an error only when nothing importable can be extracted.
func ParseSwagger(data []byte) (*ParsedDoc, []string, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil, fmt.Errorf("%w: 文档为空", ErrInvalidDocument)
	}
	var head struct {
		Swagger  string          `json:"swagger"`
		OpenAPI  string          `json:"openapi"`
		PathsRaw json.RawMessage `json:"paths"`
		DefsRaw  json.RawMessage `json:"definitions"`
	}
	if err := json.Unmarshal(trimmed, &head); err != nil {
		return nil, nil, fmt.Errorf("%w: 不是合法 JSON: %v", ErrInvalidDocument, err)
	}
	if len(head.PathsRaw) == 0 || bytes.Equal(bytes.TrimSpace(head.PathsRaw), []byte("null")) {
		return nil, nil, fmt.Errorf("%w: 文档缺少 paths 字段", ErrInvalidDocument)
	}

	var ops []ParsedOperation
	var basePath string
	var reasons []string

	switch {
	case head.Swagger == "2.0":
		var doc openapi2.T
		if err := json.Unmarshal(trimmed, &doc); err != nil {
			reasons = append(reasons, fmt.Sprintf("OpenAPI 2.0 严格解析失败(%v), 已回退通用解析", err))
			ops = genericOps(head.PathsRaw, buildDefs(head.DefsRaw))
		} else {
			basePath = doc.BasePath
			for p, item := range doc.Paths {
				if item == nil {
					continue
				}
				ops = append(ops, v2Ops(p, item, doc.Definitions)...)
			}
		}
	case strings.HasPrefix(head.OpenAPI, "3."):
		doc, err := openapi3.NewLoader().LoadFromData(trimmed)
		if err != nil {
			reasons = append(reasons, fmt.Sprintf("OpenAPI 3.x 严格解析失败(%v), 已回退通用解析", err))
			ops = genericOps(head.PathsRaw, buildDefs(head.DefsRaw))
		} else {
			for p, item := range doc.Paths.Map() {
				if item == nil {
					continue
				}
				if item.Get != nil {
					ops = append(ops, v3Op(p, "GET", item.Get))
				}
				if item.Post != nil {
					ops = append(ops, v3Op(p, "POST", item.Post))
				}
				if item.Put != nil {
					ops = append(ops, v3Op(p, "PUT", item.Put))
				}
				if item.Delete != nil {
					ops = append(ops, v3Op(p, "DELETE", item.Delete))
				}
				if item.Patch != nil {
					ops = append(ops, v3Op(p, "PATCH", item.Patch))
				}
				if item.Head != nil {
					ops = append(ops, v3Op(p, "HEAD", item.Head))
				}
				if item.Options != nil {
					ops = append(ops, v3Op(p, "OPTIONS", item.Options))
				}
			}
		}
	default:
		reasons = append(reasons, fmt.Sprintf("无法识别的版本(swagger=%q, openapi=%q), 已按通用结构解析", head.Swagger, head.OpenAPI))
		ops = genericOps(head.PathsRaw, buildDefs(head.DefsRaw))
	}

	if len(ops) == 0 {
		return nil, nil, fmt.Errorf("%w: 文档中未找到任何可导入的接口操作", ErrInvalidDocument)
	}
	return &ParsedDoc{Operations: ops, BasePath: basePath}, reasons, nil
}

func buildDefs(raw json.RawMessage) map[string]json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var defs map[string]json.RawMessage
	if err := json.Unmarshal(raw, &defs); err != nil {
		return nil
	}
	return defs
}

func resolveRefSchema(raw json.RawMessage, defs map[string]json.RawMessage) json.RawMessage {
	if defs == nil {
		return nil
	}
	var ref struct {
		Ref string `json:"$ref"`
	}
	if err := json.Unmarshal(raw, &ref); err != nil || ref.Ref == "" || !strings.HasPrefix(ref.Ref, "#/definitions/") {
		return nil
	}
	key := strings.TrimPrefix(ref.Ref, "#/definitions/")
	if resolved, ok := defs[key]; ok {
		return resolved
	}
	return nil
}

func resolveParamRefs(raw json.RawMessage, defs map[string]json.RawMessage) json.RawMessage {
	if defs == nil {
		return raw
	}
	var params []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &params); err != nil {
		return raw
	}
	changed := false
	for i, p := range params {
		_, hasSchema := p["schema"]
		if !hasSchema {
			if pRef, ok := p["$ref"]; ok {
				if resolved := resolveRefSchema(pRef, defs); resolved != nil {
					params[i] = map[string]json.RawMessage{"schema": resolved}
					changed = true
				}
			}
			continue
		}
		if resolved := resolveRefSchema(p["schema"], defs); resolved != nil {
			p["schema"] = resolved
			changed = true
		}
	}
	if !changed {
		return raw
	}
	b, _ := json.Marshal(params)
	return b
}

func resolveRefBody(raw json.RawMessage, defs map[string]json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 || defs == nil {
		return raw
	}
	var body struct {
		Schema json.RawMessage `json:"schema"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return raw
	}
	if resolved := resolveRefSchema(body.Schema, defs); resolved != nil {
		body.Schema = resolved
		b, _ := json.Marshal(body)
		return b
	}
	if body.Content != nil {
		var content map[string]json.RawMessage
		if err := json.Unmarshal(body.Content, &content); err != nil {
			return raw
		}
		changed := false
		for ct, media := range content {
			var mediaObj struct {
				Schema json.RawMessage `json:"schema"`
			}
			if err := json.Unmarshal(media, &mediaObj); err != nil || mediaObj.Schema == nil {
				continue
			}
			if resolved := resolveRefSchema(mediaObj.Schema, defs); resolved != nil {
				content[ct], _ = json.Marshal(map[string]json.RawMessage{"schema": resolved})
				changed = true
			}
		}
		if changed {
			body.Content, _ = json.Marshal(content)
			b, _ := json.Marshal(body)
			return b
		}
	}
	return raw
}

func genericOps(raw json.RawMessage, defs map[string]json.RawMessage) []ParsedOperation {
	var paths map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &paths); err != nil {
		return nil
	}
	var ops []ParsedOperation
	for p, item := range paths {
		for m, opRaw := range item {
			method := strings.ToUpper(m)
			switch method {
			case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS":
			default:
				continue
			}
			var op struct {
				Summary     string          `json:"summary"`
				OperationID string          `json:"operationId"`
				Tags        []string        `json:"tags"`
				Parameters  json.RawMessage `json:"parameters"`
				RequestBody json.RawMessage `json:"requestBody"`
				Responses   json.RawMessage `json:"responses"`
				Security    json.RawMessage `json:"security"`
			}
			if err := json.Unmarshal(opRaw, &op); err != nil {
				continue
			}
			tag := ""
			if len(op.Tags) > 0 {
				tag = op.Tags[0]
			}
			ops = append(ops, ParsedOperation{
				Method:      method,
				Path:        p,
				Tag:         tag,
				Name:        firstNonEmpty(op.Summary, op.OperationID),
				Params:      rawOrNull(resolveParamRefs(op.Parameters, defs)),
				RequestBody: rawOrNull(resolveRefBody(op.RequestBody, defs)),
				Responses:   rawOrNull(op.Responses),
				Security:    rawOrNull(op.Security),
				Spec:        string(opRaw),
			})
		}
	}
	return ops
}

func rawOrNull(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "null"
	}
	return string(raw)
}

var v2Methods = []struct {
	name string
	get  func(*openapi2.PathItem) *openapi2.Operation
}{
	{"GET", func(p *openapi2.PathItem) *openapi2.Operation { return p.Get }},
	{"POST", func(p *openapi2.PathItem) *openapi2.Operation { return p.Post }},
	{"PUT", func(p *openapi2.PathItem) *openapi2.Operation { return p.Put }},
	{"DELETE", func(p *openapi2.PathItem) *openapi2.Operation { return p.Delete }},
	{"PATCH", func(p *openapi2.PathItem) *openapi2.Operation { return p.Patch }},
	{"HEAD", func(p *openapi2.PathItem) *openapi2.Operation { return p.Head }},
	{"OPTIONS", func(p *openapi2.PathItem) *openapi2.Operation { return p.Options }},
}

func v2Ops(path string, item *openapi2.PathItem, defs map[string]*openapi2.SchemaRef) []ParsedOperation {
	var out []ParsedOperation
	for _, m := range v2Methods {
		if op := m.get(item); op != nil {
			out = append(out, v2Op(path, m.name, op, defs))
		}
	}
	return out
}

func v2Op(path, method string, op *openapi2.Operation, defs map[string]*openapi2.SchemaRef) ParsedOperation {
	tag := ""
	if len(op.Tags) > 0 {
		tag = op.Tags[0]
	}
	requestBody := "null"
	for _, p := range op.Parameters {
		if p != nil && p.In == "body" {
			schema := p.Schema
			if schema != nil && schema.Ref != "" && defs != nil {
				ref := strings.TrimPrefix(schema.Ref, "#/definitions/")
				if def, ok := defs[ref]; ok && def.Value != nil {
					requestBody = mustJSON(def.Value)
					break
				}
			}
			requestBody = mustJSON(schema)
			break
		}
	}
	security := "null"
	if op.Security != nil {
		security = mustJSON(op.Security)
	}
	return ParsedOperation{
		Method:      method,
		Path:        path,
		Tag:         tag,
		Name:        firstNonEmpty(op.Summary, op.OperationID),
		Params:      mustJSON(op.Parameters),
		RequestBody: requestBody,
		Responses:   mustJSON(op.Responses),
		Security:    security,
		Spec:        mustJSON(op),
	}
}

func v3Op(path, method string, op *openapi3.Operation) ParsedOperation {
	tag := ""
	if len(op.Tags) > 0 {
		tag = op.Tags[0]
	}
	requestBody := "null"
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		requestBody = mustJSON(op.RequestBody.Value)
	}
	security := "null"
	if op.Security != nil {
		security = mustJSON(op.Security)
	}
	return ParsedOperation{
		Method:      method,
		Path:        path,
		Tag:         tag,
		Name:        firstNonEmpty(op.Summary, op.OperationID),
		Params:      mustJSON(op.Parameters),
		RequestBody: requestBody,
		Responses:   mustJSON(op.Responses),
		Security:    security,
		Spec:        mustJSON(op),
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Slug normalizes method+path to a stable identifier with slashes replaced by
// hyphens, e.g. GET /users/{id} -> get-users-{id}.
func Slug(method, path string) string {
	return strings.ToLower(method) + "-" + strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(path), "/"), "/", "-")
}
