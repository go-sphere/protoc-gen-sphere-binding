package main

import (
	"flag"
	"fmt"
	"strings"

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

		aliases := make(map[string][]string)
		if *bindingAliases != "" {
			for _, alias := range strings.Split(*bindingAliases, ",") {
				alias = strings.TrimSpace(alias)
				if len(alias) == 0 {
					continue
				}

				kv := strings.Split(alias, "=")
				if len(kv) != 2 {
					return fmt.Errorf("invalid binding alias format '%s': expected 'key=value'", alias)
				}

				key := strings.TrimSpace(kv[0])
				value := strings.TrimSpace(kv[1])

				if len(key) == 0 {
					return fmt.Errorf("invalid binding alias '%s': key cannot be empty", alias)
				}
				if strings.ContainsAny(key, " \t\n\r`:\"") {
					return fmt.Errorf("invalid binding alias key '%s': contains illegal characters", key)
				}

				if len(value) == 0 {
					return fmt.Errorf("invalid binding alias '%s': value cannot be empty", alias)
				}
				if strings.ContainsAny(value, " \t\n\r`:\"") {
					return fmt.Errorf("invalid binding alias value '%s': contains illegal characters", value)
				}

				aliases[key] = append(aliases[key], value)
			}
		}
		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			err := binding.GenerateFile(f, *out, &binding.Config{
				AutoRemoveJson: *autoRemoveJson,
				BindingAliases: aliases,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}
