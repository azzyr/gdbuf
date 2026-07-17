package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/azzyr/gdbuf/internal/codegen"
	"github.com/azzyr/gdbuf/internal/gdextension"
	"github.com/azzyr/gdbuf/internal/protoc"
)

var (
	// Version is the semantic version of the binary, injected at build time.
	Version = "dev"
	// Commit is the git commit hash of the binary, injected at build time.
	Commit = "none"
)

type arrayFlags []string

func (i *arrayFlags) String() string {
	return strings.Join(*i, ",")
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func main() {
	var includeDirs arrayFlags
	flag.Var(&includeDirs, "include", "include directories for proto files")
	protoInputDirPtr := flag.String("proto", "", "path to proto definition files")
	cppOutputDirPtr := flag.String("genout", ".", "generated proto c++ code output path")
	extensionNamePtr := flag.String("name", "gdbufgen", "name of the generated gdextension")
	extensionArtifactOutputDirPtr := flag.String("out", "./out", "output directory location of the generated gdextension")
	generateOnlyPtr := flag.Bool("generate-only", false, "only generate c++ code, do not compile gdextension")
	platformPtr := flag.String("platform", "", "target platform (linux, windows, macos-x86_64, macos-arm64, web, android)")
	cacheDirPtr := flag.String("cache", "", "cache directory for build artifacts (default: system cache)")
	versionPtr := flag.Bool("version", false, "print version information and exit")

	debugPtr := flag.Bool("debug", false, "enable debug build symbols and tooling configurations")
	threadedPtr := flag.Bool("threaded", false, "enable multi-threaded support profiles")
	doublePtr := flag.Bool("double", false, "enable double-precision floating point rendering variables")

	flag.Parse()

	if *versionPtr {
		fmt.Printf("gdbuf version: %s\n", Version)
		fmt.Printf("git commit: %s\n", Commit)
		os.Exit(0)
	}

	var logHandler slog.Handler
	logHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{ Level: slog.LevelDebug, } )

	logger := slog.New(logHandler)

	logger.Info("starting gdbuf", "version", Version)

	if len(*protoInputDirPtr) == 0 {
		logger.Error("required argument --proto not given")
		os.Exit(1)
	}

	if err := checkPath(*protoInputDirPtr, true); err != nil {
		logger.Error("invalid path for proto files", "err", err)
		os.Exit(1)
	}

	for _, includeDir := range includeDirs {
		if err := checkPath(includeDir, true); err != nil {
			logger.Error("invalid path for include directory", "dir", includeDir, "err", err)
			os.Exit(1)
		}
	}

	if err := checkPath(*cppOutputDirPtr, true); err != nil {
		logger.Error("invalid path for code gen output directory", "err", err)
		os.Exit(1)
	}

	if err := checkPath(*extensionArtifactOutputDirPtr, true); err != nil {
		logger.Error("invalid path for gdextension output directory", "err", err)
		os.Exit(1)
	}

	// Prepare Nanopb Generator (extracted to temp)
	genTmpDir, err := os.MkdirTemp("", "nanopb-gen-")
	if err != nil {
		logger.Error("could not create temp dir for generator", "err", err)
		os.Exit(1)
	}
	defer os.RemoveAll(genTmpDir)

	gdExtensionBuilder := gdextension.NewGDExtensionBuilder(logger, *cacheDirPtr)
	if err := gdExtensionBuilder.ExtractNanopbGenerator(genTmpDir); err != nil {
		logger.Error("could not extract nanopb generator", "err", err)
		os.Exit(1)
	}

	protoc, err := protoc.NewProtoCompiler(logger.WithGroup("protoc"))
	if err != nil {
		logger.Error("could not create new proto compiler", "err", err)
		os.Exit(1)
	}

	descriptorSet, err := protoc.BuildDescriptorSet(*protoInputDirPtr, includeDirs)
	if err != nil {
		logger.Error("could not build descriptor set for protobuf definitions", "err", err)
		os.Exit(1)
	}

	compiledProtoCppTempDirPath, err := protoc.CompileNanopb(*protoInputDirPtr, includeDirs, genTmpDir)
	if err != nil {
		logger.Error("could not compile proto cpp (nanopb)", "err", err)
		os.Exit(1)
	}

	codeGenerator, err := codegen.NewCodeGenerator(logger, *cppOutputDirPtr, *extensionNamePtr, protoc.GetVersion())
	if err != nil {
		logger.Error("could not create new code generator", "err", err)
		os.Exit(1)
	}

	err = codeGenerator.GenerateCode(descriptorSet)
	if err != nil {
		logger.Error("problem generating code", "err", err)
		os.Exit(1)
	}

	compiledProtoCppOutDirPath := filepath.Join(*cppOutputDirPtr, "src")
	err = copyDir(compiledProtoCppTempDirPath, compiledProtoCppOutDirPath)
	if err != nil {
		logger.Error("problem copying compiled cpp proto to directory", "err", err)
		os.Exit(1)
	}

	platforms := []string{*platformPtr}
	if *platformPtr == "all" {
		platforms = []string{"linux", "windows", "macos-x86_64", "macos-arm64", "web", "android"}
	} else if strings.Contains(*platformPtr, ",") {
		platforms = strings.Split(*platformPtr, ",")
	}

	var validPlatforms []string
	for _, platform := range platforms {
		// Trim space just in case user did "linux, windows"
		platform = strings.TrimSpace(platform)
		if platform == "" {
			continue
		}
		validPlatforms = append(validPlatforms, platform)
	}

	if len(validPlatforms) > 0 {
		for _, platform := range validPlatforms {
			targetName := "build-" + platform

			if *debugPtr {
				targetName += "-debug"
			} else {
				targetName += "-release"
			}

			if *doublePtr {
				targetName += "-double"
			} else {
				targetName += "-single"
			}

			if platform == "web" && !*threadedPtr {
				targetName += "-nothreads"
			}

			logger.Info("building gdextension", "platform", platform, "target", targetName)
			err = gdExtensionBuilder.Build(*cppOutputDirPtr, *extensionArtifactOutputDirPtr, platform, targetName, *generateOnlyPtr, os.Stdout, os.Stderr)
			if err != nil {
				logger.Error("problem building gdextension", "platform", platform, "target", targetName, "err", err)
				os.Exit(1)
			}
		}
	} else {
		logger.Warn("no valid platform configuration specified, nothing to build")
	}
}

func copyDir(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

func checkPath(path string, isDir bool) error {
	fileInfo, err := os.Stat(path)
	if os.IsNotExist(err) {
		return errors.New("path does not exist")
	} else if err != nil {
		return fmt.Errorf("problem with path: %w", err)
	} else if fileInfo.IsDir() != isDir {
		return fmt.Errorf("isDir expected: %t but got: %t", isDir, fileInfo.IsDir())
	}
	return nil
}
