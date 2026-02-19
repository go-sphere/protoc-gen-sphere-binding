package binding

import (
	"bytes"
	"fmt"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"maps"
	"os"
	"path/filepath"
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

type Config struct {
	AutoRemoveJson bool
	BindingAliases map[string][]string
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

func GenerateFile(file *protogen.File, out string, config *Config) error {
	err := generateFile(file, out, config)
	if err != nil {
		return err
	}
	return nil
}

// writeFileAtomic writes data to a file atomically using temp file + rename
func writeFileAtomic(filename string, data []byte, perm os.FileMode) error {
	tempFile, cErr := os.CreateTemp(filepath.Dir(filename), ".tmp-*.pb.go")
	if cErr != nil {
		return cErr
	}
	tempName := tempFile.Name()
	defer func() {
		_ = os.Remove(tempName)
	}()
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempName, perm); err != nil {
		return err
	}

	if err := os.Rename(tempName, filename); err != nil {
		return err
	}
	return nil
}

func generateFile(file *protogen.File, out string, config *Config) error {
	tags, err := extractFile(file, config)
	if err != nil {
		return err
	}
	if len(tags) == 0 {
		return nil
	}

	// Validate output path to prevent path traversal
	out = filepath.Clean(out)
	filename := filepath.Join(out, file.GeneratedFilenamePrefix+".pb.go")
	rel, err := filepath.Rel(out, filepath.Clean(filename))
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid file path: potential path traversal")
	}

	// Preserve original file permissions
	originalInfo, err := os.Stat(filename)
	if err != nil {
		return err
	}
	originalPerm := originalInfo.Mode().Perm()

	fs := token.NewFileSet()
	fn, err := parser.ParseFile(fs, filename, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	// Skip if no actual changes needed
	changed := false
	err = ReTagsWithCheck(fn, tags, &changed)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	var buf bytes.Buffer
	err = printer.Fprint(&buf, fs, fn)
	if err != nil {
		return err
	}

	source, err := format.Source(buf.Bytes())
	if err != nil {
		return err
	}

	return writeFileAtomic(filename, source, originalPerm)
}

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

	// process fields
	for _, field := range message.Fields {
		fieldTags, err := extractField(field, location, autoTags, config)
		if err != nil {
			return nil, err
		}
		if fieldTags.Len() > 0 {
			messageTags[field.GoName] = fieldTags
		}
	}

	// process one_of
	for _, oneOf := range message.Oneofs {
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
			if fieldTags.Len() > 0 {
				messageTags[field.GoName] = fieldTags
			}
		}
	}

	// process nested messages
	for _, nested := range message.Messages {
		extraTags, err := extractMessage(nested, location, autoTags, config)
		if err != nil {
			return nil, err
		}
		maps.Copy(tags, extraTags)
	}

	tags[message.GoIdent.GoName] = messageTags
	return tags, nil
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
