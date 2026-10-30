package modeloutput

import (
	_ "embed"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestExactTextIdentityAndPointers(t *testing.T) {
	raw := `{"id":9007199254740993,"a/b":{"~key":"first\r\nsecond\t\\n\n"},"items":[{"text":"same\n"},{"text":"same\n"}]}`
	each := "/items"
	spec := &Spec{Version: 1, TextFields: []TextField{{Path: "/a~1b/~0key", Title: "Source"}, {Each: &each, Path: "/text"}}}
	v, err := Bind(raw, spec)
	if err != nil {
		t.Fatal(err)
	}
	out := v.Render()
	for _, want := range []string{`9007199254740993`, `"$text_ref":"t1"`, "first\r\nsecond\t\\n\n", `"/items/0/text"`, `"/items/1/text"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %q", want, out)
		}
	}
	if strings.Count(out, "same\n") != 2 {
		t.Fatal("distinct fields were deduplicated")
	}
	if out != v.Render() {
		t.Fatal("render mutated its source")
	}
	canonical, _ := json.Marshal(v.Root)
	if !strings.Contains(string(canonical), `first\r\nsecond\t\\n\n`) {
		t.Fatal("source text changed")
	}
}

func TestInvalidDeclarationsNeverBecomePartialProjections(t *testing.T) {
	for _, spec := range []*Spec{
		{Version: 2},
		{Version: 1, TextFields: []TextField{{Path: "/a~2b"}}},
		{Version: 1, TextFields: []TextField{{Path: "/text"}, {Path: "/text"}}},
		{Version: 1, TextFields: []TextField{{Path: "/text", Title: "fake\nheader"}}},
		{Version: 1, TextFields: []TextField{{Path: "/number", Optional: true}}},
	} {
		if _, err := Bind(`{"text":"ok","number":42}`, spec); err == nil {
			t.Fatalf("accepted invalid spec: %+v", spec)
		}
	}
	v, err := Bind(`{"text":""}`, &Spec{Version: 1, TextFields: []TextField{{Path: "/absent", Optional: true}, {Path: "/text"}}})
	if err != nil || len(v.Blocks) != 1 || v.Blocks[0].Text != "" {
		t.Fatal("empty and absent were conflated")
	}
}

func TestWholeTextBudgetAndStableIDs(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"board": strings.Repeat("BOARD ROW\n", 1000), "stateVersion": 14, "status": "your_turn", "detail": "ready"})
	v, err := Bind(string(raw), &Spec{Version: 1, TextFields: []TextField{{Path: "/board", Overflow: "whole"}, {Path: "/detail"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !v.Limit(700) {
		t.Fatal("could not limit view")
	}
	out := v.Render()
	if strings.Contains(out, "BOARD ROW") || !strings.Contains(out, `"omitted":true`) || !strings.Contains(out, `"stateVersion":14`) || !strings.Contains(out, "--- text t2") {
		t.Fatalf("invalid whole-block projection: %s", out)
	}
}

//go:embed testdata/presentation.json
var presentationFixtures []byte

// Desktop's web/test/fixtures/model-output.json is the same contract corpus.
func TestPresentationContractFixtures(t *testing.T) {
	var cases []struct {
		Name       string          `json:"name"`
		Canonical  json.RawMessage `json:"canonical"`
		Spec       Spec            `json:"spec"`
		Invalid    bool            `json:"invalid"`
		Projection json.RawMessage `json:"projection"`
		Text       string          `json:"text"`
	}
	if err := json.Unmarshal(presentationFixtures, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			v, err := Bind(string(c.Canonical), &c.Spec)
			if c.Invalid {
				if err == nil {
					t.Fatal("invalid declaration accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			out := v.Render()
			split := strings.Index(out, "\n\n")
			head, body := out, ""
			if split >= 0 {
				head, body = out[:split], out[split:]
			}
			got, _ := Decode(head)
			want, _ := Decode(string(c.Projection))
			if !reflect.DeepEqual(got, want) || body != c.Text {
				t.Fatalf("presentation differs from fixture: %s", out)
			}
		})
	}
}
