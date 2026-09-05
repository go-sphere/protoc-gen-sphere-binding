package binding

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

// Generator owns validated configuration for a binding generation run.
type Generator struct {
	out string
	cfg *Config
}

// NewGenerator validates cfg and snapshots it for reuse across all files in a
// protoc invocation.
func NewGenerator(out string, cfg *Config) (*Generator, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfgCopy := new(*cfg)
	cfgCopy.BindingAliases = make(map[string][]string, len(cfg.BindingAliases))
	for key, aliases := range cfg.BindingAliases {
		cfgCopy.BindingAliases[key] = slices.Clone(aliases)
	}
	return &Generator{out: out, cfg: cfgCopy}, nil
}

// GenerateFile is a convenience wrapper for processing one file. Callers that
// process multiple files should construct a Generator and reuse it.
func GenerateFile(file *protogen.File, out string, cfg *Config) error {
	generator, err := NewGenerator(out, cfg)
	if err != nil {
		return err
	}
	return generator.GenerateFile(file)
}

// GenerateFile re-tags the protoc-gen-go output for file, writing the result
// back into the configured output directory.
func (g *Generator) GenerateFile(file *protogen.File) error {
	return generateFile(file, g.out, g.cfg)
}

// generateFile orchestrates the impure steps: extract tags from the descriptor,
// resolve the target path, read the existing .pb.go, apply the tags, and write
// it back atomically. All of the logic that does not touch the filesystem lives
// in the pure helpers (extractFile, resolveOutputPath, RetagSource) so it can be
// unit tested in isolation.
func generateFile(file *protogen.File, out string, cfg *Config) error {
	tags, err := extractFile(file, cfg)
	if err != nil {
		return fmt.Errorf("extract tags for %q: %w", file.Desc.Path(), err)
	}
	if len(tags) == 0 {
		return nil
	}

	filename, err := resolveOutputPath(out, file.GeneratedFilenamePrefix)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}

	// Preserve original file permissions.
	originalInfo, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("stat output file %q: %w", filename, err)
	}
	originalPerm := originalInfo.Mode().Perm()

	src, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read output file %q: %w", filename, err)
	}

	source, changed, err := RetagSource(filename, src, tags)
	if err != nil {
		return fmt.Errorf("retag output file %q: %w", filename, err)
	}
	if !changed {
		return nil
	}

	if err := writeFileAtomic(filename, source, originalPerm); err != nil {
		return fmt.Errorf("write output file %q: %w", filename, err)
	}
	return nil
}

// resolveOutputPath builds the target .pb.go path for prefix inside out and
// guards against path traversal escaping the output directory. It performs no
// I/O, which makes it cheap to unit test.
func resolveOutputPath(out, prefix string) (string, error) {
	out = filepath.Clean(out)
	filename := filepath.Join(out, prefix+".pb.go")

	rel, err := filepath.Rel(out, filepath.Clean(filename))
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid file path: potential path traversal")
	}
	return filename, nil
}

// writeFileAtomic writes data to filename atomically using a temp file + rename.
func writeFileAtomic(filename string, data []byte, perm os.FileMode) error {
	tempFile, err := os.CreateTemp(filepath.Dir(filename), ".tmp-*.pb.go")
	if err != nil {
		return err
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

	return os.Rename(tempName, filename)
}
