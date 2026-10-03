// Package manifestgen renders api/operations_gen.go from the vendored server manifest
// (contract/api-manifest.json, format tansr-api-manifest-v1). The manifest is the sole fact source of
// the Go operation table: no path, method, query key or domain is written by hand.
//
// The command internal/gen/manifest2go wraps Render for `go run`; api/operations_gen_test.go calls
// Render directly so that `go test ./api` is also the `-check` gate (and `-update` the writer) on
// hosts where freshly built executables cannot be started.
package manifestgen

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/format"
	"strings"
)

// Manifest is the subset of tansr-api-manifest-v1 the generator reads.
type Manifest struct {
	Format     string      `json:"format"`
	Contract   string      `json:"contract"`
	Revision   int         `json:"revision"`
	SchemaHash string      `json:"schemaHash"`
	Families   []Family    `json:"families"`
	Operations []Operation `json:"operations"`
}

// Family is the subset of one manifest families[] entry the generator reads (revision 7: the
// family's body position of the client idempotency key, null when the family has none).
type Family struct {
	ID            string   `json:"id"`
	RequestIDPath []string `json:"requestIdPath"`
}

// ExpectedRevision is the revision-7 If-Match mapping target of a write operation: the body key path
// of expectedRevision and its value kind (sequence = decimal string, integer = non-negative integer).
type ExpectedRevision struct {
	Path []string `json:"path"`
	Kind string   `json:"kind"`
}

// Operation is one manifest operations[] entry.
type Operation struct {
	Name             string            `json:"name"`
	Domain           string            `json:"domain"`
	Family           *string           `json:"family"`
	Method           string            `json:"method"`
	APIPath          string            `json:"apiPath"`
	Aliases          []string          `json:"aliases"`
	Kind             string            `json:"kind"`
	SSE              bool              `json:"sse"`
	Query            []string          `json:"query"`
	Request          *string           `json:"request"`
	Response         *string           `json:"response"`
	ETagPath         []string          `json:"etagPath"`
	ExpectedRevision *ExpectedRevision `json:"expectedRevision"`
}

// Render parses the manifest bytes and returns the formatted Go source of package api's operation table.
func Render(raw []byte) ([]byte, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Format != "tansr-api-manifest-v1" {
		return nil, fmt.Errorf("unsupported manifest format %q", m.Format)
	}
	if m.Contract != "unified-v1" {
		return nil, fmt.Errorf("unsupported manifest contract %q", m.Contract)
	}
	sum := sha256.Sum256(raw)
	return render(&m, hex.EncodeToString(sum[:]))
}

// TemplateParams extracts `:name` placeholders in template order.
func TemplateParams(path string) []string {
	var params []string
	for _, segment := range strings.Split(path, "/") {
		if strings.HasPrefix(segment, ":") {
			params = append(params, segment[1:])
		}
	}
	return params
}

// ConstName turns "session.message.send" into "OpSessionMessageSend".
func ConstName(name string) string {
	var b strings.Builder
	b.WriteString("Op")
	for _, part := range strings.FieldsFunc(name, func(r rune) bool { return r == '.' || r == '-' || r == '_' }) {
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	return b.String()
}

func quoteList(items []string) string {
	if len(items) == 0 {
		return "nil"
	}
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = fmt.Sprintf("%q", item)
	}
	return "[]string{" + strings.Join(quoted, ", ") + "}"
}

