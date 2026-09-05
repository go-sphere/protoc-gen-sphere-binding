package main

import (
	"flag"
	"fmt"

	"github.com/go-sphere/protoc-gen-sphere-binding/generate/binding"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"
)

const (
	version          = "0.0.1"
	defaultOutputDir = "api"
)

var (
	showVersion    = flag.Bool("version", false, "print the version and exit")
	autoRemoveJSON = flag.Bool("auto_remove_json", binding.DefaultAutoRemoveJSON, "automatically remove json tag if sphere binding location set")
	bindingAliases = flag.String("binding_aliases", "", "example: query=form,uri=path,db=database. add additional tag aliases for any binding tag")
	out            = flag.String("out", defaultOutputDir, "output directory for generated files")
)

func main() {
	flag.Parse()
	if *showVersion {
		fmt.Printf("protoc-gen-sphere-binding %s\n", version)
		return
	}
	protogen.Options{
		ParamFunc: flag.CommandLine.Set,
	}.Run(run)
}

func run(plugin *protogen.Plugin) error {
	plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
	cfg, err := extractConfig()
	if err != nil {
		return err
	}
	generator, err := binding.NewGenerator(*out, cfg)
	if err != nil {
		return err
	}
	for _, file := range plugin.Files {
		if !file.Generate {
			continue
		}
		if err := generator.GenerateFile(file); err != nil {
			return err
		}
	}
	return nil
}

func extractConfig() (*binding.Config, error) {
	aliases, err := binding.ParseBindingAliases(*bindingAliases)
	if err != nil {
		return nil, err
	}
	cfg := binding.DefaultConfig()
	cfg.AutoRemoveJSON = *autoRemoveJSON
	cfg.BindingAliases = aliases
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
