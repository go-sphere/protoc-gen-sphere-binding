package binding

import (
	"fmt"
	"maps"
	"strings"

	"github.com/fatih/structtag"
	"github.com/go-sphere/binding/sphere/binding"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var noJsonBinding = map[binding.BindingLocation]string{
	binding.BindingLocation_BINDING_LOCATION_QUERY:  "query",
	binding.BindingLocation_BINDING_LOCATION_URI:    "uri",
	binding.BindingLocation_BINDING_LOCATION_FORM:   "form",
	binding.BindingLocation_BINDING_LOCATION_HEADER: "header",
}

// scalarBindLocations are the binding locations whose value is decoded from a
// single string token. A map, bytes or arbitrary message field has no scalar
// text form, so tagging it for one of these locations produces a struct tag the
// runtime binder cannot satisfy. FORM is intentionally excluded: form data can
// legitimately carry files/bytes. Kept in sync with protoc-gen-sphere's
// parser.checkScalarBindable.
var scalarBindLocations = map[binding.BindingLocation]bool{
	binding.BindingLocation_BINDING_LOCATION_QUERY:  true,
	binding.BindingLocation_BINDING_LOCATION_URI:    true,
	binding.BindingLocation_BINDING_LOCATION_HEADER: true,
}

type Config struct {
	AutoRemoveJson bool
	BindingAliases map[string][]string
}

// DefaultConfig returns the configuration the plugin uses out of the box, i.e.
// the same defaults main.go wires from its flags: json tags are removed for
// non-JSON binding locations and no extra tag aliases are registered. Tests use
// it so golden files stay representative of real output.
func DefaultConfig() *Config {
	return &Config{
		AutoRemoveJson: true,
		BindingAliases: map[string][]string{},
	}
}

// ValidateTagKey validates if a tag key is valid for Go struct tags
func ValidateTagKey(key string) error {
	if len(key) == 0 {
		return fmt.Errorf("tag key cannot be empty")
	}
	if strings.ContainsAny(key, " \t\n\r`:\"") {
		return fmt.Errorf("tag key '%s' contains illegal characters", key)
	}
	return nil
}

// ParseBindingAliases parses and validates binding aliases from a comma-separated string
func ParseBindingAliases(aliasStr string) (map[string][]string, error) {
	aliases := make(map[string][]string)
	if aliasStr == "" {
		return aliases, nil
	}

	for _, alias := range strings.Split(aliasStr, ",") {
		alias = strings.TrimSpace(alias)
		if len(alias) == 0 {
			continue
		}

		kv := strings.Split(alias, "=")
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid binding alias format '%s': expected 'key=value'", alias)
		}

		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])

		if err := ValidateTagKey(key); err != nil {
			return nil, fmt.Errorf("invalid binding alias '%s': %w", alias, err)
		}

		if err := ValidateTagKey(value); err != nil {
			return nil, fmt.Errorf("invalid binding alias '%s': %w", alias, err)
		}

		aliases[key] = append(aliases[key], value)
	}

	return aliases, nil
}

// extractFile walks every top-level message in file and collects the struct
// tags that should be applied to the generated Go structs. It is pure: it only
// reads the descriptor and never touches the filesystem.
func extractFile(file *protogen.File, config *Config) (StructTags, error) {
	tags := make(StructTags)
	for _, message := range file.Messages {
		extraTags, err := extractMessage(message, binding.BindingLocation_BINDING_LOCATION_UNSPECIFIED, nil, config)
		if err != nil {
			return nil, err
		}
		for name, tag := range extraTags {
			if len(tag) > 0 {
				tags[name] = tag
			}
		}
	}
	return tags, nil
}

func resolveLocationAndAutoTags(
	options proto.Message,
	locationExt protoreflect.ExtensionType,
	autoTagsExt protoreflect.ExtensionType,
	defaultLocation binding.BindingLocation,
	defaultAutoTags []string,
) (binding.BindingLocation, []string) {
	location := defaultLocation
	if proto.HasExtension(options, locationExt) {
		location = proto.GetExtension(options, locationExt).(binding.BindingLocation)
	}

	autoTags := defaultAutoTags
	if proto.HasExtension(options, autoTagsExt) {
		autoTags = proto.GetExtension(options, autoTagsExt).([]string)
	}

	return location, autoTags
}

func setTag(tags *structtag.Tags, key, name string) error {
	if key == "" {
		return nil
	}
	return tags.Set(&structtag.Tag{
		Key:     key,
		Name:    name,
		Options: nil,
	})
}

func setTagsByKeys(tags *structtag.Tags, keys []string, name string) error {
	for _, key := range keys {
		if err := setTag(tags, key, name); err != nil {
			return err
		}
	}
	return nil
}

