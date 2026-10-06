package binding

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/go-sphere/binding/sphere/binding"
	"github.com/go-sphere/protoc-gen-sphere-binding/generate/internal/testutil"
)

// TestScalarBindabilityContract runs this plugin's real per-field tagging path
// over the shared scalar-bindability fixture and compares every
// (location, field) decision with the shared expected table.
//
// testdata/proto/scalar_bindability.proto and
// testdata/golden/scalar_bindability.golden are kept byte-identical with
// protoc-gen-sphere (generate/http/testdata/), whose parser runs the same table
// through its own copy of isScalarBindable/fieldKindDesc. The table is a
// hand-maintained contract, not a regenerated snapshot: change it in both
// repositories together, never from one implementation's output alone.
func TestScalarBindabilityContract(t *testing.T) {
	set := testutil.LoadDescriptorSet(t, "testdata/pb/scalar_bindability.pb")
	plugin := testutil.MustCreatePlugin(t, set, "scalar_bindability.proto")
	file := testutil.FileToGenerate(t, plugin)
	cfg := DefaultConfig()

	var got []string
	for _, message := range file.Messages {
		if !strings.HasSuffix(message.GoIdent.GoName, "Fields") {
			continue
		}
		location, autoTags := resolveLocationAndAutoTags(
			message.Desc.Options(), binding.E_DefaultLocation, binding.E_DefaultAutoTags,
			binding.BindingLocation_BINDING_LOCATION_UNSPECIFIED, nil,
		)
		locName, ok := noJSONBinding[location]
		if !ok {
			t.Fatalf("message %s has no query/uri/header/form default_location", message.Desc.Name())
		}
		for _, field := range message.Fields {
			// Mirror extractMessage: real oneof members inherit the oneof's
			// default location (none is set in the fixture).
			fieldLocation, fieldAutoTags := location, autoTags
			if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
				fieldLocation, fieldAutoTags = resolveLocationAndAutoTags(
					field.Oneof.Desc.Options(), binding.E_DefaultOneofLocation, binding.E_DefaultOneofAutoTags,
					location, autoTags,
				)
			}
			decision := "accept"
			tags, err := extractField(field, fieldLocation, fieldAutoTags, cfg)
			switch {
			case err != nil:
				decision = "reject"
			case tags == nil:
				t.Fatalf("%s.%s: extractField returned no tags and no error", message.Desc.Name(), field.Desc.Name())
			default:
				if _, tagErr := tags.Get(locName); tagErr != nil {
					t.Fatalf("%s.%s: accepted but no %q tag was set", message.Desc.Name(), field.Desc.Name(), locName)
				}
			}
			got = append(got, fmt.Sprintf("%s %s %s %s", locName, field.Desc.Name(), fieldKindDesc(field), decision))
		}
	}

	want := readScalarBindabilityTable(t, "testdata/golden/scalar_bindability.golden")
	if !slices.Equal(want, got) {
		t.Errorf("scalar bindability decisions diverge from the shared table:\n%s\nfull table from this implementation:\n%s",
			firstDiff(strings.Join(want, "\n"), strings.Join(got, "\n")), strings.Join(got, "\n"))
	}
}

// readScalarBindabilityTable returns the non-comment rows of the shared table
// with runs of whitespace collapsed, so columns may be aligned freely.
func readScalarBindabilityTable(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	var rows []string
	for line := range strings.Lines(string(data)) {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		rows = append(rows, strings.Join(fields, " "))
	}
	return rows
}
