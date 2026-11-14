package main

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestTheCommittedTypesMatchTheContract(t *testing.T) {
	contract, err := os.ReadFile("../../../openapi/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	generated, err := Generate(contract)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("../../../types.gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, committed) {
		t.Error("types.gen.go is out of date: run go run ./internal/cmd/openapi generate")
	}
	again, _ := Generate(contract)
	if !bytes.Equal(generated, again) {
		t.Error("the generator is not deterministic")
	}
}

// document is a minimal OpenAPI document with these schemas, and paths when given.
func document(schemas string, paths ...string) []byte {
	path := `{}`
	if len(paths) > 0 {
		path = paths[0]
	}
	return []byte(`{"openapi":"3.1.0","paths":` + path + `,"components":{"schemas":` + schemas + `}}`)
}

func TestTheGeneratorRefusesWhatItDoesNotKnow(t *testing.T) {
	cases := map[string]struct {
		contract []byte
		message  string
	}{
		"not JSON":                {[]byte(`{`), "not JSON"},
		"OpenAPI 3.0":             {[]byte(`{"openapi":"3.0.3","components":{"schemas":{}}}`), "only OpenAPI 3.1"},
		"no schemas":              {[]byte(`{"openapi":"3.1.0"}`), "no components.schemas"},
		"unknown keyword":         {document(`{"A":{"type":"object","properties":{"a":{"type":"string","x-new":1}}}}`), "unsupported keyword x-new"},
		"unknown reference":       {document(`{"A":{"type":"object","properties":{"a":{"$ref":"#/components/schemas/B"}}}}`), "unknown reference"},
		"ref with siblings":       {document(`{"A":{"type":"object","properties":{"a":{"$ref":"#/components/schemas/A","type":"object"}}}}`), "$ref with sibling keywords"},
		"allOf of two":            {document(`{"B":{"type":"object","properties":{}},"A":{"type":"object","properties":{"a":{"allOf":[{"$ref":"#/components/schemas/B"},{"$ref":"#/components/schemas/B"}]}}}}`), "allOf is only supported with one member"},
		"anyOf of two values":     {document(`{"A":{"type":"object","properties":{"a":{"anyOf":[{"type":"string"},{"type":"integer"}]}}}}`), "anyOf is only supported as one schema or null"},
		"inline oneOf":            {document(`{"A":{"type":"object","properties":{"a":{"oneOf":[{"type":"string"}]}}}}`), "oneOf is only supported as a component"},
		"several types":           {document(`{"A":{"type":"object","properties":{"a":{"type":["string","integer"]}}}}`), "several non-null types"},
		"number enum":             {document(`{"A":{"type":"object","properties":{"a":{"type":"integer","enum":[1,2]}}}}`), "enum values must be strings"},
		"empty enum":              {document(`{"A":{"type":"object","properties":{"a":{"type":"string","enum":[null]}}}}`), "an enum with no values"},
		"binary":                  {document(`{"A":{"type":"object","properties":{"a":{"type":"string","format":"binary"}}}}`), "binary strings"},
		"array without items":     {document(`{"A":{"type":"object","properties":{"a":{"type":"array"}}}}`), "an array without items"},
		"required without schema": {document(`{"A":{"type":"object","required":["b"],"properties":{"a":{"type":"string"}}}}`), "the required member b has no schema"},
		"typed extra members":     {document(`{"A":{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":{"type":"string"}}}`), "typed members with typed additionalProperties"},
		"bad not":                 {document(`{"A":{"type":"object","properties":{"a":{"type":"string"}},"not":{"type":"string"}}}`), "only supported as not.required"},
		"forbidden and declared":  {document(`{"A":{"type":"object","properties":{"a":{"type":"string"}},"not":{"required":["a"]}}}`), "both declared and forbidden"},
		"backtick in a member":    {document(`{"A":{"type":"object","properties":{"x\u0060}; func init() { panic(1) }; type B struct { Y int \u0060":{"type":"string"}}}}`), "characters other than letters"},
		"quote in a query":        {document(`{}`, `{"/x":{"get":{"operationId":"xList","parameters":[{"name":"a\"b","in":"query","schema":{"type":"string"}}],"responses":{}}}}`), "characters other than letters"},
		"comma in a member":       {document(`{"A":{"type":"object","properties":{"a,omitempty":{"type":"string"}}}}`), "characters other than letters"},
		"name clash":              {document(`{"A":{"type":"object","properties":{"issuerId":{"type":"string"},"issuer_id":{"type":"string"}}}}`), "both IssuerID in Go"},
		"type clash":              {document(`{"IssuerStatus":{"type":"object","properties":{}},"Issuer":{"type":"object","properties":{"status":{"type":"string","enum":["a"]}}}}`), "two definitions are named IssuerStatus"},
		"constant clash":          {document(`{"A":{"type":"string","enum":["a-b","a_b"]}}`), "like a constant of A"},
		"other union":             {document(`{"U":{"oneOf":[{"$ref":"#/components/schemas/U"}]}}`), "the only discriminated union supported is Event"},
		"plain component":         {document(`{"A":{"type":"string"}}`), "only objects, enums and discriminated unions"},
		"required query":          {document(`{}`, `{"/x":{"get":{"operationId":"xList","parameters":[{"name":"q","in":"query","required":true,"schema":{"type":"string"}}],"responses":{}}}}`), "required query parameters"},
		"array query":             {document(`{}`, `{"/x":{"get":{"operationId":"xList","parameters":[{"name":"q","in":"query","schema":{"type":"array","items":{"type":"string"}}}],"responses":{}}}}`), "only strings, numbers, booleans and enums"},
		"no operationId":          {document(`{}`, `{"/x":{"get":{"responses":{}}}}`), "has no operationId"},
		"sent and received":       {document(`{"A":{"type":"object","properties":{}}}`, `{"/x":{"post":{"operationId":"x","requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/A"}}}},"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/A"}}}}}}}}`), "both sent and received"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := generate(tc.contract, nil, nil)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Errorf("got %v, want %q", err, tc.message)
			}
		})
	}
}

