package main

import (
	"flag"
	"fmt"

	"github.com/go-sphere/protoc-gen-sphere-binding/generate/binding"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"
)

var (
	showVersion    = flag.Bool("version", false, "print the version and exit")
	autoRemoveJson = flag.Bool("auto_remove_json", true, "automatically remove json tag if sphere binding location set")
	bindingAliases = flag.String("binding_aliases", "", "example: query=form,uri=path,db=database. add additional tag aliases for any binding tag")
	out            = flag.String("out", "api", "output directory for generated files")
)

func main() {
	flag.Parse()
	if *showVersion {
		fmt.Printf("protoc-gen-sphere-binding %v\n", "0.0.1")
		return
	}
	protogen.Options{
		ParamFunc: flag.CommandLine.Set,
	}.Run(func(gen *protogen.Plugin) error {
		gen.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

		aliases, err := binding.ParseBindingAliases(*bindingAliases)
		if err != nil {
			return err
		}

		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			bErr := binding.GenerateFile(f, *out, &binding.Config{
				AutoRemoveJson: *autoRemoveJson,
				BindingAliases: aliases,
			})
			if bErr != nil {
				return bErr
			}
		}
		return nil
	})
}