func extractMessage(message *protogen.Message, location binding.BindingLocation, autoTags []string, config *Config) (StructTags, error) {
	tags := make(StructTags)

	location, autoTags = resolveLocationAndAutoTags(
		message.Desc.Options(),
		binding.E_DefaultLocation,
		binding.E_DefaultAutoTags,
		location,
		autoTags,
	)

	messageTags := make(map[string]*structtag.Tags)

	// process fields (skip real oneof members; they are tagged on wrappers below)
	for _, field := range message.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		fieldTags, err := extractField(field, location, autoTags, config)
		if err != nil {
			return nil, err
		}
		if fieldTags.Len() > 0 {
			messageTags[field.GoName] = fieldTags
		}
	}

	// process one_of (skip proto3 optional synthetic oneofs; those fields
	// were already tagged in the parent loop above)
	for _, oneOf := range message.Oneofs {
		if oneOf.Desc.IsSynthetic() {
			continue
		}
		oneOfLocation, oneOfAutoTags := resolveLocationAndAutoTags(
			oneOf.Desc.Options(),
			binding.E_DefaultOneofLocation,
			binding.E_DefaultOneofAutoTags,
			location,
			autoTags,
		)

		for _, field := range oneOf.Fields {
			fieldTags, err := extractField(field, oneOfLocation, oneOfAutoTags, config)
			if err != nil {
				return nil, err
			}
			if fieldTags.Len() == 0 {
				continue
			}
			// protoc-gen-go emits every oneof member in its own wrapper struct
			// named `{ParentMessage}_{Field}` with a single field `{Field}`, so
			// the tag must be keyed by that wrapper struct rather than the parent
			// message (which has no such field).
			wrapperName := message.GoIdent.GoName + "_" + field.GoName
			tags[wrapperName] = map[string]*structtag.Tags{field.GoName: fieldTags}
		}
	}

	// Nested type definitions do not inherit QUERY/URI/HEADER from the
	// enclosing message: a JSON body field of a nested type would otherwise
	// get json:"-" plus query tags. Map entries are skipped entirely.
	for _, nested := range message.Messages {
		if nested.Desc.IsMapEntry() {
			continue
		}
		extraTags, err := extractMessage(nested, binding.BindingLocation_BINDING_LOCATION_UNSPECIFIED, nil, config)
		if err != nil {
			return nil, err
		}
		maps.Copy(tags, extraTags)
	}

	tags[message.GoIdent.GoName] = messageTags
	return tags, nil
}

// isScalarBindable reports whether field can be bound from a single string token
// (query/uri/header). Maps and bytes cannot; message fields are only allowed
// when they are well-known scalar wrappers (Timestamp/Duration/wrapperspb.*Value).
func isScalarBindable(field *protogen.Field) bool {
	if field.Desc.IsMap() {
		return false
	}
	switch field.Desc.Kind() {
	case protoreflect.BytesKind:
		return false
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return isWellKnownScalarMessage(field)
	default:
		return true
	}
}

// isWellKnownScalarMessage reports whether a message field is one of the
// well-known types that carry a natural scalar representation and can therefore
// be decoded from a single string token.
func isWellKnownScalarMessage(field *protogen.Field) bool {
	if field.Message == nil {
		return false
	}
	switch field.Message.Desc.FullName() {
	case "google.protobuf.Timestamp", "google.protobuf.Duration",
		"google.protobuf.StringValue", "google.protobuf.BytesValue",
		"google.protobuf.BoolValue",
		"google.protobuf.DoubleValue", "google.protobuf.FloatValue",
		"google.protobuf.Int32Value", "google.protobuf.UInt32Value",
		"google.protobuf.Int64Value", "google.protobuf.UInt64Value":
		return true
	}
	return false
}

// fieldKindDesc returns a human-readable description of a field's type for use
// in error messages (e.g. "map", "bytes", "message").
func fieldKindDesc(field *protogen.Field) string {
	switch {
	case field.Desc.IsMap():
		return "map"
	case field.Desc.Kind() == protoreflect.BytesKind:
		return "bytes"
	case field.Desc.Kind() == protoreflect.MessageKind || field.Desc.Kind() == protoreflect.GroupKind:
		return "message"
	default:
		return field.Desc.Kind().String()
	}
}

func extractField(field *protogen.Field, location binding.BindingLocation, autoTags []string, config *Config) (*structtag.Tags, error) {
	location, autoTags = resolveLocationAndAutoTags(
		field.Desc.Options(),
		binding.E_Location,
		binding.E_AutoTags,
		location,
		autoTags,
	)

	fieldTags := &structtag.Tags{}
	fieldName := string(field.Desc.Name())

	// Add auto tags
	if err := setTagsByKeys(fieldTags, autoTags, fieldName); err != nil {
		return nil, err
	}

	// Add sphere binding tags
	if tag, ok := noJsonBinding[location]; ok {
		if scalarBindLocations[location] && !isScalarBindable(field) {
			return nil, fmt.Errorf("field `%s` of type `%s` cannot be bound to %q: only scalar types (and well-known scalar wrappers) are supported there",
				field.Desc.FullName(),
				fieldKindDesc(field),
				tag,
			)
		}
		if err := setTag(fieldTags, tag, fieldName); err != nil {
			return nil, err
		}
		if aliases, exist := config.BindingAliases[tag]; exist {
			if err := setTagsByKeys(fieldTags, aliases, fieldName); err != nil {
				return nil, err
			}
		}
		if config.AutoRemoveJson {
			if err := setTag(fieldTags, "json", "-"); err != nil {
				return nil, err
			}
		}
	}

	// Manual tags override all previous settings
	if proto.HasExtension(field.Desc.Options(), binding.E_Tags) {
		tags := proto.GetExtension(field.Desc.Options(), binding.E_Tags).([]string)
		for _, tag := range tags {
			if len(tag) == 0 {
				continue
			}
			parse, err := structtag.Parse(tag)
			if err != nil {
				return nil, err
			}
			for _, t := range parse.Tags() {
				if err = fieldTags.Set(t); err != nil {
					return nil, err
				}
			}
		}
	}
	return fieldTags, nil
}
