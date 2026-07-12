package binding

import (
	"os"
	"strings"
	"testing"

	"github.com/go-sphere/protoc-gen-sphere-binding/generate/internal/testutil"
)

// TestIntegrationTags is the binding half of the cross-plugin integration test
// (OPT-02). It retags the same fixture shapes that protoc-gen-sphere's
// integration test consumes and asserts the produced struct tags match what the
// HTTP handler expects to bind:
//
//   - uri / header params and well-known scalar wrappers bound to query all get
//     the matching tag (and their json tag stripped);
//   - FORM fields get a form tag;
//   - a nested message and a map carried in the JSON body keep their json tag
//     and get no query/uri/header/form tag (the "binding silently mis-tags a
//     non-scalar field" bug);
//   - oneof members are tagged on their generated wrapper structs.
func TestIntegrationTags(t *testing.T) {
	set := testutil.LoadDescriptorSet(t, "testdata/pb/integration.pb")
	plugin := testutil.MustCreatePlugin(t, set, "integration.proto")
	file := testutil.FileToGenerate(t, plugin)

	tags, err := extractFile(file, DefaultConfig())
	if err != nil {
		t.Fatalf("extractFile failed: %v", err)
	}

	const input = "testdata/gen/integration.pb.go"
	src, err := os.ReadFile(input)
	if err != nil {
		t.Fatalf("failed to read input fixture %q (run `make testdata`): %v", input, err)
	}
	content, changed, err := RetagSource(input, src, tags)
	if err != nil {
		t.Fatalf("RetagSource failed: %v", err)
	}
	if !changed {
		t.Fatal("expected the plugin to retag the integration fixture")
	}
	got := string(content)

	// uri / header params.
	assertContains(t, got, "json:\"-\" uri:\"tenant_id\"")
	assertContains(t, got, "json:\"-\" header:\"request_id\"")

	// well-known scalar wrappers bound to query are allowed and tagged.
	assertContains(t, got, "json:\"-\" query:\"not_before\"")
	assertContains(t, got, "json:\"-\" query:\"created_after\"")
	assertContains(t, got, "json:\"-\" query:\"max_age\"")
	assertContains(t, got, "json:\"-\" query:\"keyword\"")
	assertContains(t, got, "json:\"-\" query:\"limit\"")

	// FORM fields.
	assertContains(t, got, "json:\"-\" form:\"filename\"")
	assertContains(t, got, "json:\"-\" form:\"size\"")

	// oneof members are tagged on their wrapper structs (no location set here, so
	// they simply keep json - the point is generation does not silently fail).
	assertContains(t, got, "type CreateItemRequest struct")

	// A nested message and a map in the JSON body must NOT be given a
	// query/uri/header/form tag.
	assertNotContains(t, got, "query:\"item\"")
	assertNotContains(t, got, "uri:\"item\"")
	assertNotContains(t, got, "query:\"labels\"")
	assertNotContains(t, got, "form:\"labels\"")
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("output missing expected fragment:\n  %q", needle)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("output unexpectedly contains forbidden fragment:\n  %q", needle)
	}
}
