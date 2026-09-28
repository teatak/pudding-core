package workbench

import (
	"encoding/json"
	"strings"
	"testing"
)

func fixturePackage(t *testing.T) Package {
	t.Helper()
	return Package{Files: map[string]string{
		"workbench.json": `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{"main":{"appID":"fixture","endpoint":"rest"}},"operations":{"items":{"source":"main","kind":"rest","effectHint":"read","inputSchema":{"type":"object","properties":{"id":{"type":"string","maxLength":64}},"required":["id"],"additionalProperties":false},"request":{"method":"GET","path":"/items/{id}","pathParams":{"id":{"$input":"/id"}}}}}}`,
		"src/App.tsx":    "export default function App() { return <p>Items</p>; }",
	}}
}

func TestPackageHashAndRequest(t *testing.T) {
	p := fixturePackage(t)
	m, hash, err := p.Validate()
	if err != nil || len(hash) != 64 {
		t.Fatalf("%s %v", hash, err)
	}
	_, same, _ := p.Validate()
	if same != hash {
		t.Fatal("hash changed without source change")
	}
	r, err := m.Operations["items"].ResolveRequest(map[string]any{"id": "a/b?c"})
	if err != nil || r.Path != "/items/a%2Fb%3Fc" {
		t.Fatalf("%+v %v", r, err)
	}
	for _, input := range []map[string]any{{}, {"id": ".."}, {"id": "x", "host": "evil"}, {"id": strings.Repeat("x", 65)}} {
		if _, err := m.Operations["items"].ResolveRequest(input); err == nil {
			t.Fatalf("accepted invalid input: %+v", input)
		}
	}
	p.Files["src/App.tsx"] += "\n"
	_, changed, _ := p.Validate()
	if changed == hash {
		t.Fatal("source edit did not change hash")
	}
}

func TestPackageRejectsUnsafeAndUnknownDefinitions(t *testing.T) {
	for _, name := range []string{"../escape.ts", "src/../../escape.ts", "/src/App.tsx", "src\\evil.ts", "src/../App.tsx", "package.json", "src/script.sh"} {
		p := fixturePackage(t)
		p.Files[name] = "bad"
		if _, _, err := p.Validate(); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	for _, change := range []func(map[string]any){
		func(m map[string]any) { m["unknown"] = true },
		func(m map[string]any) { m["sdkVersion"] = "999" },
		func(m map[string]any) {
			m["operations"].(map[string]any)["items"].(map[string]any)["inputSchema"].(map[string]any)["$ref"] = "https://evil"
		},
		func(m map[string]any) {
			m["operations"].(map[string]any)["items"].(map[string]any)["request"].(map[string]any)["path"] = "https://evil"
		},
		func(m map[string]any) {
			m["operations"].(map[string]any)["items"].(map[string]any)["request"].(map[string]any)["path"] = "/%2e%2e/secrets"
		},
	} {
		p := fixturePackage(t)
		var m map[string]any
		if err := json.Unmarshal([]byte(p.Files["workbench.json"]), &m); err != nil {
			t.Fatal(err)
		}
		change(m)
		b, _ := json.Marshal(m)
		p.Files["workbench.json"] = string(b)
		if _, _, err := p.Validate(); err == nil {
			t.Fatal("accepted invalid manifest")
		}
	}
}

func TestPointerDoesNotInventMissingData(t *testing.T) {
	data := map[string]any{"a/b": map[string]any{"~key": []any{nil, "value"}}}
	got, err := Pointer(data, "/a~1b/~0key/1")
	if err != nil || got != "value" {
		t.Fatalf("%v %v", got, err)
	}
	for _, p := range []string{"/missing", "/a~1b/~0key/01", "/a~1b/~0key/2", "/a~2b"} {
		if _, err := Pointer(data, p); err == nil {
			t.Fatalf("accepted %s", p)
		}
	}
}

func TestSafeReadRequiresReadTransport(t *testing.T) {
	for _, tc := range []struct {
		op   Operation
		want bool
	}{
		{Operation{Kind: "rest", EffectHint: "read", Request: Request{Method: "GET", Path: "/items"}}, true},
		{Operation{Kind: "rest", EffectHint: "read", Request: Request{Method: "POST", Path: "/items"}}, false},
		{Operation{Kind: "rest", EffectHint: "write", Request: Request{Method: "GET", Path: "/items"}}, false},
		{Operation{Kind: "graphql", EffectHint: "read", Request: Request{Document: "query Dashboard { viewer { login } }"}}, true},
		{Operation{Kind: "graphql", EffectHint: "read", Request: Request{Document: "mutation Update { updateThing { id } }"}}, false},
		{Operation{Kind: "graphql", EffectHint: "read", Request: Request{Document: "query Dashboard { viewer { login } } mutation Update { updateThing { id } }"}}, false},
	} {
		if got := tc.op.SafeRead(); got != tc.want {
			t.Errorf("SafeRead(%+v) = %v, want %v", tc.op, got, tc.want)
		}
	}
}
