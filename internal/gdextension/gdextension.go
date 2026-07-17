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
	"strings"
)

//go:embed all:buildenv/*
var buildEnvFS embed.FS

const gdbufNamingStyleScript = `class GdbufNamingStyle:
    def sanitize(self, name):
        if name in [
            "alignas", "alignof", "and", "and_eq", "asm", "atomic_cancel", "atomic_commit", "atomic_noexcept", "auto",
            "bitand", "bitor", "bool", "break", "case", "catch", "char", "char8_t", "char16_t", "char32_t", "class",
            "compl", "concept", "const", "consteval", "constexpr", "constinit", "const_cast", "continue", "co_await",
            "co_return", "co_yield", "decltype", "default", "delete", "do", "double", "dynamic_cast", "else", "enum",
            "explicit", "export", "extern", "false", "float", "for", "friend", "goto", "if", "inline", "int", "long",
            "mutable", "namespace", "new", "noexcept", "not", "not_eq", "nullptr", "operator", "or", "or_eq", "private",
            "protected", "public", "register", "reinterpret_cast", "requires", "return", "short", "signed", "sizeof",
            "static", "static_assert", "static_cast", "struct", "switch", "synchronized", "template", "this",
            "thread_local", "throw", "true", "try", "typedef", "typeid", "typename", "union", "unsigned", "using",
            "virtual", "void", "volatile", "wchar_t", "while", "xor", "xor_eq"
        ]:
            return name + "_"
        return name

    def enum_name(self, name):
        return self.sanitize("_%s" % (name))

    def struct_name(self, name):
        return self.sanitize("_%s" % (name))

    def union_name(self, name):
        return self.sanitize("_%s" % (name))

    def type_name(self, name):
        return self.sanitize("%s" % (name))

    def define_name(self, name):
        return self.sanitize("%s" % (name))

    def var_name(self, name):
        return self.sanitize("%s" % (name))

    def enum_entry(self, name):
        return self.sanitize("%s" % (name))

    def func_name(self, name):
        return self.sanitize("%s" % (name))

    def bytes_type(self, struct_name, name):
        return "%s_%s_t" % (struct_name, name)
`

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

func (gde *GDExtensionBuilder) ExtractNanopbGenerator(dst string) error {
	genFS, err := fs.Sub(buildEnvFS, "buildenv/nanopb/generator")
	if err != nil {
		return err
	}
	if err := copyFS(genFS, dst, nil); err != nil {
		return err
	}

	// Write custom naming style script
	stylePath := filepath.Join(dst, "gdbuf_naming_style.py")
	if err := os.WriteFile(stylePath, []byte(gdbufNamingStyleScript), 0644); err != nil {
		return fmt.Errorf("could not write custom style script: %w", err)
	}
	return nil
}

