package binding

import (
	"go/ast"
	"go/token"
	"sort"
	"strings"

	"github.com/fatih/structtag"
)

type StructTags map[string]map[string]*structtag.Tags

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

		var typeSpec *ast.TypeSpec
		for _, spec := range genDecl.Specs {
			if ts, tsOK := spec.(*ast.TypeSpec); tsOK {
				typeSpec = ts
				break
			}
		}
		if typeSpec == nil {
			continue
		}

		structDecl, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
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
	return nil
}
