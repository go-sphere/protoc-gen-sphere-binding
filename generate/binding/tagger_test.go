package binding

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fatih/structtag"
	"github.com/go-sphere/binding/sphere/binding"
	"github.com/go-sphere/protoc-gen-sphere-binding/generate/internal/testutil"
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
			input: "query=form,uri=path,header=h",
			want:  map[string][]string{"query": {"form"}, "uri": {"path"}, "header": {"h"}},
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
	t.Run("rejects invalid key", func(t *testing.T) {
		for _, badKey := range []string{"bad key", "js`on", "js:on", "js\"on"} {
			tags := &structtag.Tags{}
			if err := setTag(tags, badKey, "name"); err == nil {
				t.Fatalf("setTag(%q) error = nil, want error", badKey)
			}
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

	// The payload oneof sets no location, so it must not inherit QUERY.
	for _, name := range []string{"OneofRequest_Note", "OneofRequest_Detail"} {
		if wrapperTags, ok := tags[name]; ok {
			t.Errorf("oneof wrapper %s inherited the message location: %v", name, wrapperTags)
		}
	}
}

func TestExtractFile_OneofWrapperNameCollision(t *testing.T) {
	set := testutil.LoadDescriptorSet(t, "testdata/pb/oneof.pb")
	plugin := testutil.MustCreatePlugin(t, set, "oneof.proto")
	file := testutil.FileToGenerate(t, plugin)

	tags, err := extractFile(file, DefaultConfig())
	if err != nil {
		t.Fatalf("extractFile: %v", err)
	}
	wrapper := tags["WrapperCollision_Text_"]["Text"]
	if wrapper == nil || !strings.Contains(wrapper.String(), `query:"text"`) {
		t.Fatalf("tags must be keyed by the generated wrapper WrapperCollision_Text_, got %v", tags["WrapperCollision_Text_"])
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.AutoRemoveJSON {
		t.Error("DefaultConfig().AutoRemoveJSON = false, want true")
	}
	if cfg.BindingAliases == nil {
		t.Error("DefaultConfig().BindingAliases = nil, want non-nil")
	}
}

func TestConfigValidate(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("DefaultConfig().Validate() error = %v", err)
	}
	if err := (*Config)(nil).Validate(); err == nil {
		t.Fatal("nil Config.Validate() error = nil")
	}
	for _, cfg := range []*Config{
		{BindingAliases: map[string][]string{"bad key": {"form"}}},
		{BindingAliases: map[string][]string{"query": {"bad alias"}}},
	} {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("Validate(%v) error = nil", cfg.BindingAliases)
		}
	}
}

func TestExtractFile_InvalidTagKeysAndBackticks(t *testing.T) {
	newFile := func(msg *descriptorpb.DescriptorProto) *protogen.File {
		t.Helper()
		fd := &descriptorpb.FileDescriptorProto{
			Name:        proto.String("invalid_tags.proto"),
			Package:     proto.String("tests.invalidtags"),
			Syntax:      proto.String("proto3"),
			Options:     &descriptorpb.FileOptions{GoPackage: proto.String("github.com/example/invalidtags")},
			MessageType: []*descriptorpb.DescriptorProto{msg},
		}
		req := &pluginpb.CodeGeneratorRequest{
			FileToGenerate: []string{"invalid_tags.proto"},
			ProtoFile:      []*descriptorpb.FileDescriptorProto{fd},
		}
		plugin, err := protogen.Options{}.New(req)
		if err != nil {
			t.Fatalf("protogen.Options.New: %v", err)
		}
		return testutil.FileToGenerate(t, plugin)
	}

	t.Run("rejects invalid default_auto_tags key", func(t *testing.T) {
		msgOpts := &descriptorpb.MessageOptions{}
		proto.SetExtension(msgOpts, binding.E_DefaultAutoTags, []string{"bad key"})
		file := newFile(&descriptorpb.DescriptorProto{
			Name:    proto.String("Req"),
			Options: msgOpts,
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:     proto.String("name"),
				Number:   proto.Int32(1),
				JsonName: proto.String("name"),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			}},
		})
		if _, err := extractFile(file, DefaultConfig()); err == nil {
			t.Fatal("expected error for invalid default_auto_tags key")
		}
	})

	t.Run("rejects invalid default_oneof_auto_tags key", func(t *testing.T) {
		oneofOpts := &descriptorpb.OneofOptions{}
		proto.SetExtension(oneofOpts, binding.E_DefaultOneofAutoTags, []string{"bad:key"})
		file := newFile(&descriptorpb.DescriptorProto{
			Name: proto.String("Req"),
			OneofDecl: []*descriptorpb.OneofDescriptorProto{{
				Name:    proto.String("choice"),
				Options: oneofOpts,
			}},
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:       proto.String("text"),
				Number:     proto.Int32(1),
				JsonName:   proto.String("text"),
				OneofIndex: proto.Int32(0),
				Type:       descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			}},
		})
		if _, err := extractFile(file, DefaultConfig()); err == nil {
			t.Fatal("expected error for invalid default_oneof_auto_tags key")
		}
	})

	t.Run("rejects invalid field auto_tags key", func(t *testing.T) {
		fieldOpts := &descriptorpb.FieldOptions{}
		proto.SetExtension(fieldOpts, binding.E_AutoTags, []string{"bad`key"})
		file := newFile(&descriptorpb.DescriptorProto{
			Name: proto.String("Req"),
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:     proto.String("name"),
				Number:   proto.Int32(1),
				JsonName: proto.String("name"),
				Options:  fieldOpts,
				Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			}},
		})
		if _, err := extractFile(file, DefaultConfig()); err == nil {
			t.Fatal("expected error for invalid auto_tags key")
		}
	})

	for _, tc := range []struct {
		name string
		tag  string
	}{
		{name: "backtick in value", tag: "custom:\"a`b\""},
		{name: "escaped backtick in value", tag: `custom:"\x60"`},
		{name: "backtick in key", tag: "a`b:\"c\""},
	} {
		t.Run("rejects E_Tags "+tc.name, func(t *testing.T) {
			fieldOpts := &descriptorpb.FieldOptions{}
			proto.SetExtension(fieldOpts, binding.E_Tags, []string{tc.tag})
			file := newFile(&descriptorpb.DescriptorProto{
				Name: proto.String("Req"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:     proto.String("name"),
					Number:   proto.Int32(1),
					JsonName: proto.String("name"),
					Options:  fieldOpts,
					Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				}},
			})
			if _, err := extractFile(file, DefaultConfig()); err == nil {
				t.Fatalf("expected error for E_Tags %q", tc.tag)
			}
		})
	}

	t.Run("ignores whitespace-only E_Tags", func(t *testing.T) {
		fieldOpts := &descriptorpb.FieldOptions{}
		proto.SetExtension(fieldOpts, binding.E_Tags, []string{"   "})
		file := newFile(&descriptorpb.DescriptorProto{
			Name: proto.String("Req"),
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:     proto.String("name"),
				Number:   proto.Int32(1),
				JsonName: proto.String("name"),
				Options:  fieldOpts,
				Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			}},
		})
		tags, err := extractFile(file, DefaultConfig())
		if err != nil {
			t.Fatalf("unexpected error for whitespace E_Tags: %v", err)
		}
		if len(tags) != 0 {
			t.Fatalf("expected empty tags, got %v", tags)
		}
	})
}
