package binding

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-sphere/protoc-gen-sphere-binding/generate/internal/testutil"
)

func TestResolveOutputPath(t *testing.T) {
	t.Run("nested prefix stays inside out", func(t *testing.T) {
		got, err := resolveOutputPath("api", "service/v1/foo")
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join("api", "service/v1/foo.pb.go"); got != want {
			t.Fatalf("resolveOutputPath = %q, want %q", got, want)
		}
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		if _, err := resolveOutputPath("api", "../../etc/passwd"); err == nil {
			t.Fatal("expected a path traversal error")
		}
	})
}

// TestGenerateFile_RoundTrip exercises the full impure path: extract tags from a
// descriptor, read the protoc-gen-go input from disk, rewrite it, and write it
// back atomically. The result must match the golden file produced by the pure
// RetagSource pipeline, proving GenerateFile and RetagSource agree.
func TestGenerateFile_RoundTrip(t *testing.T) {
	set := testutil.LoadDescriptorSet(t, "testdata/pb/basic.pb")
	plugin := testutil.MustCreatePlugin(t, set, "basic.proto")
	file := testutil.FileToGenerate(t, plugin)

	input, err := os.ReadFile("testdata/gen/basic.pb.go")
	if err != nil {
		t.Fatalf("read input fixture (run `make testdata`): %v", err)
	}

	// Lay the input out under a temp out-dir at the path the plugin computes.
	out := t.TempDir()
	dst := filepath.Join(out, file.GeneratedFilenamePrefix+".pb.go")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, input, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := GenerateFile(file, out, DefaultConfig()); err != nil {
		t.Fatalf("GenerateFile failed: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/golden/basic.pb.go")
	if err != nil {
		t.Fatalf("read golden (run `make update-golden`): %v", err)
	}
	if diff := firstDiff(string(want), string(got)); diff != "" {
		t.Errorf("GenerateFile output differs from golden:\n%s", diff)
	}
}

// TestGenerateFile_NoOptions verifies that a descriptor without any binding
// options leaves the on-disk file untouched (no rewrite, original bytes intact).
func TestGenerateFile_NoOptions(t *testing.T) {
	set := testutil.LoadDescriptorSet(t, "testdata/pb/no_binding.pb")
	plugin := testutil.MustCreatePlugin(t, set, "no_binding.proto")
	file := testutil.FileToGenerate(t, plugin)

	input, err := os.ReadFile("testdata/gen/no_binding.pb.go")
	if err != nil {
		t.Fatalf("read input fixture (run `make testdata`): %v", err)
	}

	out := t.TempDir()
	dst := filepath.Join(out, file.GeneratedFilenamePrefix+".pb.go")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, input, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := GenerateFile(file, out, DefaultConfig()); err != nil {
		t.Fatalf("GenerateFile failed: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(input) {
		t.Error("expected the file to be left untouched when there are no binding options")
	}
}
