package frontmatter

import (
	"reflect"
	"testing"
)

// corpus pins the flat-parser contract every listing surface shares. The
// Swift suite asserts the same inputs and expectations verbatim in
// FrontmatterTests.testFlatParserCorpus
// (mac-app/Tests/SkillsRegistryCoreTests/CoreContractTests.swift); change
// both tables together.
var corpus = []struct {
	name  string
	lines []string
	want  map[string]string
}{
	{
		name:  "double-quoted",
		lines: []string{`name: "Quoted Name"`},
		want:  map[string]string{"name": "Quoted Name"},
	},
	{
		name:  "single-quoted",
		lines: []string{`description: 'single quoted'`},
		want:  map[string]string{"description": "single quoted"},
	},
	{
		name:  "nested-quotes-strip-fully",
		lines: []string{`name: "''deep''"`},
		want:  map[string]string{"name": "deep"},
	},
	{
		name:  "mismatched-quotes-strip-fully",
		lines: []string{`name: "'mixed'"`},
		want:  map[string]string{"name": "mixed"},
	},
	{
		name:  "unbalanced-quote-strips",
		lines: []string{`name: "abc`},
		want:  map[string]string{"name": "abc"},
	},
	{
		name:  "quote-escapes-not-interpreted",
		lines: []string{`name: 'it''s'`},
		want:  map[string]string{"name": "it''s"},
	},
	{
		name:  "numeric-stays-verbatim",
		lines: []string{"name: 123", "description: 4.5"},
		want:  map[string]string{"name": "123", "description": "4.5"},
	},
	{
		name:  "bool-stays-verbatim",
		lines: []string{"description: true"},
		want:  map[string]string{"description": "true"},
	},
	{
		name:  "flow-list-stays-verbatim",
		lines: []string{"description: [a, b]"},
		want:  map[string]string{"description": "[a, b]"},
	},
	{
		name:  "flow-map-stays-verbatim",
		lines: []string{"description: {a: b}"},
		want:  map[string]string{"description": "{a: b}"},
	},
	{
		name:  "folded-block-scalar",
		lines: []string{"description: >", "  line one", "  line two"},
		want:  map[string]string{"description": "line one line two"},
	},
	{
		name:  "literal-block-scalar",
		lines: []string{"description: |", "  line one", "  line two"},
		want:  map[string]string{"description": "line one\nline two"},
	},
	{
		name:  "plain-multiline-continuation",
		lines: []string{"description: line one", "  line two"},
		want:  map[string]string{"description": "line one line two"},
	},
	{
		name:  "duplicate-keys-last-wins",
		lines: []string{"name: first", "name: second"},
		want:  map[string]string{"name": "second"},
	},
	{
		name:  "comments-and-blanks-skipped",
		lines: []string{"# leading comment", "", "name: kept", "  # indented comment"},
		want:  map[string]string{"name": "kept"},
	},
	{
		name:  "nested-mapping-read-flat",
		lines: []string{"metadata:", "  foo: bar"},
		want:  map[string]string{"metadata": "", "foo": "bar"},
	},
	{
		name:  "empty-value",
		lines: []string{"name:"},
		want:  map[string]string{"name": ""},
	},
	{
		name:  "colon-in-value",
		lines: []string{"description: a: b"},
		want:  map[string]string{"description": "a: b"},
	},
	{
		name:  "comment-after-block-marker",
		lines: []string{"description: > # multi-line", "  folded here"},
		want:  map[string]string{"description": "folded here"},
	},
}

func TestParseCorpus(t *testing.T) {
	for _, c := range corpus {
		if got := Parse(c.lines); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Parse(%q) = %#v, want %#v", c.name, c.lines, got, c.want)
		}
	}
}
