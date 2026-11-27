package gdextension

import (
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

//go:embed all:buildenv/*
var buildEnvFS embed.FS

type GDExtensionBuilder struct {
	logger   *slog.Logger
	cacheDir string
}

func NewGDExtensionBuilder(logger *slog.Logger, cacheDir string) *GDExtensionBuilder {
	return &GDExtensionBuilder{
		logger:   logger,
		cacheDir: cacheDir,
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

	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return fmt.Errorf("could not make build directory: %w", err)
	}

	gde.logger.Info("preparing gdextension build environment", "build_dir", buildDir)

	buildEnv, err := fs.Sub(buildEnvFS, "buildenv")
	if err != nil {
		return fmt.Errorf("could not get build environment fs: %w", err)
	}

	if err = copyFS(buildEnv, buildDir, nil); err != nil {
		return fmt.Errorf("could not copy build environment to build directory: %w", err)
	}

	// Clean up the src directory in the build directory to avoid stale files
	srcDir := filepath.Join(buildDir, "src")
	if err := os.RemoveAll(srcDir); err != nil {
		return fmt.Errorf("could not remove src directory in build directory: %w", err)
	}

	copyExcludes, err := computeCopyExcludes(generatedCppSourceDir, buildDir)
	if err != nil {
		return fmt.Errorf("could not compute copy exclusions: %w", err)
	}
	if err = copyFS(os.DirFS(generatedCppSourceDir), buildDir, nil, copyExcludes...); err != nil {
		return fmt.Errorf("could not copy custom build files to build directory: %w", err)
	}

	if generateOnly {
		gde.logger.Info("skipping build step as --generate-only was provided")
		return nil
	}

	// Parse platform and optional arch (e.g. "macos-arm64")
	sconsPlatform := platform
	sconsArch := ""
	if idx := strings.Index(platform, "-"); idx != -1 {
		sconsPlatform = platform[:idx]
		sconsArch = platform[idx+1:]
	}

	// all files are in place, try to build
	buildCmd := exec.Command("scons")

	buildCmd.Env = os.Environ()

	// Add arguments
	buildCmd.Args = append(buildCmd.Args, fmt.Sprintf("-j %d", runtime.NumCPU()))
	buildCmd.Args = append(buildCmd.Args, fmt.Sprintf("platform=%s", sconsPlatform))
	if sconsArch != "" {
		buildCmd.Args = append(buildCmd.Args, fmt.Sprintf("arch=%s", sconsArch))
	}

	if isDebug {
		buildCmd.Args = append(buildCmd.Args, "target=template_debug")
	} else {
		buildCmd.Args = append(buildCmd.Args, "target=template_release")
	}

	if sconsPlatform == "web" {
		if !isThreaded {
			buildCmd.Args = append(buildCmd.Args, "threads=no")
		} else {
			buildCmd.Args = append(buildCmd.Args, "threads=yes")
		}
	}

	buildCmd.Dir = buildDir
	buildCmd.Stdout = stdout
	buildCmd.Stderr = stderr

	// bin persists in the cache; clear it so only this platform's library gets copied
	binDir := filepath.Join(buildDir, "bin")
	if err := os.RemoveAll(binDir); err != nil {
		return fmt.Errorf("could not clear bin directory: %w", err)
	}

	err = buildCmd.Run()
	if err != nil {
		return fmt.Errorf("build error: %w", err)
	}
	gde.logger.Info("build successful")

	if err = copyFS(os.DirFS(binDir), filepath.Join(outputDir, "dist"), stdout); err != nil {
		return fmt.Errorf("could not copy build output to output directory: %w", err)
	}
	return nil
}

func copyFS(src fs.FS, dst string, logWriter io.Writer, excludePrefixes ...string) error {
	var normalizedPrefixes []string
	for _, prefix := range excludePrefixes {
		normalized := normalizeFSPath(prefix)
		if normalized == "." || normalized == "" {
			continue
		}
		normalizedPrefixes = append(normalizedPrefixes, normalized)
	}

	fileCount := 0
	return fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		normalizedPath := normalizeFSPath(path)
		for _, prefix := range normalizedPrefixes {
			if normalizedPath == prefix || strings.HasPrefix(normalizedPath, prefix+"/") {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
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

		// Throttle log output for file copies to prevent flooding the TUI
		if logWriter != nil {
			fileCount++
			if fileCount%10 == 0 {
				fmt.Fprintln(logWriter, "Copying "+path)
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

func computeCopyExcludes(srcRoot, dst string) ([]string, error) {
	srcAbs, err := filepath.Abs(srcRoot)
	if err != nil {
		return nil, err
	}

	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return nil, err
	}

	rel, err := filepath.Rel(srcAbs, dstAbs)
	if err != nil {
		return nil, err
	}

	if rel == "." || rel == "" {
		return nil, errors.New("source and destination directories must be different")
	}

	if strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return nil, nil
	}

	return []string{filepath.ToSlash(rel)}, nil
}

func normalizeFSPath(path string) string {
	path = filepath.ToSlash(path)
	path = strings.TrimPrefix(path, "./")
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return "."
	}
	return path
}
