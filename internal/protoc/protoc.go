package protoc

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/linker"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

type ProtoCompiler struct {
	logger *slog.Logger
}

func NewProtoCompiler(logger *slog.Logger) (*ProtoCompiler, error) {
	return &ProtoCompiler{logger: logger}, nil
}

// The proto directory comes first so --include adds paths instead of replacing it.
func compile(protoFilesDirPath string, includeDirs []string, names []string) (linker.Files, error) {
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: append([]string{protoFilesDirPath}, includeDirs...),
		}),
		SourceInfoMode: protocompile.SourceInfoStandard,
	}
	return compiler.Compile(context.Background(), names...)
}

func (c *ProtoCompiler) BuildDescriptorSet(protoFilesDirPath string, includeDirs []string) ([]*descriptorpb.FileDescriptorProto, error) {
	names, err := getProtoFilesInDir(protoFilesDirPath)
	if err != nil {
		return nil, fmt.Errorf("could not get proto files from %s: %w", protoFilesDirPath, err)
	}

	files, err := compile(protoFilesDirPath, includeDirs, names)
	if err != nil {
		return nil, fmt.Errorf("could not parse proto files: %w", err)
	}

	// imports first, like protoc; classes register in this order
	requested := map[string]bool{}
	for _, name := range names {
		requested[name] = true
	}
	var descriptorSet []*descriptorpb.FileDescriptorProto
	for _, fd := range withDependencies(files) {
		if requested[fd.GetName()] {
			descriptorSet = append(descriptorSet, fd)
		}
	}
	c.logger.Info("built protobuf descriptor set", "files", len(descriptorSet))
	return descriptorSet, nil
}

func withDependencies(files linker.Files) []*descriptorpb.FileDescriptorProto {
	var out []*descriptorpb.FileDescriptorProto
	seen := map[string]bool{}
	var visit func(f protoreflect.FileDescriptor)
	visit = func(f protoreflect.FileDescriptor) {
		if seen[f.Path()] {
			return
		}
		seen[f.Path()] = true
		imports := f.Imports()
		for i := 0; i < imports.Len(); i++ {
			visit(imports.Get(i).FileDescriptor)
		}
		out = append(out, protodesc.ToFileDescriptorProto(f))
	}
	for _, f := range files {
		visit(f)
	}
	return out
}

func getProtoFilesInDir(protoFilesDirPath string) ([]string, error) {
	protoFilePaths := []string{}
	err := fs.WalkDir(os.DirFS(protoFilesDirPath), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("dir walk err: %w", err)
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".proto") {
			protoFilePaths = append(protoFilePaths, path)
		}
		return nil
	})
	if err != nil {
		return protoFilePaths, fmt.Errorf("could not walk proto file directory: %w", err)
	}
	return protoFilePaths, nil
}
