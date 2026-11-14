package main

import (
	"bytes"
	"fmt"
	"go/format"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// annotationKeywords only constrain or document values: a Go type cannot express them, so they
// are read for the comments or skipped.
var annotationKeywords = map[string]bool{
	"description": true, "example": true, "examples": true, "format": true, "pattern": true,
	"minLength": true, "maxLength": true, "minimum": true, "maximum": true,
	"exclusiveMinimum": true, "exclusiveMaximum": true, "multipleOf": true, "minItems": true,
	"maxItems": true, "uniqueItems": true, "minProperties": true, "maxProperties": true,
	"default": true, "title": true, "deprecated": true,
}

var structureKeywords = map[string]bool{
	"type": true, "properties": true, "required": true, "items": true, "enum": true,
	"const": true, "$ref": true, "allOf": true, "anyOf": true, "oneOf": true,
	"discriminator": true, "additionalProperties": true, "not": true,
}

// paramsNames names the query structs of the list operations; any other operation with a query
// gets <OperationID>Params.
var paramsNames = map[string]string{
	"certificatesList":               "CertificateListParams",
	"documentsList":                  "DocumentListParams",
	"emissionPointsList":             "EmissionPointListParams",
	"establishmentsList":             "EstablishmentListParams",
	"eventsList":                     "EventListParams",
	"issuersList":                    "IssuerListParams",
	"webhookEndpointsDeliveriesList": "WebhookDeliveryListParams",
	"webhookEndpointsList":           "WebhookEndpointListParams",
}

// typeNames renames schemas whose Go name reads better otherwise.
var typeNames = map[string]string{
	"EventThin": "ThinEvent",
}

// eventTypes are the types of the events the API sends.
var eventTypes = []string{
	"invoice.authorized", "invoice.rejected", "purchase_settlement.authorized",
	"purchase_settlement.rejected", "credit_note.authorized", "credit_note.rejected",
	"debit_note.authorized", "debit_note.rejected", "waybill.authorized", "waybill.rejected",
	"withholding.authorized", "withholding.rejected", "certificate.verified",
	"certificate.pending", "certificate.blocked", "certificate.revoked",
	"certificate.reactivated", "document.ride.generated", "document.ride.failed",
	"document.email.sent", "document.email.delivered", "document.email.bounced",
	"document.email.complained", "document.email.failed", "account.email_paused",
	"account.email_resumed",
}

// sharedEnums names the inline enums that several schemas repeat with the same values, in the
// same order: each set becomes one Go type instead of one per property. An enum whose values
// change no longer matches its entry and gets its own name, so a change is never folded in
// silently.
var sharedEnums = []struct {
	name   string
	values []string
}{
	{"DocumentStatus", []string{"processing", "pending_sri", "authorized", "rejected", "failed", "voided"}},
	{"DocumentType", []string{"invoice", "purchase_settlement", "credit_note", "debit_note", "waybill", "withholding"}},
	{"EnabledEvent", append(slices.Clone(eventTypes), "*")},
	{"Environment", []string{"test", "live"}},
	{"EventType", eventTypes},
	{"IDType", []string{"ruc", "cedula", "passport", "final_consumer", "foreign_id"}},
	{"PaymentMethod", []string{"cash", "debt_offset", "debit_card", "electronic_money", "prepaid_card", "credit_card", "other_financial_system", "securities_endorsement"}},
	{"PayloadMode", []string{"full", "thin"}},
	{"RIDEStatus", []string{"pending", "generated", "failed"}},
	{"RIMPERegime", []string{"entrepreneur", "popular_business"}},
	{"TaxType", []string{"vat", "ice"}},
	{"WebhookDeliveryStatus", []string{"pending", "succeeded", "failed"}},
}

func sharedEnum(values []string) (string, bool) {
	for _, shared := range sharedEnums {
		if slices.Equal(shared.values, values) {
			return shared.name, true
		}
	}
	return "", false
}

// direction says whether a schema describes what the SDK sends or what it receives: optional
// members that may be null are Nullable[T] in what it sends (null clears a field in a PATCH),
// pointers in what it receives.
type direction int

const (
	incoming direction = iota
	outgoing
)

type typeKind int

const (
	kindValue     typeKind = iota // a scalar or a struct: a pointer when optional
	kindReference                 // a slice, a map or an interface: nil already means absent
)

type goType struct {
	expr     string
	kind     typeKind
	nullable bool
	time     bool // time.Time, which needs the time import
}

type field struct {
	name     string
	jsonName string
	typ      string
	tag      string
	doc      string
}

type enumDef struct {
	values []string
}

type unionDef struct {
	members      []string          // Go type names, in document order
	discriminant string            // the JSON member that names the variant
	mapping      []member          // value -> Go type name (as a string node)
	byValue      map[string]string // value -> Go type name
}

type definition struct {
	name   string
	doc    string
	fields []field
	enum   *enumDef
	union  *unionDef
	extra  bool         // additionalProperties: true
	query  []queryField // list params
	isList bool         // a query struct
}

type queryField struct {
	field
	encode string // Go expression that turns *p.Field into the text sent
}

type generator struct {
	document   *node
	schemas    *node
	parameters *node
	directions map[string]direction
	defs       map[string]*definition
	enumSets   map[string]string // shared value sets already defined
	needTime   bool
	needJSON   bool
	needQuery  bool
}

func newGenerator(document *node) (*generator, error) {
	version := document.get("openapi").str()
	if !strings.HasPrefix(version, "3.1") {
		return nil, fmt.Errorf("only OpenAPI 3.1 documents are supported, got %q", version)
	}
	components := document.get("components")
	g := &generator{
		document:   document,
		schemas:    components.get("schemas"),
		parameters: components.get("parameters"),
		defs:       map[string]*definition{},
		enumSets:   map[string]string{},
	}
	if g.schemas == nil || g.schemas.kind != kindObject {
		return nil, fmt.Errorf("the document has no components.schemas")
	}
	directions, err := g.classify()
	if err != nil {
		return nil, err
	}
	g.directions = directions
	return g, nil
}

// classify finds which schemas requests send and which responses carry. A schema used by both
// would need a choice for its nullable members, so it is refused. Schemas used by neither (the
// webhook bodies, the error body) are received.
func (g *generator) classify() (map[string]direction, error) {
	sent := map[string]bool{}
	received := map[string]bool{}
	var walk func(n *node, into map[string]bool) error
	walk = func(n *node, into map[string]bool) error {
		if n == nil {
			return nil
		}
		switch n.kind {
		case kindObject:
			if ref := n.get("$ref").str(); strings.HasPrefix(ref, "#/components/schemas/") {
				name := strings.TrimPrefix(ref, "#/components/schemas/")
				target := g.schemas.get(name)
				if target == nil {
					return fmt.Errorf("unknown reference %s", ref)
				}
				if !into[name] {
					into[name] = true
					if err := walk(target, into); err != nil {
						return err
					}
				}
			}
			for _, m := range n.members {
				if err := walk(m.value, into); err != nil {
					return err
				}
			}
		case kindArray:
			for _, item := range n.items {
				if err := walk(item, into); err != nil {
					return err
				}
			}
		}
		return nil
	}
	paths := g.document.get("paths")
	for _, path := range paths.entries() {
		for _, operation := range path.value.entries() {
			if !isMethod(operation.key) {
				continue
			}
			if err := walk(operation.value.get("requestBody"), sent); err != nil {
				return nil, err
			}
			for _, response := range operation.value.get("responses").entries() {
				if strings.HasPrefix(response.key, "2") {
					if err := walk(response.value, received); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	result := map[string]direction{}
	for _, schema := range g.schemas.members {
		if sent[schema.key] && received[schema.key] {
			return nil, fmt.Errorf("%s is both sent and received: decide how its nullable members are written", schema.key)
		}
		if sent[schema.key] {
			result[schema.key] = outgoing
		} else {
			result[schema.key] = incoming
		}
	}
	return result, nil
}

// wireName is what a member or a query parameter may be called: it is written into struct tags
// and string literals, so a name that could end them (a backtick, a quote, a comma) is refused.
var wireName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

func checkWireName(name string) error {
	if !wireName.MatchString(name) {
		return fmt.Errorf("the name %q has characters other than letters, digits, _, . and -", name)
	}
	return nil
}

func isMethod(key string) bool {
	switch key {
	case "get", "post", "put", "patch", "delete":
		return true
	}
	return false
}

func (g *generator) typeName(schema string) (string, error) {
	if name, ok := typeNames[schema]; ok {
		return name, nil
	}
	return goName(schema)
}

func (g *generator) define(def *definition) error {
	if _, exists := g.defs[def.name]; exists {
		return fmt.Errorf("two definitions are named %s", def.name)
	}
	g.defs[def.name] = def
	return nil
}

func (g *generator) checkKeywords(schema *node, where string) error {
	if schema.kind != kindObject {
		return fmt.Errorf("%s: expected a schema object", where)
	}
	for _, key := range schema.keys() {
		if !annotationKeywords[key] && !structureKeywords[key] {
			return fmt.Errorf("%s: unsupported keyword %s", where, key)
		}
	}
	return nil
}

func typesOf(schema *node, where string) ([]string, error) {
	declared := schema.get("type")
	if declared == nil {
		return nil, nil
	}
	if declared.kind == kindString {
		return []string{declared.text}, nil
	}
	return declared.strings(where + ".type")
}

// resolve is the Go type of a schema. Inline enums and objects become definitions named hint.
func (g *generator) resolve(schema *node, hint, where string, dir direction) (goType, error) {
	if err := g.checkKeywords(schema, where); err != nil {
		return goType{}, err
	}
	if ref := schema.get("$ref"); ref != nil {
		return g.reference(schema, where)
	}
	for _, combinator := range []string{"allOf", "anyOf", "oneOf"} {
		if schema.has(combinator) {
			return g.combination(schema, combinator, hint, where, dir)
		}
	}
	types, err := typesOf(schema, where)
	if err != nil {
		return goType{}, err
	}
	nullable := slices.Contains(types, "null")
	types = slices.DeleteFunc(types, func(t string) bool { return t == "null" })
	if schema.has("not") && !slices.Equal(types, []string{"object"}) {
		return goType{}, fmt.Errorf("%s: `not` is only supported on objects", where)
	}
	if constant := schema.get("const"); constant != nil {
		if constant.kind != kindString || (len(types) > 0 && !slices.Equal(types, []string{"string"})) {
			return goType{}, fmt.Errorf("%s: only string constants are supported", where)
		}
		return goType{expr: "string", kind: kindValue, nullable: nullable}, nil
	}
	if schema.has("enum") {
		return g.enum(schema, hint, where, nullable)
	}
	if len(types) > 1 {
		return goType{}, fmt.Errorf("%s: several non-null types", where)
	}
	if len(types) == 0 {
		if schema.has("properties") {
			types = []string{"object"}
		} else if nullable {
			return goType{}, fmt.Errorf("%s: a schema that is only null", where)
		} else {
			for _, key := range schema.keys() {
				if !annotationKeywords[key] {
					return goType{}, fmt.Errorf("%s: a schema without a type", where)
				}
			}
			return goType{expr: "any", kind: kindReference}, nil
		}
	}
	result, err := g.kind(schema, types[0], hint, where, dir)
	if err != nil {
		return goType{}, err
	}
	result.nullable = result.nullable || nullable
	return result, nil
}

func (g *generator) kind(schema *node, kind, hint, where string, dir direction) (goType, error) {
	switch kind {
	case "string":
		switch schema.get("format").str() {
		case "date-time":
			g.needTime = true
			return goType{expr: "time.Time", kind: kindValue, time: true}, nil
		case "binary":
			return goType{}, fmt.Errorf("%s: binary strings are only supported in multipart bodies", where)
		}
		return goType{expr: "string", kind: kindValue}, nil
	case "integer":
		return goType{expr: "int", kind: kindValue}, nil
	case "number":
		return goType{expr: "float64", kind: kindValue}, nil
	case "boolean":
		return goType{expr: "bool", kind: kindValue}, nil
	case "array":
		items := schema.get("items")
		if items == nil {
			return goType{}, fmt.Errorf("%s: an array without items", where)
		}
		item, err := g.resolve(items, hint+"Item", where+".items", dir)
		if err != nil {
			return goType{}, err
		}
		element := item.expr
		if item.nullable && item.kind == kindValue {
			element = "*" + element
		}
		return goType{expr: "[]" + element, kind: kindReference}, nil
	case "object":
		properties := schema.get("properties")
		extra := schema.get("additionalProperties")
		if properties == nil {
			if extra != nil && extra.kind == kindObject {
				value, err := g.resolve(extra, hint+"Value", where+".additionalProperties", dir)
				if err != nil {
					return goType{}, err
				}
				element := value.expr
				if value.nullable && value.kind == kindValue {
					element = "*" + element
				}
				return goType{expr: "map[string]" + element, kind: kindReference}, nil
			}
			return goType{expr: "map[string]any", kind: kindReference}, nil
		}
		if err := g.object(hint, schema, where, dir); err != nil {
			return goType{}, err
		}
		return goType{expr: hint, kind: kindValue}, nil
	}
	return goType{}, fmt.Errorf("%s: unsupported type %q", where, kind)
}

func (g *generator) reference(schema *node, where string) (goType, error) {
	ref := schema.get("$ref").str()
	const prefix = "#/components/schemas/"
	name := strings.TrimPrefix(ref, prefix)
	target := g.schemas.get(name)
	if !strings.HasPrefix(ref, prefix) || target == nil {
		return goType{}, fmt.Errorf("%s: unknown reference %s", where, ref)
	}
	for _, key := range schema.keys() {
		if key != "$ref" && !annotationKeywords[key] {
			return goType{}, fmt.Errorf("%s: $ref with sibling keywords", where)
		}
	}
	goName, err := g.typeName(name)
	if err != nil {
		return goType{}, err
	}
	if target.has("oneOf") {
		return goType{expr: goName, kind: kindReference}, nil
	}
	types, err := typesOf(target, name)
	if err != nil {
		return goType{}, err
	}
	nullable := slices.Contains(types, "null")
	if target.has("properties") || target.has("enum") {
		return goType{expr: goName, kind: kindValue, nullable: nullable}, nil
	}
	return goType{}, fmt.Errorf("%s: references to %s, which is not an object or an enum, are not supported", where, name)
}

func (g *generator) combination(schema *node, combinator, hint, where string, dir direction) (goType, error) {
	for _, key := range schema.keys() {
		if structureKeywords[key] && key != combinator && key != "discriminator" {
			return goType{}, fmt.Errorf("%s: %s with %s", where, combinator, key)
		}
	}
	members := schema.get(combinator).children()
	switch combinator {
	case "allOf":
		if len(members) != 1 {
			return goType{}, fmt.Errorf("%s: allOf is only supported with one member", where)
		}
		return g.resolve(members[0], hint, where+".allOf[0]", dir)
	case "anyOf":
		// Only "this or null".
		var value *node
		nulls := 0
		for _, item := range members {
			types, err := typesOf(item, where)
			if err != nil {
				return goType{}, err
			}
			if slices.Equal(types, []string{"null"}) && len(item.members) == 1 {
				nulls++
				continue
			}
			value = item
		}
		if len(members) != 2 || nulls != 1 || value == nil {
			return goType{}, fmt.Errorf("%s: anyOf is only supported as one schema or null", where)
		}
		result, err := g.resolve(value, hint, where+".anyOf", dir)
		if err != nil {
			return goType{}, err
		}
		result.nullable = true
		return result, nil
	}
	return goType{}, fmt.Errorf("%s: oneOf is only supported as a component", where)
}

func (g *generator) enum(schema *node, hint, where string, nullable bool) (goType, error) {
	var values []string
	for _, item := range schema.get("enum").children() {
		switch item.kind {
		case kindNull:
			nullable = true
		case kindString:
			values = append(values, item.text)
		default:
			return goType{}, fmt.Errorf("%s: enum values must be strings", where)
		}
	}
	if len(values) == 0 {
		return goType{}, fmt.Errorf("%s: an enum with no values", where)
	}
	key := strings.Join(values, "|")
	name := hint
	if shared, ok := sharedEnum(values); ok {
		name = shared
		if _, done := g.enumSets[key]; done {
			return goType{expr: name, kind: kindValue, nullable: nullable}, nil
		}
		g.enumSets[key] = name
		if existing := g.defs[name]; existing != nil {
			return goType{}, fmt.Errorf("%s: %s is both a shared enum and a schema", where, name)
		}
	}
	def := &definition{name: name, doc: describe(schema), enum: &enumDef{values: values}}
	if name != hint {
		def.doc = ""
	}
	if err := g.define(def); err != nil {
		return goType{}, fmt.Errorf("%s: %w", where, err)
	}
	return goType{expr: name, kind: kindValue, nullable: nullable}, nil
}

func (g *generator) object(name string, schema *node, where string, dir direction) error {
	for _, key := range []string{"allOf", "anyOf", "oneOf", "enum", "const", "items"} {
		if schema.has(key) {
			return fmt.Errorf("%s: properties mixed with %s", where, key)
		}
	}
	types, err := typesOf(schema, where)
	if err != nil {
		return err
	}
	if len(types) > 0 && !slices.Equal(types, []string{"object"}) {
		return fmt.Errorf("%s: properties mixed with other types", where)
	}
	if not := schema.get("not"); not != nil {
		if !slices.Equal(not.keys(), []string{"required"}) {
			return fmt.Errorf("%s: `not` is only supported as not.required", where)
		}
		forbidden, err := not.get("required").strings(where + ".not.required")
		if err != nil {
			return err
		}
		for _, key := range forbidden {
			if schema.get("properties").has(key) {
				return fmt.Errorf("%s: %s is both declared and forbidden", where, key)
			}
		}
	}
	extra := schema.get("additionalProperties")
	if extra != nil && extra.kind != kindBool {
		return fmt.Errorf("%s: typed members with typed additionalProperties", where)
	}
	required, err := schema.get("required").strings(where + ".required")
	if err != nil {
		return err
	}
	properties := schema.get("properties")
	for _, key := range required {
		if !properties.has(key) {
			return fmt.Errorf("%s: the required member %s has no schema", where, key)
		}
	}
	def := &definition{name: name, doc: describe(schema), extra: extra != nil && extra.boolean}
	if def.extra {
		g.needJSON = true
	}
	if err := g.define(def); err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	seen := map[string]string{}
	for _, property := range properties.entries() {
		if err := checkWireName(property.key); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		fieldName, err := goName(property.key)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", where, property.key, err)
		}
		if def.extra && fieldName == "Extra" {
			return fmt.Errorf("%s.%s: clashes with the Extra member", where, property.key)
		}
		if other, clash := seen[fieldName]; clash {
			return fmt.Errorf("%s: %s and %s are both %s in Go", where, other, property.key, fieldName)
		}
		seen[fieldName] = property.key
		typ, err := g.resolve(property.value, name+fieldName, where+"."+property.key, dir)
		if err != nil {
			return err
		}
		if typ.kind == kindReference && g.isUnion(typ.expr) {
			return fmt.Errorf("%s.%s: a union inside an object is not supported", where, property.key)
		}
		isRequired := slices.Contains(required, property.key)
		expr, tag := memberType(typ, isRequired, dir)
		def.fields = append(def.fields, field{
			name:     fieldName,
			jsonName: property.key,
			typ:      expr,
			tag:      property.key + tag,
			doc:      describe(property.value),
		})
	}
	return nil
}

// memberType is how a member is declared: required members are values (pointers when they may
// be null), optional ones are pointers (slices and maps: nil), and optional members that may be
// null are Nullable[T] in what the SDK sends.
func memberType(typ goType, required bool, dir direction) (string, string) {
	switch {
	case !required && typ.nullable && dir == outgoing:
		return "Nullable[" + typ.expr + "]", ",omitzero"
	case typ.kind == kindReference:
		if required {
			return typ.expr, ""
		}
		return typ.expr, ",omitempty"
	case required && !typ.nullable:
		return typ.expr, ""
	case required:
		return "*" + typ.expr, ""
	default:
		return "*" + typ.expr, ",omitempty"
	}
}

func (g *generator) isUnion(name string) bool {
	def := g.defs[name]
	return def != nil && def.union != nil
}

// isListBody reports the body of a list operation ({data, pagination}): Page[T] stands for it.
func isListBody(schema *node) bool {
	properties := schema.get("properties")
	return slices.Equal(properties.keys(), []string{"data", "pagination"}) &&
		properties.get("data").get("type").str() == "array" &&
		properties.get("pagination").get("$ref").str() == "#/components/schemas/Pagination"
}

func (g *generator) component(schemaName string, schema *node) error {
	where := "components.schemas." + schemaName
	if err := g.checkKeywords(schema, where); err != nil {
		return err
	}
	if isListBody(schema) {
		return nil
	}
	name, err := g.typeName(schemaName)
	if err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	dir := g.directions[schemaName]
	switch {
	case schema.has("oneOf"):
		return g.union(name, schema, where)
	case schema.has("properties"):
		return g.object(name, schema, where, dir)
	case schema.has("enum"):
		types, err := typesOf(schema, where)
		if err != nil {
			return err
		}
		if !slices.Equal(types, []string{"string"}) {
			return fmt.Errorf("%s: only string enums are supported as components", where)
		}
		values, err := schema.get("enum").strings(where + ".enum")
		if err != nil {
			return err
		}
		return g.define(&definition{name: name, doc: describe(schema), enum: &enumDef{values: values}})
	}
	return fmt.Errorf("%s: only objects, enums and discriminated unions are supported as components", where)
}

// union is a discriminated oneOf: an interface its members implement, and a decoder that picks
// the member from the discriminator. Only the API's Event union is known: its members also
// implement WebhookEvent, the interface of every webhook body.
func (g *generator) union(name string, schema *node, where string) error {
	if name != "Event" {
		return fmt.Errorf("%s: the only discriminated union supported is Event", where)
	}
	for _, key := range schema.keys() {
		if structureKeywords[key] && key != "oneOf" && key != "discriminator" {
			return fmt.Errorf("%s: oneOf with %s", where, key)
		}
	}
	discriminator := schema.get("discriminator")
	property := discriminator.get("propertyName").str()
	if property == "" {
		return fmt.Errorf("%s: a oneOf needs a discriminator.propertyName", where)
	}
	if err := checkWireName(property); err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	def := &definition{name: name, doc: describe(schema), union: &unionDef{discriminant: property, byValue: map[string]string{}}}
	referenced := map[string]bool{}
	for index, item := range schema.get("oneOf").children() {
		ref := strings.TrimPrefix(item.get("$ref").str(), "#/components/schemas/")
		if g.schemas.get(ref) == nil || len(item.members) != 1 {
			return fmt.Errorf("%s.oneOf[%d]: a discriminated union must list references only", where, index)
		}
		memberName, err := g.typeName(ref)
		if err != nil {
			return err
		}
		if g.directions[ref] != incoming {
			return fmt.Errorf("%s: %s is sent, but the union is only decoded", where, ref)
		}
		referenced[ref] = true
		def.union.members = append(def.union.members, memberName)
	}
	for _, entry := range discriminator.get("mapping").entries() {
		target := strings.TrimPrefix(entry.value.str(), "#/components/schemas/")
		if !referenced[target] {
			return fmt.Errorf("%s: the mapping %s points outside the union", where, entry.key)
		}
		allowed, err := g.discriminantValues(target, property)
		if err != nil {
			return err
		}
		if !slices.Contains(allowed, entry.key) {
			return fmt.Errorf("%s: %s.%s does not allow %s", where, target, property, entry.key)
		}
		memberName, _ := g.typeName(target)
		def.union.byValue[entry.key] = memberName
		def.union.mapping = append(def.union.mapping, entry)
	}
	// Every value a member allows must be mapped, or it would decode as an unknown event.
	for ref := range referenced {
		allowed, err := g.discriminantValues(ref, property)
		if err != nil {
			return err
		}
		for _, value := range allowed {
			if _, ok := def.union.byValue[value]; !ok {
				return fmt.Errorf("%s: %s allows %s, which the discriminator does not map", where, ref, value)
			}
		}
		if err := g.checkEventMember(ref); err != nil {
			return err
		}
	}
	g.needJSON = true
	return g.define(def)
}

func (g *generator) discriminantValues(schemaName, property string) ([]string, error) {
	discriminant := g.schemas.get(schemaName).get("properties").get(property)
	if discriminant == nil {
		return nil, fmt.Errorf("%s has no %s member", schemaName, property)
	}
	if constant := discriminant.get("const"); constant != nil {
		return []string{constant.str()}, nil
	}
	return discriminant.get("enum").strings(schemaName + "." + property)
}

// checkEventMember checks a member of Event has what EventInfo reads from it.
func (g *generator) checkEventMember(schemaName string) error {
	schema := g.schemas.get(schemaName)
	required, err := schema.get("required").strings(schemaName + ".required")
	if err != nil {
		return err
	}
	for _, key := range []string{"id", "type", "environment", "createdAt"} {
		if !slices.Contains(required, key) {
			return fmt.Errorf("%s: an event must require %s", schemaName, key)
		}
	}
	properties := schema.get("properties")
	if properties.get("createdAt").get("format").str() != "date-time" {
		return fmt.Errorf("%s: createdAt must be a date-time", schemaName)
	}
	values, err := properties.get("environment").get("enum").strings(schemaName + ".environment")
	if err != nil || strings.Join(values, "|") != "test|live" {
		return fmt.Errorf("%s: environment must be test or live", schemaName)
	}
	return nil
}

// queries defines the query struct of every operation that takes one.
func (g *generator) queries() error {
	for _, path := range g.document.get("paths").entries() {
		for _, operation := range path.value.entries() {
			if !isMethod(operation.key) {
				continue
			}
			operationID := operation.value.get("operationId").str()
			if operationID == "" {
				return fmt.Errorf("%s %s has no operationId", operation.key, path.key)
			}
			name, ok := paramsNames[operationID]
			if !ok {
				generated, err := goName(operationID)
				if err != nil {
					return err
				}
				name = generated + "Params"
			}
			def := &definition{
				name:   name,
				doc:    fmt.Sprintf("%s holds the query of %s (%s %s). Nil members are not sent.", name, operationID, strings.ToUpper(operation.key), path.key),
				isList: true,
			}
			for _, raw := range operation.value.get("parameters").children() {
				parameter, err := g.parameter(raw, operationID)
				if err != nil {
					return err
				}
				if parameter.get("in").str() != "query" {
					continue
				}
				key := parameter.get("name").str()
				where := operationID + "." + key
				if err := checkWireName(key); err != nil {
					return fmt.Errorf("%s: %w", operationID, err)
				}
				if parameter.get("required") != nil && parameter.get("required").boolean {
					return fmt.Errorf("%s: required query parameters are not supported", where)
				}
				fieldName, err := goName(key)
				if err != nil {
					return fmt.Errorf("%s: %w", where, err)
				}
				schema := parameter.get("schema")
				typ, err := g.resolve(schema, name+fieldName, where, outgoing)
				if err != nil {
					return err
				}
				encode, err := queryEncoding(typ, fieldName, where)
				if err != nil {
					return err
				}
				def.query = append(def.query, queryField{
					field: field{
						name:     fieldName,
						jsonName: key,
						typ:      "*" + typ.expr,
						doc:      joinSentences(describe(parameter), describe(schema)),
					},
					encode: encode,
				})
			}
			if len(def.query) > 0 {
				g.needQuery = true
				if err := g.define(def); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func queryEncoding(typ goType, fieldName, where string) (string, error) {
	if typ.nullable || typ.kind != kindValue || typ.time {
		return "", fmt.Errorf("%s: only strings, numbers, booleans and enums are supported in a query", where)
	}
	value := "*p." + fieldName
	switch typ.expr {
	case "string":
		return value, nil
	case "int":
		return "strconv.Itoa(" + value + ")", nil
	case "float64":
		return "strconv.FormatFloat(" + value + ", 'f', -1, 64)", nil
	case "bool":
		return "strconv.FormatBool(" + value + ")", nil
	}
	return "string(" + value + ")", nil
}

func (g *generator) parameter(raw *node, where string) (*node, error) {
	ref := raw.get("$ref").str()
	if ref == "" {
		return raw, nil
	}
	const prefix = "#/components/parameters/"
	target := g.parameters.get(strings.TrimPrefix(ref, prefix))
	if !strings.HasPrefix(ref, prefix) || target == nil {
		return nil, fmt.Errorf("%s: unknown parameter %s", where, ref)
	}
	return target, nil
}

// describe is the comment of a schema: its description, then its format and default.
func describe(schema *node) string {
	var parts []string
	if description := strings.Join(strings.Fields(schema.get("description").str()), " "); description != "" {
		parts = append(parts, description)
	}
	if format := schema.get("format").str(); format != "" && format != "binary" && format != "password" {
		parts = append(parts, "Format: "+format+".")
	}
	if value := schema.get("default"); value != nil {
		parts = append(parts, "Default: "+value.literal()+".")
	}
	if deprecated := schema.get("deprecated"); deprecated != nil && deprecated.boolean {
		parts = append(parts, "\n\nDeprecated: the API marks it as deprecated.")
	}
	return strings.Join(parts, " ")
}

func joinSentences(first, second string) string {
	return strings.TrimSpace(first + " " + second)
}

// Generate returns the Go source of the types of a contract.
func Generate(contract []byte) ([]byte, error) {
	document, err := parseJSON(contract)
	if err != nil {
		return nil, fmt.Errorf("the contract is not JSON: %w", err)
	}
	g, err := newGenerator(document)
	if err != nil {
		return nil, err
	}
	names := g.schemas.keys()
	sort.Strings(names)
	for _, name := range names {
		if err := g.component(name, g.schemas.get(name)); err != nil {
			return nil, err
		}
	}
	if err := g.queries(); err != nil {
		return nil, err
	}
	if err := g.check(); err != nil {
		return nil, err
	}
	source := g.emit()
	formatted, err := format.Source(source)
	if err != nil {
		return nil, fmt.Errorf("the generated code does not compile: %w\n%s", err, source)
	}
	return formatted, nil
}

// check refuses names that would clash once the constants of the enums exist.
func (g *generator) check() error {
	taken := map[string]string{}
	for name := range g.defs {
		taken[name] = "type " + name
	}
	for _, def := range g.defs {
		if def.enum == nil {
			continue
		}
		for _, value := range def.enum.values {
			suffix, err := constantSuffix(value)
			if err != nil {
				return fmt.Errorf("%s: %w", def.name, err)
			}
			constant := def.name + suffix
			if other, clash := taken[constant]; clash {
				return fmt.Errorf("the constant for %s %q is named %s, like %s", def.name, value, constant, other)
			}
			taken[constant] = "a constant of " + def.name
		}
	}
	return nil
}

const header = `// Code generated by internal/cmd/openapi from openapi/openapi.json. DO NOT EDIT.

// The types of the Flashpi API, generated from its public OpenAPI document. Regenerate them with
// "go run ./internal/cmd/openapi generate", or download the document again first with "sync".

package flashpi
`

func (g *generator) emit() []byte {
	var out bytes.Buffer
	out.WriteString(header)
	var imports []string
	if g.needJSON {
		imports = append(imports, `"encoding/json"`)
	}
	if g.needQuery {
		imports = append(imports, `"net/url"`, `"strconv"`)
	}
	if g.needTime {
		imports = append(imports, `"time"`)
	}
	if len(imports) > 0 {
		out.WriteString("\nimport (\n" + strings.Join(imports, "\n") + "\n)\n")
	}
	names := make([]string, 0, len(g.defs))
	for name := range g.defs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out.WriteString("\n")
		g.emitDefinition(&out, g.defs[name])
	}
	return out.Bytes()
}

func (g *generator) emitDefinition(out *bytes.Buffer, def *definition) {
	switch {
	case def.enum != nil:
		emitEnum(out, def)
	case def.union != nil:
		emitUnion(out, def)
	case def.isList:
		emitQuery(out, def)
	default:
		emitStruct(out, def)
	}
}

func emitEnum(out *bytes.Buffer, def *definition) {
	doc := def.doc
	if doc == "" {
		doc = "One of the constants below. The API may add values: keep a default case."
	} else {
		doc += " The API may add values: keep a default case."
	}
	comment(out, "", def.name+" is a value of the API. "+doc)
	fmt.Fprintf(out, "type %s string\n\n", def.name)
	fmt.Fprintf(out, "// The values of %s.\nconst (\n", def.name)
	for _, value := range def.enum.values {
		suffix, _ := constantSuffix(value)
		fmt.Fprintf(out, "%s%s %s = %s\n", def.name, suffix, def.name, strconv.Quote(value))
	}
	out.WriteString(")\n")
}

func emitStruct(out *bytes.Buffer, def *definition) {
	doc := def.name + " is a schema of the API."
	if def.doc != "" {
		doc = def.name + ": " + def.doc
	}
	comment(out, "", doc)
	fmt.Fprintf(out, "type %s struct {\n", def.name)
	for index, f := range def.fields {
		if index > 0 {
			out.WriteString("\n")
		}
		if f.doc != "" {
			comment(out, "\t", f.doc)
		}
		fmt.Fprintf(out, "\t%s %s `json:%s`\n", f.name, f.typ, strconv.Quote(f.tag))
	}
	if def.extra {
		if len(def.fields) > 0 {
			out.WriteString("\n")
		}
		comment(out, "\t", "Extra holds the members the API sent that this version of the SDK does not declare, as raw JSON.")
		out.WriteString("\tExtra map[string]json.RawMessage `json:\"-\"`\n")
	}
	out.WriteString("}\n")
	if def.extra {
		emitExtra(out, def)
	}
}

// emitExtra writes the JSON methods of an object with additionalProperties: true, which keep
// the members it does not declare in Extra.
func emitExtra(out *bytes.Buffer, def *definition) {
	keys := make([]string, 0, len(def.fields))
	for _, f := range def.fields {
		keys = append(keys, strconv.Quote(f.jsonName))
	}
	known := "known" + def.name + "Members"
	fmt.Fprintf(out, "\nvar %s = []string{%s}\n", known, strings.Join(keys, ", "))
	fmt.Fprintf(out, `
// UnmarshalJSON reads the declared members, and keeps the others in Extra.
func (v *%[1]s) UnmarshalJSON(data []byte) error {
	type plain %[1]s
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return err
	}
	for _, key := range %[2]s {
		delete(members, key)
	}
	v.Extra = nil
	if len(members) > 0 {
		v.Extra = members
	}
	return nil
}

// MarshalJSON writes the declared members and the ones in Extra.
func (v %[1]s) MarshalJSON() ([]byte, error) {
	type plain %[1]s
	data, err := json.Marshal(plain(v))
	if err != nil || len(v.Extra) == 0 {
		return data, err
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return nil, err
	}
	for key, value := range v.Extra {
		if _, declared := members[key]; !declared {
			members[key] = value
		}
	}
	return json.Marshal(members)
}
`, def.name, known)
}

func emitQuery(out *bytes.Buffer, def *definition) {
	comment(out, "", def.doc)
	fmt.Fprintf(out, "type %s struct {\n", def.name)
	for index, f := range def.query {
		if index > 0 {
			out.WriteString("\n")
		}
		if f.doc != "" {
			comment(out, "\t", f.doc)
		}
		fmt.Fprintf(out, "\t%s %s\n", f.name, f.typ)
	}
	out.WriteString("}\n\n")
	fmt.Fprintf(out, "// values is the query string of p, without the members that are nil.\n")
	fmt.Fprintf(out, "func (p *%s) values() url.Values {\n\tquery := url.Values{}\n\tif p == nil {\n\t\treturn query\n\t}\n", def.name)
	for _, f := range def.query {
		fmt.Fprintf(out, "\tif p.%s != nil {\n\t\tquery.Set(%s, %s)\n\t}\n", f.name, strconv.Quote(f.jsonName), f.encode)
	}
	out.WriteString("\treturn query\n}\n")
}

func emitUnion(out *bytes.Buffer, def *definition) {
	members := make([]string, 0, len(def.union.members))
	for _, name := range def.union.members {
		members = append(members, "*"+name)
	}
	list := strings.Join(members[:len(members)-1], ", ") + " or " + members[len(members)-1]
	doc := def.name + " is what happened to a resource: one of " + list + ", or an *UnknownEvent for a type this version of the SDK does not know. Use a type switch."
	if def.doc != "" {
		doc += " " + def.doc
	}
	comment(out, "", doc)
	fmt.Fprintf(out, "type %s interface {\n\tWebhookEvent\n\tis%s()\n}\n", def.name, def.name)
	for _, name := range def.union.members {
		fmt.Fprintf(out, "\nfunc (*%s) is%s() {}\n", name, def.name)
		fmt.Fprintf(out, "\nfunc (*%s) isWebhookEvent() {}\n", name)
		fmt.Fprintf(out, `
// Info returns what every event has: its id, type, environment and creation time.
func (e *%s) Info() EventInfo {
	return EventInfo{ID: e.ID, Type: EventType(e.Type), Environment: e.Environment, CreatedAt: e.CreatedAt}
}
`, name)
	}
	// One case per member, with its values in document order.
	fmt.Fprintf(out, `
// decode%[1]s decodes an event of the API into its type, from its %[2]s member.
func decode%[1]s(data []byte) (%[1]s, error) {
	var head struct {
		Discriminant string `+"`json:%[3]s`"+`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	var event %[1]s
	switch head.Discriminant {
`, def.name, def.union.discriminant, strconv.Quote(def.union.discriminant))
	for _, name := range def.union.members {
		var values []string
		for _, entry := range def.union.mapping {
			if def.union.byValue[entry.key] == name {
				values = append(values, strconv.Quote(entry.key))
			}
		}
		fmt.Fprintf(out, "\tcase %s:\n\t\tevent = new(%s)\n", strings.Join(values, ", "), name)
	}
	out.WriteString("\tdefault:\n\t\tevent = new(UnknownEvent)\n\t}\n")
	out.WriteString("\tif err := json.Unmarshal(data, event); err != nil {\n\t\treturn nil, err\n\t}\n\treturn event, nil\n}\n")
}

// comment writes text as a // comment wrapped at 100 columns, keeping paragraph breaks.
func comment(out *bytes.Buffer, indent, text string) {
	width := 100 - len(indent) - 3
	if indent == "\t" {
		width = 100 - 4 - 3
	}
	for index, paragraph := range strings.Split(text, "\n\n") {
		if index > 0 {
			out.WriteString(indent + "//\n")
		}
		line := ""
		for _, word := range strings.Fields(paragraph) {
			if line != "" && len(line)+1+len(word) > width {
				out.WriteString(indent + "// " + line + "\n")
				line = ""
			}
			if line == "" {
				line = word
			} else {
				line += " " + word
			}
		}
		if line != "" {
			out.WriteString(indent + "// " + line + "\n")
		}
	}
}
