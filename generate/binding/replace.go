package binding

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"sort"
	"strings"

	"github.com/fatih/structtag"
)

type StructTags map[string]map[string]*structtag.Tags

// RetagSource parses the Go source in src, applies tags to the matching struct
// fields, and returns the gofmt-formatted result together with whether anything
// actually changed. When no field was retagged it returns the original src
// unchanged so callers can skip rewriting the file.
//
// RetagSource performs no file I/O; filename is only used for error positions.
// This makes it the natural seam for unit and golden tests.
func RetagSource(filename string, src []byte, tags StructTags) ([]byte, bool, error) {
	fs := token.NewFileSet()
	fn, err := parser.ParseFile(fs, filename, src, parser.ParseComments)
	if err != nil {
		return nil, false, err
	}

	changed := false
	if err := ReTagsWithCheck(fn, tags, &changed); err != nil {
		return nil, false, err
	}
	if !changed {
		return src, false, nil
	}

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fs, fn); err != nil {
		return nil, false, err
	}

	source, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, false, err
	}
	return source, true, nil
}

// ReTagsWithCheck modifies tags and detects actual changes
func ReTagsWithCheck(file *ast.File, tags StructTags, changed *bool) error {
	if changed != nil {
		*changed = false
	}
	return reTagsInternal(file, tags, changed)
}

func ReTags(file *ast.File, tags StructTags) error {
	return reTagsInternal(file, tags, nil)
}

func reTagsInternal(file *ast.File, tags StructTags, changed *bool) error {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, tsOK := spec.(*ast.TypeSpec)
			if !tsOK {
				continue
			}

			structDecl, isStruct := typeSpec.Type.(*ast.StructType)
			if !isStruct {
				continue
			}

			structName := typeSpec.Name.String()
			fieldsToRetag, structFound := tags[structName]
			if !structFound {
				continue
			}

			for _, field := range structDecl.Fields.List {
				for _, fieldName := range field.Names {
					newTags, fieldFound := fieldsToRetag[fieldName.String()]
					if !fieldFound || newTags == nil {
						continue
					}

					if field.Tag == nil {
						field.Tag = &ast.BasicLit{Kind: token.STRING}
					}

					currentTagValue := strings.Trim(field.Tag.Value, "`")
					oldTags, parseErr := structtag.Parse(currentTagValue)
					if parseErr != nil {
						return parseErr
					}

					originalTagValue := oldTags.String()

					sort.Stable(newTags)
					for _, t := range newTags.Tags() {
						if setErr := oldTags.Set(t); setErr != nil {
							return setErr
						}
					}
					newTagValue := oldTags.String()

					if changed != nil && originalTagValue != newTagValue {
						*changed = true
					}

					field.Tag.Value = "`" + newTagValue + "`"
				}
			}
		}
	}
	return nil
}
