package main

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-sphere/protoc-gen-sphere-binding/generate/binding"
)

func TestExtractConfig(t *testing.T) {
	want := binding.DefaultConfig()
	got, err := extractConfig()
	if err != nil {
		t.Fatalf("extractConfig() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("extractConfig() = %#v, want %#v", got, want)
	}

	original := *bindingAliases
	t.Cleanup(func() { *bindingAliases = original })
	*bindingAliases = "query=bad alias"
	if _, err := extractConfig(); err == nil {
		t.Fatal("extractConfig() with invalid binding_aliases error = nil")
	}
}

func TestVersionFlag(t *testing.T) {
	binPath := filepath.Join(t.TempDir(), "protoc-gen-sphere-binding")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binPath, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build plugin: %v\n%s", err, output)
	}

	command := exec.CommandContext(t.Context(), binPath, "-version")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run -version: %v\n%s", err, output)
	}
	if got, want := string(output), "protoc-gen-sphere-binding "+version+"\n"; got != want {
		t.Errorf("version output = %q, want %q", got, want)
	}
}
