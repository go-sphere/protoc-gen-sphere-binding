package binding

import (
	"reflect"
	"testing"

	"github.com/fatih/structtag"
	"github.com/go-sphere/binding/sphere/binding"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
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

func TestSetTagsByKeys(t *testing.T) {
	tags := &structtag.Tags{}
	if err := setTagsByKeys(tags, []string{"query", "form"}, "name"); err != nil {
		t.Fatal(err)
	}
	if got, want := tags.String(), `query:"name" form:"name"`; got != want {
		t.Fatalf("setTagsByKeys = %q, want %q", got, want)
	}
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

// TestExtractFile_NoOptions verifies the skip logic: a message whose fields have
// no sphere.binding options contributes no struct tags, so extractFile returns
// an empty map and the plugin leaves the file untouched. This mirrors the
// Layer 1 "hand-written descriptor" approach from TESTING.md (no custom
// extensions, so no descriptor-set dependency).
func TestExtractFile_NoOptions(t *testing.T) {
	fd := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("plain.proto"),
		Package: proto.String("api.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/api/v1;apiv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Plain"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("name"),
						Number: proto.Int32(1),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					},
				},
			},
		},
	}

	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"plain.proto"},
		ProtoFile:      []*descriptorpb.FileDescriptorProto{fd},
	}
	plugin, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatalf("failed to create plugin: %v", err)
	}

	// Safe here: a single hand-written file with no imports is plugin.Files[0].
	tags, err := extractFile(plugin.Files[0], DefaultConfig())
	if err != nil {
		t.Fatalf("extractFile failed: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("expected no struct tags, got %v", tags)
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