func (gde *GDExtensionBuilder) Build(generatedCppSourceDir, outputDir, platform string, targetName string, generateOnly bool, stdout, stderr io.Writer) error {
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

	// Copy doc_classes from source to build dir if they exist (to be packaged later)
	docsSrc := filepath.Join(generatedCppSourceDir, "doc_classes")
	if _, err := os.Stat(docsSrc); err == nil {
		docsDest := filepath.Join(buildDir, "doc_classes")
		if err := os.MkdirAll(docsDest, 0755); err != nil {
			return fmt.Errorf("could not create docs directory in build dir: %w", err)
		}
		if err := copyFS(os.DirFS(docsSrc), docsDest, nil); err != nil {
			return fmt.Errorf("could not copy doc files to build dir: %w", err)
		}
	}

	if generateOnly {
		gde.logger.Info("skipping build step as --generate-only was provided")
		return nil
	}

	// all files are in place, try to build
	androidNDKHome := os.Getenv("ANDROID_NDK_HOME")
	emsdkHome := os.Getenv("EMSDK")
	osxcrossTarget := os.Getenv("OSXCROSS_TARGET")

	switch platform {
	case "web":
		if emsdkHome == "" {
			return errors.New("EMSDK environment variable is not set. Please install Emscripten SDK and set EMSDK to its root directory")
		}
		gde.logger.Info("using system EMSDK", "path", emsdkHome)
	case "android":
		if androidNDKHome == "" {
			return errors.New("ANDROID_NDK_HOME environment variable is not set. Please install Android NDK and set ANDROID_NDK_HOME to its root directory")
		}
		gde.logger.Info("using system ANDROID_NDK_HOME", "path", androidNDKHome)
	default:
		if strings.HasPrefix(platform, "macos") {
			if osxcrossTarget == "" {
				return errors.New("OSXCROSS_TARGET is not set. " +
					"Point it to the OSXCross target/ directory, or use the mbround18/setup-osxcross action")
			}
			gde.logger.Info("using OSXCROSS_TARGET", "path", osxcrossTarget)
		}
	}

	buildCmd := exec.Command("make", targetName)

	buildCmd.Env = os.Environ()
	buildCmd.Env = append(buildCmd.Env, fmt.Sprintf("WORKSPACE=%s", buildDir))
	cmakeBin := os.Getenv("CMAKE")
	if cmakeBin == "" {
		cmakeBin, err = exec.LookPath("cmake")
		if err != nil {
			return fmt.Errorf("could not locate cmake executable in PATH: %w", err)
		}
	}
	buildCmd.Env = append(buildCmd.Env, fmt.Sprintf("CMAKE=%s", cmakeBin))
	if androidNDKHome != "" {
		buildCmd.Env = append(buildCmd.Env, fmt.Sprintf("ANDROID_NDK_HOME=%s", androidNDKHome))
	}
	if emsdkHome != "" {
		buildCmd.Env = append(buildCmd.Env, fmt.Sprintf("EMSDK=%s", emsdkHome))
		// Add emscripten to PATH
		// The path structure is usually emsdk/upstream/emscripten
		emscriptenBin := filepath.Join(emsdkHome, "upstream", "emscripten")
		currentPath := os.Getenv("PATH")
		buildCmd.Env = append(buildCmd.Env, fmt.Sprintf("PATH=%s%c%s", emscriptenBin, os.PathListSeparator, currentPath))
	}
	if osxcrossTarget != "" {
		buildCmd.Env = append(buildCmd.Env, fmt.Sprintf("OSXCROSS_TARGET=%s", osxcrossTarget))
	}
	buildCmd.Dir = buildDir
	buildCmd.Stdout = stdout
	buildCmd.Stderr = stderr

	err = buildCmd.Run()
	if err != nil {
		return fmt.Errorf("build error: %w", err)
	}
	fmt.Fprintln(stdout, "Build command finished, copying artifacts...")
	gde.logger.Info("build successful")

	fmt.Fprintln(stdout, "Copying binaries to intermediate location...")
	if err = copyFS(os.DirFS(filepath.Join(buildDir, "build", platform, "bin")), filepath.Join(buildDir, "out", "dist"), stdout); err != nil {
		return fmt.Errorf("could not copy build output to output directory: %w", err)
	}

	if err = os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("could not create output directory: %w", err)
	}

	fmt.Fprintln(stdout, "Copying artifacts to final destination...")
	if err = copyFS(os.DirFS(filepath.Join(buildDir, "out")), outputDir, stdout); err != nil {
		return fmt.Errorf("could not copy build output to output directory: %w", err)
	}

	// Also copy doc_classes to the final output (addon)
	if _, err := os.Stat(docsSrc); err == nil {
		docsDest := filepath.Join(outputDir, "doc_classes")
		if err := os.MkdirAll(docsDest, 0755); err != nil {
			return fmt.Errorf("could not create docs directory in output: %w", err)
		}
		fmt.Fprintln(stdout, "Copying documentation...")
		if err := copyFS(os.DirFS(docsSrc), docsDest, stdout); err != nil {
			return fmt.Errorf("could not copy doc files to output: %w", err)
		}
	}

	fmt.Fprintln(stdout, "Build procedure complete.")
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

		if logWriter != nil {
			// Throttle log output for file copies to prevent flooding the TUI
			fileCount++
			if fileCount%10 == 0 {
				fmt.Fprintln(logWriter, "Copying "+path)
			}
		}

		srcFile, err := src.Open(path)
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
