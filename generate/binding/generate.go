package binding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

// GenerateFile re-tags the protoc-gen-go output for file, writing the result
// back into the out directory. It is a thin wrapper over generateFile so the
// public surface stays small.
func GenerateFile(file *protogen.File, out string, config *Config) error {
	return generateFile(file, out, config)
}

// generateFile orchestrates the impure steps: extract tags from the descriptor,
// resolve the target path, read the existing .pb.go, apply the tags, and write
// it back atomically. All of the logic that does not touch the filesystem lives
// in the pure helpers (extractFile, resolveOutputPath, RetagSource) so it can be
// unit tested in isolation.
func generateFile(file *protogen.File, out string, config *Config) error {
	tags, err := extractFile(file, config)
	if err != nil {
		return err
	}
	if len(tags) == 0 {
		return nil
	}

	filename, err := resolveOutputPath(out, file.GeneratedFilenamePrefix)
	if err != nil {
		return err
	}

	// Preserve original file permissions.
	originalInfo, err := os.Stat(filename)
	if err != nil {
		return err
	}
	originalPerm := originalInfo.Mode().Perm()

	src, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	source, changed, err := RetagSource(filename, src, tags)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	return writeFileAtomic(filename, source, originalPerm)
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
		return "", fmt.Errorf("invalid file path: potential path traversal")
	}
	return filename, nil
}

// writeFileAtomic writes data to filename atomically using a temp file + rename.
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

	return os.Rename(tempName, filename)
}
