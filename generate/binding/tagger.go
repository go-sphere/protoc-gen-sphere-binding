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
	if !strings.HasPrefix(filepath.Clean(filename), out) {
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

// getLocationAndAutoTags extracts location and autoTags from options
func getLocationAndAutoTags(hasLocation bool, locationValue binding.BindingLocation,
	hasAutoTags bool, autoTagsValue []string,
	defaultLocation binding.BindingLocation, defaultAutoTags []string) (binding.BindingLocation, []string) {

	location := defaultLocation
	if hasLocation {
		location = locationValue
	}

	autoTags := defaultAutoTags
	if hasAutoTags {
		autoTags = autoTagsValue
	}

	return location, autoTags
}

func extractMessage(message *protogen.Message, location binding.BindingLocation, autoTags []string, config *Config) (StructTags, error) {
	tags := make(StructTags)

	location, autoTags = getLocationAndAutoTags(
		proto.HasExtension(message.Desc.Options(), binding.E_DefaultLocation),
		proto.GetExtension(message.Desc.Options(), binding.E_DefaultLocation).(binding.BindingLocation),
		proto.HasExtension(message.Desc.Options(), binding.E_DefaultAutoTags),
		proto.GetExtension(message.Desc.Options(), binding.E_DefaultAutoTags).([]string),
		location, autoTags,
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
		oneOfLocation, oneOfAutoTags := getLocationAndAutoTags(
			proto.HasExtension(oneOf.Desc.Options(), binding.E_DefaultOneofLocation),
			proto.GetExtension(oneOf.Desc.Options(), binding.E_DefaultOneofLocation).(binding.BindingLocation),
			proto.HasExtension(oneOf.Desc.Options(), binding.E_DefaultOneofAutoTags),
			proto.GetExtension(oneOf.Desc.Options(), binding.E_DefaultOneofAutoTags).([]string),
			location, autoTags,
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
	location, autoTags = getLocationAndAutoTags(
		proto.HasExtension(field.Desc.Options(), binding.E_Location),
		proto.GetExtension(field.Desc.Options(), binding.E_Location).(binding.BindingLocation),
		proto.HasExtension(field.Desc.Options(), binding.E_AutoTags),
		proto.GetExtension(field.Desc.Options(), binding.E_AutoTags).([]string),
		location, autoTags,
	)

	fieldTags := &structtag.Tags{}

	// Add auto tags
	for _, tag := range autoTags {
		if len(tag) > 0 {
			_ = fieldTags.Set(&structtag.Tag{
				Key:     tag,
				Name:    string(field.Desc.Name()),
				Options: nil,
			})
		}
	}

	// Add sphere binding tags
	if tag, ok := noJsonBinding[location]; ok {
		if len(tag) > 0 {
			_ = fieldTags.Set(&structtag.Tag{
				Key:     tag,
				Name:    string(field.Desc.Name()),
				Options: nil,
			})
			if aliases, exist := config.BindingAliases[tag]; exist {
				for _, alias := range aliases {
					if len(alias) > 0 {
						_ = fieldTags.Set(&structtag.Tag{
							Key:     alias,
							Name:    string(field.Desc.Name()),
							Options: nil,
						})
					}
				}
			}
		}
		if config.AutoRemoveJson {
			_ = fieldTags.Set(&structtag.Tag{
				Key:     "json",
				Name:    "-",
				Options: nil,
			})
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
				_ = fieldTags.Set(t)
			}
		}
	}
	return fieldTags, nil
}
