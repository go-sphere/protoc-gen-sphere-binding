package binding

import (
	"slices"
	"strings"
	"testing"

	"github.com/fatih/structtag"
)

// mustTags parses a struct tag literal (without backticks) into structtag.Tags.
func mustTags(t *testing.T, value string) *structtag.Tags {
	t.Helper()
	tags, err := structtag.Parse(value)
	if err != nil {
		t.Fatalf("structtag.Parse(%q): %v", value, err)
	}
	return tags
}

const retagSrc = `package p

type Foo struct {
	Name string ` + "`json:\"name,omitempty\"`" + `
	Age  int    ` + "`json:\"age,omitempty\"`" + `
}

type Bar struct {
	ID string ` + "`json:\"id,omitempty\"`" + `
}
`

func TestRetagSource(t *testing.T) {
	t.Run("adds and overrides tags", func(t *testing.T) {
		tags := StructTags{
			"Foo": {
				"Name": mustTags(t, `query:"name" json:"-"`),
			},
		}
		out, changed, err := RetagSource("foo.go", []byte(retagSrc), tags)
		if err != nil {
			t.Fatal(err)
		}
		if !changed {
			t.Fatal("expected changed = true")
		}
		got := string(out)
		if !strings.Contains(got, `query:"name"`) {
			t.Errorf("missing query tag:\n%s", got)
		}
		if !strings.Contains(got, `json:"-"`) {
			t.Errorf("json tag not overridden to \"-\":\n%s", got)
		}
		// Untouched fields keep their original tags.
		if !strings.Contains(got, `json:"age,omitempty"`) {
			t.Errorf("Age tag should be unchanged:\n%s", got)
		}
		if !strings.Contains(got, `json:"id,omitempty"`) {
			t.Errorf("Bar.ID tag should be unchanged:\n%s", got)
		}
	})

	t.Run("no matching struct leaves source unchanged", func(t *testing.T) {
		tags := StructTags{
			"DoesNotExist": {"Whatever": mustTags(t, `query:"x"`)},
		}
		out, changed, err := RetagSource("foo.go", []byte(retagSrc), tags)
		if err != nil {
			t.Fatal(err)
		}
		if changed {
			t.Fatal("expected changed = false")
		}
		if string(out) != retagSrc {
			t.Errorf("source should be returned verbatim when nothing changes")
		}
	})

	t.Run("identical tag is not a change", func(t *testing.T) {
		tags := StructTags{
			"Foo": {"Name": mustTags(t, `json:"name,omitempty"`)},
		}
		_, changed, err := RetagSource("foo.go", []byte(retagSrc), tags)
		if err != nil {
			t.Fatal(err)
		}
		if changed {
			t.Fatal("setting an identical tag should not count as a change")
		}
	})

	t.Run("invalid go source returns an error", func(t *testing.T) {
		_, _, err := RetagSource("bad.go", []byte("package p\nthis is not go"), StructTags{})
		if err == nil {
			t.Fatal("expected a parse error")
		}
	})

	t.Run("handles quoted tag literals", func(t *testing.T) {
		const quotedSrc = `package p

type Foo struct {
	Name  string "json:\"name,omitempty\""
	Plain string
}
`
		tags := StructTags{
			"Foo": {
				"Name":  mustTags(t, `query:"name"`),
				"Plain": mustTags(t, `form:"plain"`),
			},
		}
		out, changed, err := RetagSource("quoted.go", []byte(quotedSrc), tags)
		if err != nil {
			t.Fatal(err)
		}
		if !changed {
			t.Fatal("expected changed = true")
		}
		got := string(out)
		if !strings.Contains(got, "`json:\"name,omitempty\" query:\"name\"`") {
			t.Errorf("quoted tag not unquoted and updated cleanly:\n%s", got)
		}
		if !strings.Contains(got, "`form:\"plain\"`") {
			t.Errorf("untagged field missing form tag:\n%s", got)
		}
	})

	t.Run("identical tag on quoted literal is not a change", func(t *testing.T) {
		const quotedSrc = `package p

type Foo struct {
	Name string "json:\"name,omitempty\""
}
`
		tags := StructTags{
			"Foo": {"Name": mustTags(t, `json:"name,omitempty"`)},
		}
		out, changed, err := RetagSource("quoted.go", []byte(quotedSrc), tags)
		if err != nil {
			t.Fatal(err)
		}
		if changed {
			t.Fatal("setting an identical tag on a quoted literal should not count as a change")
		}
		if string(out) != quotedSrc {
			t.Errorf("source should be returned verbatim when nothing changes")
		}
	})

	t.Run("does not mutate caller StructTags order", func(t *testing.T) {
		newTags := mustTags(t, `z:"1" a:"2"`)
		wantOrder := slices.Clone(newTags.Keys())
		tags := StructTags{
			"Foo": {"Name": newTags},
		}
		out, changed, err := RetagSource("foo.go", []byte(retagSrc), tags)
		if err != nil {
			t.Fatal(err)
		}
		if !changed {
			t.Fatal("expected changed = true")
		}
		got := string(out)
		if !strings.Contains(got, `a:"2" z:"1"`) {
			t.Errorf("expected sorted tags in output:\n%s", got)
		}
		if !slices.Equal(newTags.Keys(), wantOrder) {
			t.Fatalf("caller StructTags mutated: got keys %v, want %v", newTags.Keys(), wantOrder)
		}
	})
}