func render(m *Manifest, sourceSHA256 string) ([]byte, error) {
	seen := map[string]bool{}
	var domains, families []string
	domainSeen, familySeen := map[string]bool{}, map[string]bool{}
	for _, op := range m.Operations {
		if seen[op.Name] {
			return nil, fmt.Errorf("duplicate operation %q", op.Name)
		}
		seen[op.Name] = true
		if !strings.HasPrefix(op.APIPath, "/api/") {
			return nil, fmt.Errorf("%s: apiPath %q is not under /api/", op.Name, op.APIPath)
		}
		if op.SSE != (op.Kind == "stream") {
			return nil, fmt.Errorf("%s: sse=%v does not match kind=%q", op.Name, op.SSE, op.Kind)
		}
		params := TemplateParams(op.APIPath)
		for _, alias := range op.Aliases {
			if !strings.HasPrefix(alias, "/api/") {
				return nil, fmt.Errorf("%s: alias %q is not under /api/", op.Name, alias)
			}
			if strings.Join(TemplateParams(alias), ",") != strings.Join(params, ",") {
				return nil, fmt.Errorf("%s: alias %q placeholders differ from apiPath", op.Name, alias)
			}
		}
		if !domainSeen[op.Domain] {
			domainSeen[op.Domain] = true
			domains = append(domains, op.Domain)
		}
		if op.Family != nil && !familySeen[*op.Family] {
			familySeen[*op.Family] = true
			families = append(families, *op.Family)
		}
		// revision 7 three-header facts: a key path is never empty; expectedRevision only on writes.
		if op.ETagPath != nil && len(op.ETagPath) == 0 {
			return nil, fmt.Errorf("%s: etagPath must be null or a non-empty key path", op.Name)
		}
		if op.ExpectedRevision != nil {
			if op.Kind != "write" {
				return nil, fmt.Errorf("%s: expectedRevision is only allowed on write operations", op.Name)
			}
			if len(op.ExpectedRevision.Path) == 0 || (op.ExpectedRevision.Kind != "sequence" && op.ExpectedRevision.Kind != "integer") {
				return nil, fmt.Errorf("%s: expectedRevision must have a non-empty path and kind sequence|integer", op.Name)
			}
		}
	}
	familyIDs := map[string]bool{}
	for _, fam := range m.Families {
		if familyIDs[fam.ID] {
			return nil, fmt.Errorf("duplicate family %q", fam.ID)
		}
		familyIDs[fam.ID] = true
		if fam.RequestIDPath != nil && len(fam.RequestIDPath) == 0 {
			return nil, fmt.Errorf("family %s: requestIdPath must be null or a non-empty key path", fam.ID)
		}
	}
	for _, family := range families {
		if !familyIDs[family] {
			return nil, fmt.Errorf("family %q is used by an operation but not registered in families[]", family)
		}
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by internal/gen/manifest2go from contract/api-manifest.json (revision %d, schemaHash %s, manifest SHA256 %s). DO NOT EDIT.\n\n", m.Revision, m.SchemaHash, sourceSHA256)
	b.WriteString("package api\n\n")
	b.WriteString("// ManifestRevision is the manifest revision locked at build time. It is compared with the response\n// header tansr-manifest-revision for diagnostics only; the client never switches logic on it.\n")
	fmt.Fprintf(&b, "const ManifestRevision = %d\n\n", m.Revision)
	b.WriteString("// ManifestSchemaHash is the aggregate schemaHash locked at build time (compared with the `sha256:` part\n// of tansr-schema-hash on discovery responses).\n")
	fmt.Fprintf(&b, "const ManifestSchemaHash = %q\n\n", m.SchemaHash)
	b.WriteString("// ManifestSourceSHA256 is the SHA256 of the manifest file the table was generated from.\n")
	fmt.Fprintf(&b, "const ManifestSourceSHA256 = %q\n\n", sourceSHA256)
	b.WriteString("// Domains is the accepting-domain vocabulary (= response header tansr-domain), derived from the operation table.\n")
	fmt.Fprintf(&b, "var Domains = %s\n\n", quoteList(domains))
	b.WriteString("// Families is the contract-family vocabulary (facade operations have no family and are not listed).\n")
	fmt.Fprintf(&b, "var Families = %s\n\n", quoteList(families))
	b.WriteString("// FamilyRequestIDPaths is the body key path of the client idempotency key per registered family\n// (manifest families[].requestIdPath, revision 7): the position Idempotency-Key is mapped to by the server;\n// nil = the family's body has no such position (deduplication by request fingerprint only).\n")
	b.WriteString("var FamilyRequestIDPaths = map[string][]string{\n")
	for _, fam := range m.Families {
		fmt.Fprintf(&b, "\t%q: %s,\n", fam.ID, quoteList(fam.RequestIDPath))
	}
	b.WriteString("}\n\n")

	b.WriteString("// Operation names.\nconst (\n")
	for _, op := range m.Operations {
		fmt.Fprintf(&b, "\t%s = %q\n", ConstName(op.Name), op.Name)
	}
	b.WriteString(")\n\n")

	b.WriteString("// operations is the operation table (manifest operations[] verbatim): Path is the /api template that the\n// client always instantiates locally, Params the placeholder sequence, Aliases the session-scoped alias\n// templates of the same operation and Query the whitelist of allowed query keys.\n")
	b.WriteString("var operations = []Operation{\n")
	for _, op := range m.Operations {
		family := `""`
		if op.Family != nil {
			family = fmt.Sprintf("%q", *op.Family)
		}
		request, response := `""`, `""`
		if op.Request != nil {
			request = fmt.Sprintf("%q", *op.Request)
		}
		if op.Response != nil {
			response = fmt.Sprintf("%q", *op.Response)
		}
		expected := "nil"
		if op.ExpectedRevision != nil {
			expected = fmt.Sprintf("&ExpectedRevision{Path: %s, Kind: %q}", quoteList(op.ExpectedRevision.Path), op.ExpectedRevision.Kind)
		}
		fmt.Fprintf(&b, "\t{Name: %s, Method: %q, Path: %q, Params: %s, Aliases: %s, Domain: %q, Family: %s, Kind: %q, SSE: %v, Query: %s, Request: %s, Response: %s, ETagPath: %s, ExpectedRevision: %s},\n",
			ConstName(op.Name), op.Method, op.APIPath, quoteList(TemplateParams(op.APIPath)), quoteList(op.Aliases), op.Domain, family, op.Kind, op.SSE, quoteList(op.Query), request, response, quoteList(op.ETagPath), expected)
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}
