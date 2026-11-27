package protoc

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/linker"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// Well-Known Types compiled with Nanopb so messages that use them link
var wkts = []string{
	"google/protobuf/any.proto",
	"google/protobuf/duration.proto",
	"google/protobuf/struct.proto",
	"google/protobuf/timestamp.proto",
	"google/protobuf/wrappers.proto",
	"google/protobuf/field_mask.proto",
	"google/protobuf/empty.proto",
}

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

func (c *ProtoCompiler) CompileNanopb(protoFilesDirPath string, includeDirs []string, generatorDir string) (string, error) {
	tempProtocBuildDir, err := os.MkdirTemp("", "gdbuf-build-")
	if err != nil {
		return "", fmt.Errorf("could not make temp directory for proto cpp build: %w", err)
	}

	names, err := getProtoFilesInDir(protoFilesDirPath)
	if err != nil {
		return "", fmt.Errorf("could not get proto files from %s: %w", protoFilesDirPath, err)
	}
	names = append(names, wkts...)

	files, err := compile(protoFilesDirPath, includeDirs, names)
	if err != nil {
		return "", fmt.Errorf("could not parse proto files: %w", err)
	}

	pluginName := "protoc-gen-nanopb"
	if runtime.GOOS == "windows" {
		pluginName += ".bat"
	}
	pluginPath := filepath.Join(generatorDir, pluginName)
	if err := os.Chmod(pluginPath, 0755); err != nil {
		c.logger.Warn("could not chmod plugin", "path", pluginPath, "err", err)
	}

	// Use FT_POINTER for strings/arrays to use malloc/free instead of static buffers/callbacks
	customStylePath := filepath.Join(generatorDir, "gdbuf_naming_style.py")
	customStyleArg := strings.TrimSuffix(customStylePath, ".py") + ".GdbufNamingStyle"
	request := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: names,
		Parameter:      proto.String("-s type:FT_POINTER,--custom-style=" + customStyleArg),
		ProtoFile:      withDependencies(files),
	}
	requestData, err := proto.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("could not marshal nanopb plugin request: %w", err)
	}

	pluginCmd := exec.Command(pluginPath)
	pluginCmd.Stdin = bytes.NewReader(requestData)
	var stdout, stderr bytes.Buffer
	pluginCmd.Stdout = &stdout
	pluginCmd.Stderr = &stderr
	if err := pluginCmd.Run(); err != nil {
		return "", fmt.Errorf("could not run nanopb plugin %s: %w: %s", pluginPath, err, stderr.String())
	}

	var response pluginpb.CodeGeneratorResponse
	if err := proto.Unmarshal(stdout.Bytes(), &response); err != nil {
		return "", fmt.Errorf("could not unmarshal nanopb plugin response: %w: %s", err, stderr.String())
	}
	if response.Error != nil {
		return "", fmt.Errorf("nanopb plugin failed: %s", response.GetError())
	}

	for _, file := range response.GetFile() {
		outPath := filepath.Join(tempProtocBuildDir, filepath.FromSlash(file.GetName()))
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return "", fmt.Errorf("could not make directory for %s: %w", outPath, err)
		}
		if err := os.WriteFile(outPath, []byte(file.GetContent()), 0644); err != nil {
			return "", fmt.Errorf("could not write %s: %w", outPath, err)
		}
	}

	return tempProtocBuildDir, nil
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
