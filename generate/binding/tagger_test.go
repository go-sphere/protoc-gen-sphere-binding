package binding

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fatih/structtag"
	"github.com/go-sphere/binding/sphere/binding"
	"github.com/go-sphere/protoc-gen-sphere-binding/generate/internal/testutil"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestValidateTagKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"simple", "json", false},
		{"with_underscore", "auto_tags", false},
		{"empty", "", true},
		{"space", "json tag", true},
		{"colon", "js:on", true},
		{"quote", "js\"on", true},
		{"backtick", "js`on", true},
		{"tab", "js\ton", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTagKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateTagKey(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
			}
		})
	}
}

func TestParseBindingAliases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string][]string
		wantErr bool
	}{
		{"empty", "", map[string][]string{}, false},
		{
			name:  "single",
			input: "query=form",
			want:  map[string][]string{"query": {"form"}},
		},
		{
			name:  "multiple",
			input: "query=form,uri=path,db=database",
			want:  map[string][]string{"query": {"form"}, "uri": {"path"}, "db": {"database"}},
		},
		{
			name:  "repeated_key_accumulates",
			input: "query=form,query=extra",
			want:  map[string][]string{"query": {"form", "extra"}},
		},
		{
			name:  "trims_spaces_and_skips_blanks",
			input: " query = form , , uri=path ",
			want:  map[string][]string{"query": {"form"}, "uri": {"path"}},
		},
		{"missing_value", "query", nil, true},
		{"too_many_parts", "query=form=extra", nil, true},
		{"invalid_key", "in valid=form", nil, true},
		{"invalid_value", "query=fo rm", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBindingAliases(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseBindingAliases(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseBindingAliases(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSetTag(t *testing.T) {
	t.Run("sets a key", func(t *testing.T) {
		tags := &structtag.Tags{}
		if err := setTag(tags, "query", "name"); err != nil {
			t.Fatal(err)
		}
		if got, want := tags.String(), `query:"name"`; got != want {
			t.Fatalf("setTag = %q, want %q", got, want)
		}
	})
	t.Run("empty key is a no-op", func(t *testing.T) {
		tags := &structtag.Tags{}
		if err := setTag(tags, "", "name"); err != nil {
			t.Fatal(err)
		}
		if tags.Len() != 0 {
			t.Fatalf("expected no tags, got %q", tags.String())
		}
	})
}

func TestResolveLocationAndAutoTags(t *testing.T) {
	const defaultLoc = binding.BindingLocation_BINDING_LOCATION_QUERY

	t.Run("falls back to defaults when unset", func(t *testing.T) {
		opts := &descriptorpb.MessageOptions{}
		loc, autoTags := resolveLocationAndAutoTags(
			opts, binding.E_DefaultLocation, binding.E_DefaultAutoTags,
			defaultLoc, []string{"validate"},
		)
		if loc != defaultLoc {
			t.Fatalf("location = %v, want %v", loc, defaultLoc)
		}
		if !reflect.DeepEqual(autoTags, []string{"validate"}) {
			t.Fatalf("autoTags = %v, want [validate]", autoTags)
		}
	})

	t.Run("uses extension values when present", func(t *testing.T) {
		opts := &descriptorpb.MessageOptions{}
		proto.SetExtension(opts, binding.E_DefaultLocation, binding.BindingLocation_BINDING_LOCATION_URI)
		proto.SetExtension(opts, binding.E_DefaultAutoTags, []string{"form", "db"})

		loc, autoTags := resolveLocationAndAutoTags(
			opts, binding.E_DefaultLocation, binding.E_DefaultAutoTags,
			defaultLoc, nil,
		)
		if loc != binding.BindingLocation_BINDING_LOCATION_URI {
			t.Fatalf("location = %v, want URI", loc)
		}
		if !reflect.DeepEqual(autoTags, []string{"form", "db"}) {
			t.Fatalf("autoTags = %v, want [form db]", autoTags)
		}
	})
}

func TestExtractFile_NestedDoesNotInheritParentQuery(t *testing.T) {
	set := testutil.LoadDescriptorSet(t, "testdata/pb/oneof.pb")
	plugin := testutil.MustCreatePlugin(t, set, "oneof.proto")
	file := testutil.FileToGenerate(t, plugin)

	tags, err := extractFile(file, DefaultConfig())
	if err != nil {
		t.Fatalf("extractFile: %v", err)
	}

	parent := tags["OneofRequest"]
	if parent == nil {
		t.Fatal("expected tags for OneofRequest")
	}
	outer := parent["Outer"]
	if outer == nil || !strings.Contains(outer.String(), `query:"outer"`) {
		t.Fatalf("parent Outer should keep QUERY tags, got %v", outer)
	}

	if filterMsg, ok := tags["OneofRequest_Filter"]; ok {
		for field, fieldTags := range filterMsg {
			got := fieldTags.String()
			if strings.Contains(got, "query:") || strings.Contains(got, `json:"-"`) {
				t.Errorf("nested Filter.%s inherited parent QUERY tags: %s", field, got)
			}
		}
	}

	byName := tags["OneofRequest_ByName"]
	if byName == nil {
		t.Fatal("expected tags for OneofRequest_ByName wrapper")
	}
	wrapper := byName["ByName"]
	if wrapper == nil || !strings.Contains(wrapper.String(), `uri:"by_name"`) {
		t.Fatalf("oneof wrapper ByName should be tagged on OneofRequest_ByName, got %v", wrapper)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.AutoRemoveJson {
		t.Error("DefaultConfig().AutoRemoveJson = false, want true")
	}
	if cfg.BindingAliases == nil {
		t.Error("DefaultConfig().BindingAliases = nil, want non-nil")
	}
}