func TestWhatTheGeneratorWrites(t *testing.T) {
	contract := document(`{
		"Thing":{"type":"object","description":"A thing.","required":["id","tags","kind","at"],"properties":{
			"id":{"type":"string","format":"uuid"},
			"tags":{"type":"array","items":{"type":["string","null"]}},
			"kind":{"type":"string","enum":["a_b","c.d","*"]},
			"at":{"type":["string","null"],"format":"date-time"},
			"count":{"type":"integer","default":3},
			"ratio":{"type":"number"},
			"flags":{"type":"object","additionalProperties":{"type":"boolean"}},
			"free":{"type":"object"},
			"anything":{"description":"Any value."},
			"label":{"const":"x"},
			"old":{"type":"boolean","deprecated":true}}},
		"Patch":{"type":"object","properties":{"name":{"type":["string","null"]},"nested":{"anyOf":[{"$ref":"#/components/schemas/Nested"},{"type":"null"}]}}},
		"Nested":{"type":"object","properties":{"x":{"type":"string"}}}
	}`, `{"/patch":{"patch":{"operationId":"patch","requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Patch"}}}},"responses":{}}}}`)
	source, err := generate(contract, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// gofmt aligns the columns: compare with single spaces.
	text := strings.Join(strings.Fields(string(source)), " ")
	for _, want := range []string{
		"// Code generated by internal/cmd/openapi from openapi/openapi.json. DO NOT EDIT.",
		"ID string `json:\"id\"`",
		"Tags []*string `json:\"tags\"`",
		"Kind ThingKind `json:\"kind\"`",
		"At *time.Time `json:\"at\"`",
		"Count *int `json:\"count,omitempty\"`",
		"// Default: 3.",
		"Ratio *float64 `json:\"ratio,omitempty\"`",
		"Flags map[string]bool `json:\"flags,omitempty\"`",
		"Free map[string]any `json:\"free,omitempty\"`",
		"Anything any `json:\"anything,omitempty\"`",
		"Label *string `json:\"label,omitempty\"`",
		"// Deprecated: the API marks it as deprecated.",
		"ThingKindAB ThingKind = \"a_b\"",
		"ThingKindCD ThingKind = \"c.d\"",
		"ThingKindAll ThingKind = \"*\"",
		"Name Nullable[string] `json:\"name,omitzero\"`",
		"Nested Nullable[Nested] `json:\"nested,omitzero\"`",
		"// Thing: A thing.",
	} {
		if !strings.Contains(text, strings.Join(strings.Fields(want), " ")) {
			t.Errorf("the output has no %q", want)
		}
	}
}

func TestGoNames(t *testing.T) {
	cases := map[string]string{
		"issuerId":                       "IssuerID",
		"sriMessages":                    "SRIMessages",
		"fingerprintSha256":              "FingerprintSHA256",
		"rucNeedsConfirmation":           "RUCNeedsConfirmation",
		"IssuerOnboardingIssuingCa":      "IssuerOnboardingIssuingCA",
		"webhookEndpointsDeliveriesList": "WebhookEndpointsDeliveriesList",
		"HTTPStatus":                     "HTTPStatus",
		"pending_sri":                    "PendingSRI",
		"url":                            "URL",
	}
	for input, want := range cases {
		if got, err := goName(input); err != nil || got != want {
			t.Errorf("goName(%q) = %q %v, want %q", input, got, err, want)
		}
	}
	for _, bad := range []string{"", "_", "1abc"} {
		if _, err := goName(bad); err == nil {
			t.Errorf("goName(%q) did not fail", bad)
		}
	}
	if suffix, err := constantSuffix("document.email.sent"); err != nil || suffix != "DocumentEmailSent" {
		t.Errorf("got %q %v", suffix, err)
	}
	if _, err := constantSuffix("--"); err == nil {
		t.Error("an enum value without letters was named")
	}
}

func TestParseJSONKeepsTheOrder(t *testing.T) {
	root, err := parseJSON([]byte(`{"b":1,"a":[true,null,"x"],"c":{"z":1,"y":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(root.keys(), ",") != "b,a,c" || strings.Join(root.get("c").keys(), ",") != "z,y" {
		t.Errorf("keys = %v", root.keys())
	}
	if root.get("a").items[0].literal() != "true" || root.get("a").items[1].literal() != "null" || root.get("a").items[2].literal() != `"x"` || root.get("b").literal() != "1" {
		t.Error("literals")
	}
	for _, bad := range []string{`{"a":1,"a":2}`, `{"a":1} 2`, `[1,`} {
		if _, err := parseJSON([]byte(bad)); err == nil {
			t.Errorf("%s was parsed", bad)
		}
	}
}

func TestRunNeedsTheModuleRoot(t *testing.T) {
	if err := run([]string{"check", "extra"}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("got %v", err)
	}
	if err := run([]string{"check"}); err == nil || !strings.Contains(err.Error(), "root") {
		t.Errorf("got %v", err)
	}
	if isHTTPS("http://flashpi.com.ec/docs/openapi.json") || !isHTTPS("https://flashpi.com.ec/docs/openapi.json") || isHTTPS("https://") {
		t.Error("isHTTPS")
	}
}

func TestTheTablesOfTheGenerator(t *testing.T) {
	contract := document(`{
		"A":{"type":"object","properties":{"mode":{"type":"string","enum":["x","y"]},"n":{"type":"number"}}},
		"B":{"type":"object","properties":{"mode":{"type":"string","enum":["x","y"]},"tags":{"type":"array","items":{"type":"string","enum":["x","y"]}}}}
	}`, `{"/l":{"get":{"operationId":"bList","parameters":[{"name":"mode","in":"query","schema":{"type":"string","enum":["x","y"]}}],"responses":{}}}}`)
	all := []string{"A.mode", "B.mode", "B.tags.items", "bList.mode"}
	source, err := generate(contract, []sharedSet{{"Mode", []string{"x", "y"}, all}}, []string{"A.n"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(source)), " ")
	for _, want := range []string{"Mode *Mode `json:\"mode,omitempty\"`", "Tags []Mode", "N *int `json:\"n,omitempty\"`", "type Mode string"} {
		if !strings.Contains(text, want) {
			t.Errorf("the output has no %q", want)
		}
	}
	failures := map[string]struct {
		shared   []sharedSet
		integers []string
		message  string
	}{
		"a member left out":       {[]sharedSet{{"Mode", []string{"x", "y"}, all[:3]}}, nil, "bList.mode: it has the values of Mode"},
		"values that changed":     {[]sharedSet{{"Mode", []string{"x", "y", "z"}, all}}, nil, "no longer those of Mode"},
		"a member that is gone":   {[]sharedSet{{"Mode", []string{"x", "y"}, append(slices.Clone(all), "C.mode")}}, nil, "C.mode is listed in sharedEnums"},
		"an integer that is not":  {nil, []string{"A.mode"}, "A.mode is no longer a number"},
		"an integer that is gone": {[]sharedSet{{"Mode", []string{"x", "y"}, all}}, []string{"A.gone"}, "A.gone is listed in integerMembers"},
	}
	for name, tc := range failures {
		if _, err := generate(contract, tc.shared, tc.integers); err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Errorf("%s: got %v, want %q", name, err, tc.message)
		}
	}
}
