package protoc

import (
	"bytes"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

type ProtoCompiler struct {
	logger *slog.Logger
}

func NewProtoCompiler(logger *slog.Logger) (*ProtoCompiler, error) {
	version, err := exec.Command("protoc", "--version").Output()
	if err != nil {
		return nil, fmt.Errorf("could not run protoc: %w", err)
	}
	logger.Debug("found protoc", "version", strings.TrimSpace(string(version)))
	return &ProtoCompiler{logger: logger}, nil
}

// includeArgs puts the proto directory first so --include adds paths instead of replacing it.
func includeArgs(protoFilesDirPath string, includeDirs []string) []string {
	args := []string{"-I", protoFilesDirPath}
	for _, dir := range includeDirs {
		args = append(args, "-I", dir)
	}
	return args
}

func (c *ProtoCompiler) BuildDescriptorSet(protoFilesDirPath string, includeDirs []string) ([]*descriptorpb.FileDescriptorProto, error) {
	var descriptorSet []*descriptorpb.FileDescriptorProto

	protoFilePaths, err := getProtoFilesInDir(protoFilesDirPath)
	if err != nil {
		return descriptorSet, fmt.Errorf("could not get proto files from %s: %w", protoFilesDirPath, err)
	}

	tmpDir, err := os.MkdirTemp("", "gdbuf-desc-")
	if err != nil {
		return descriptorSet, fmt.Errorf("could not make temp directory for descriptor set: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	protoDescriptorPath := filepath.Join(tmpDir, "gdbuf.desc.binpb")
	args := []string{fmt.Sprintf("--descriptor_set_out=%s", protoDescriptorPath), "--include_source_info"}
	args = append(args, includeArgs(protoFilesDirPath, includeDirs)...)
	args = append(args, protoFilePaths...)
	buildProtoDescriptorCmd := exec.Command("protoc", args...)

	var stderr bytes.Buffer
	buildProtoDescriptorCmd.Stderr = &stderr

	err = buildProtoDescriptorCmd.Run()
	if err != nil {
		return descriptorSet, fmt.Errorf("could not build proto description file with cmd [%v]: %s", buildProtoDescriptorCmd.Args, stderr.String())
	}
	c.logger.Info("generated protobuf descriptor file", "path", protoDescriptorPath)

	protoDescData, err := os.ReadFile(protoDescriptorPath)
	if err != nil {
		return descriptorSet, fmt.Errorf("could not read proto description file: %w", err)
	}

	var protoFileDescriptorSet descriptorpb.FileDescriptorSet
	if err = proto.Unmarshal(protoDescData, &protoFileDescriptorSet); err != nil {
		return descriptorSet, fmt.Errorf("could not unmarshal proto description data: %w", err)
	}

	return protoFileDescriptorSet.GetFile(), nil
}

func (c *ProtoCompiler) CompileNanopb(protoFilesDirPath string, includeDirs []string, generatorDir string) (string, error) {
	tempProtocBuildDir, err := os.MkdirTemp("", "gdbuf-build-")
	if err != nil {
		return "", fmt.Errorf("could not make temp directory for proto cpp build: %w", err)
	}

	protoFilePaths, err := getProtoFilesInDir(protoFilesDirPath)
	if err != nil {
		return "", fmt.Errorf("could not get proto files from %s: %w", protoFilesDirPath, err)
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
	args := []string{
		fmt.Sprintf("--plugin=protoc-gen-nanopb=%s", pluginPath),
		"--nanopb_opt=-s type:FT_POINTER",
		fmt.Sprintf("--nanopb_opt=--custom-style=%s", customStyleArg),
		fmt.Sprintf("--nanopb_out=%s", tempProtocBuildDir),
	}

	args = append(args, includeArgs(protoFilesDirPath, includeDirs)...)
	args = append(args, protoFilePaths...)

	// Add Well-Known Types (WKTs) to ensure they are compiled with Nanopb
	wkts := []string{
		"google/protobuf/any.proto",
		"google/protobuf/duration.proto",
		"google/protobuf/struct.proto",
		"google/protobuf/timestamp.proto",
		"google/protobuf/wrappers.proto",
		"google/protobuf/field_mask.proto",
		"google/protobuf/empty.proto",
	}
	args = append(args, wkts...)

	compileCppCmd := exec.Command("protoc", args...)

	var stderr bytes.Buffer
	compileCppCmd.Stderr = &stderr

	err = compileCppCmd.Run()
	if err != nil {
		return "", fmt.Errorf("could not compile cpp proto files with cmd [%v]: %s", compileCppCmd.Args, stderr.String())
	}

	return tempProtocBuildDir, nil
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
			protoFilePaths = append(protoFilePaths, filepath.Join(protoFilesDirPath, path))
		}
		return nil
	})
	if err != nil {
		return protoFilePaths, fmt.Errorf("could not walk proto file directory: %w", err)
	}
	return protoFilePaths, nil
}
