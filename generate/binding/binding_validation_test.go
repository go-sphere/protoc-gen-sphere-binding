package binding

import (
	"strings"
	"testing"

	spherebinding "github.com/go-sphere/binding/sphere/binding"
	"github.com/go-sphere/protoc-gen-sphere-binding/generate/internal/testutil"
	"google.golang.org/protobuf/compiler/protogen"
)

// TestBindingLocationKindValidation verifies BUG-47 on the binding side: a
// map / message / bytes field bound to QUERY / URI / HEADER must be rejected at
// generation time instead of silently emitting a struct tag the runtime binder
// cannot satisfy. FORM is exempt (form data can carry bytes / files), and
// scalar fields pass through.
func TestBindingLocationKindValidation(t *testing.T) {
	set := testutil.LoadDescriptorSet(t, "testdata/pb/invalid_binding.pb")
	plugin := testutil.MustCreatePlugin(t, set, "invalid_binding.proto")
	file := testutil.FileToGenerate(t, plugin)

	byName := map[string]*protogen.Message{}
	for _, m := range file.Messages {
		byName[m.GoIdent.GoName] = m
	}
	cfg := DefaultConfig()

	bad := []struct {
		message string
		want    string // substring the error should mention
	}{
		{"BadQueryMessage", "query"},
		{"BadUriMap", "uri"},
		{"BadHeaderBytes", "header"},
	}
	for _, tt := range bad {
		t.Run(tt.message, func(t *testing.T) {
			msg := byName[tt.message]
			if msg == nil {
				t.Fatalf("message %s not found", tt.message)
			}
			_, err := extractMessage(msg, spherebinding.BindingLocation_BINDING_LOCATION_UNSPECIFIED, nil, cfg)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tt.message)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error should mention %q: %v", tt.want, err)
			}
		})
	}

	t.Run("GoodScalars", func(t *testing.T) {
		msg := byName["GoodScalars"]
		if msg == nil {
			t.Fatal("message GoodScalars not found")
		}
		if _, err := extractMessage(msg, spherebinding.BindingLocation_BINDING_LOCATION_UNSPECIFIED, nil, cfg); err != nil {
			t.Errorf("scalar bindings (and bytes in FORM) must be accepted, got: %v", err)
		}
	})
}
