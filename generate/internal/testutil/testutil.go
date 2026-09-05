// Package testutil provides helpers for loading precompiled descriptor sets and
// constructing real *protogen.Plugin values in tests. It is shared by the
// integration and golden tests under generate/.
package testutil

import (
	"os"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// LoadDescriptorSet reads and unmarshals a FileDescriptorSet produced by
// `buf build --as-file-descriptor-set`. The whole set (including dependencies,
// which buf orders first) must be kept so imports can be resolved later.
func LoadDescriptorSet(t *testing.T, path string) *descriptorpb.FileDescriptorSet {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read descriptor set %q: %v", path, err)
	}

	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(data, &set); err != nil {
		t.Fatalf("failed to unmarshal descriptor set %q: %v", path, err)
	}
	if len(set.File) == 0 {
		t.Fatalf("descriptor set %q contains no files", path)
	}
	return &set
}

// MustCreatePlugin builds a real *protogen.Plugin from a descriptor set. The set
// must include every dependency; fileToGenerate is the proto path (relative to
// the buf module root) that should be generated.
//
// CompilerVersion is pinned so generated headers stay deterministic and do not
// depend on the host toolchain version (an unset version renders as "(unknown)").
func MustCreatePlugin(t *testing.T, set *descriptorpb.FileDescriptorSet, fileToGenerate string) *protogen.Plugin {
	t.Helper()

	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{fileToGenerate},
		ProtoFile:      set.File, // all files, dependencies first
		CompilerVersion: &pluginpb.Version{
			Major: proto.Int32(5),
			Minor: proto.Int32(29),
			Patch: proto.Int32(0),
		},
	}

	plugin, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatalf("failed to create plugin for %q: %v", fileToGenerate, err)
	}
	return plugin
}

// FileToGenerate returns the single file marked for generation. Always use this
// instead of plugin.Files[0]: with bundled imports, plugin.Files[0] is a
// dependency (e.g. descriptor.proto), not the target.
func FileToGenerate(t *testing.T, plugin *protogen.Plugin) *protogen.File {
	t.Helper()
	for _, f := range plugin.Files {
		if f.Generate {
			return f
		}
	}
	t.Fatal("no file marked for generation")
	return nil
}
