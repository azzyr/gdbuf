package gdextension

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

//go:embed all:buildenv/*
var buildEnvFS embed.FS

type GDExtensionBuilder struct {
	logger   *slog.Logger
	cacheDir string
	name     string
}

func NewGDExtensionBuilder(logger *slog.Logger, cacheDir, name string) *GDExtensionBuilder {
	return &GDExtensionBuilder{
		logger:   logger,
		cacheDir: cacheDir,
		name:     name,
	}
}

func (gde *GDExtensionBuilder) Build(generatedCppSourceDir, outputDir, platform string, isDebug, isThreaded, generateOnly bool, stdout, stderr io.Writer) error {
	// Determine build directory: custom cache or UserCacheDir/gdbuf
	var buildDir string
	var userCacheDir string

	if gde.cacheDir != "" {
		// Use custom cache directory
		userCacheDir = gde.cacheDir
		buildDir = filepath.Join(userCacheDir, "gdbuf")
	} else {
		// Use system cache directory
		var err error
		userCacheDir, err = os.UserCacheDir()
		if err != nil {
			// Fallback
			userCacheDir = "."
			buildDir = filepath.Join(".", ".gdbuf_cache")
		} else {
			buildDir = filepath.Join(userCacheDir, "gdbuf")
		}
	}

	if generateOnly {
		gde.logger.Info("skipping build step as --generate-only was provided")
		return nil
	}

	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return fmt.Errorf("could not make build directory: %w", err)
	}

	gde.logger.Info("preparing gdextension build environment", "build_dir", buildDir)

	buildEnv, err := fs.Sub(buildEnvFS, "buildenv")
	if err != nil {
		return fmt.Errorf("could not get build environment fs: %w", err)
	}

	if err = copyFS(buildEnv, buildDir); err != nil {
		return fmt.Errorf("could not copy build environment to build directory: %w", err)
	}

	// Parse platform and optional arch (e.g. "macos-arm64")
	platformName, arch, _ := strings.Cut(platform, "-")
	if arch == "" {
		arch = map[string]string{"linux": "x86_64", "windows": "x86_64", "web": "wasm32", "android": "arm64"}[platformName]
	}
	threads := platformName != "web" || isThreaded

	tc, err := newToolchain(platformName, arch, threads, isDebug)
	if err != nil {
		return err
	}

	target := "template_release"
	if isDebug {
		target = "template_debug"
	}
	suffix := "." + platformName + "." + target + "." + arch
	if !threads {
		suffix += ".nothreads"
	}
	outPath := filepath.Join(outputDir, "dist", "lib"+gde.name+suffix+tc.ext)

	if err := compileAndLink(buildDir, filepath.Join(generatedCppSourceDir, "src"), outPath, tc, stdout, stderr); err != nil {
		return fmt.Errorf("build error: %w", err)
	}
	gde.logger.Info("build successful", "library", outPath)
	return nil
}

func copyFS(src fs.FS, dst string) error {
	return fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, path)
		if d.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}

		srcFile, err := src.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		// Read source contents to compare
		srcData, err := io.ReadAll(srcFile)
		if err != nil {
			return err
		}

		// Smart copy: only overwrite if contents differ to preserve timestamps
		if dstInfo, err := os.Stat(dstPath); err == nil && !dstInfo.IsDir() {
			if dstInfo.Size() == int64(len(srcData)) {
				dstData, err := os.ReadFile(dstPath)
				if err == nil && slices.Equal(srcData, dstData) {
					return nil // Skip copy, contents are identical
				}
			}
		}

		dstFile, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = dstFile.Write(srcData)
		return err
	})
}
