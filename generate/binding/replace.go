package binding

import (
	"bytes"
	"cmp"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"slices"
	"strconv"
	"strings"

	"github.com/fatih/structtag"
)

// StructTags maps generated message and field names to replacement struct tags.
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
	if err := RetagASTWithCheck(fn, tags, &changed); err != nil {
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

// RetagASTWithCheck modifies tags and reports whether the AST changed.
func RetagASTWithCheck(file *ast.File, tags StructTags, changed *bool) error {
	if changed != nil {
		*changed = false
	}
	return retagAST(file, tags, changed)
}

// RetagAST applies tags to a parsed Go file.
func RetagAST(file *ast.File, tags StructTags) error {
	return retagAST(file, tags, nil)
}

// ReTagsWithCheck is retained for compatibility.
// Deprecated: use RetagASTWithCheck.
func ReTagsWithCheck(file *ast.File, tags StructTags, changed *bool) error {
	return RetagASTWithCheck(file, tags, changed)
}

// ReTags is retained for compatibility.
// Deprecated: use RetagAST.
func ReTags(file *ast.File, tags StructTags) error {
	return RetagAST(file, tags)
}

func retagAST(file *ast.File, tags StructTags, changed *bool) error {
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

					var currentTagValue string
					if field.Tag != nil && field.Tag.Value != "" {
						var err error
						currentTagValue, err = strconv.Unquote(field.Tag.Value)
						if err != nil {
							return err
						}
					}

					oldTags, err := structtag.Parse(currentTagValue)
					if err != nil {
						return err
					}
					if oldTags == nil {
						oldTags = &structtag.Tags{}
					}

					originalTagValue := oldTags.String()

					sorted := slices.Clone(newTags.Tags())
					slices.SortStableFunc(sorted, func(a, b *structtag.Tag) int {
						return cmp.Compare(a.Key, b.Key)
					})
					for _, t := range sorted {
						if err := oldTags.Set(t); err != nil {
							return err
						}
					}
					newTagValue := oldTags.String()

					if originalTagValue != newTagValue {
						if changed != nil {
							*changed = true
						}
						if field.Tag == nil {
							field.Tag = &ast.BasicLit{Kind: token.STRING}
						}
						if strings.ContainsRune(newTagValue, '`') {
							field.Tag.Value = strconv.Quote(newTagValue)
						} else {
							field.Tag.Value = "`" + newTagValue + "`"
						}
					}
				}
			}
		}
	}
	return nil
}
