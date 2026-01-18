package gdextension

import (
	"archive/zip"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

const (
	androidNDKVersion = "r28b"
	emscriptenVersion = "3.1.64"
)

var androidNDKURLs = map[string]string{
	"linux":   "https://dl.google.com/android/repository/android-ndk-" + androidNDKVersion + "-linux.zip",
	"windows": "https://dl.google.com/android/repository/android-ndk-" + androidNDKVersion + "-windows.zip",
	"darwin":  "https://dl.google.com/android/repository/android-ndk-" + androidNDKVersion + "-darwin.zip",
}

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

func (gde *GDExtensionBuilder) Build(generatedCppSourceDir, outputDir, platform string, generateOnly bool, stdout, stderr io.Writer) error {
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

	if err = copyFS(os.DirFS(generatedCppSourceDir), buildDir, nil); err != nil {
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
	var buildTarget string
	var buildSubdir string
	// Default to host OS
	switch runtime.GOOS {
	case "linux":
		buildTarget = "build-linux"
		buildSubdir = "linux"
	case "darwin":
		buildTarget = "build-macos"
		buildSubdir = "macos"
	case "windows":
		buildTarget = "build-windows"
		buildSubdir = "windows"
	default:
		return fmt.Errorf("unsupported os: %s", runtime.GOOS)
	}

	androidNDKHome := os.Getenv("ANDROID_NDK_HOME")
	emsdkHome := os.Getenv("EMSDK")

	if platform != "" {
		switch platform {
		case "linux":
			buildTarget = "build-linux"
			buildSubdir = "linux"
		case "windows":
			buildTarget = "build-windows"
			buildSubdir = "windows"
		case "web":
			buildTarget = "build-web"
			buildSubdir = "web"
			if emsdkHome == "" {
				gde.logger.Info("EMSDK not set, checking for managed Emscripten SDK")
				managedEmsdkPath, err := gde.ensureEmscripten(userCacheDir, stdout, stderr)
				if err != nil {
					return fmt.Errorf("failed to setup Emscripten SDK: %w", err)
				}
				emsdkHome = managedEmsdkPath
			} else {
				gde.logger.Info("using existing EMSDK", "path", emsdkHome)
			}
		case "android":
			buildTarget = "build-android"
			buildSubdir = "android"
			// Ensure NDK is available
			if androidNDKHome == "" {
				gde.logger.Info("ANDROID_NDK_HOME not set, checking for managed NDK")
				managedNDKPath, err := gde.ensureAndroidNDK(userCacheDir)
				if err != nil {
					return fmt.Errorf("failed to setup android NDK: %w", err)
				}
				androidNDKHome = managedNDKPath
			} else {
				gde.logger.Info("using existing ANDROID_NDK_HOME", "path", androidNDKHome)
			}
		default:
			return fmt.Errorf("unsupported platform: %s", platform)
		}
	}

	buildCmd := exec.Command("make", buildTarget)

	buildCmd.Env = os.Environ()
	buildCmd.Env = append(buildCmd.Env, fmt.Sprintf("WORKSPACE=%s", buildDir))
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
	buildCmd.Dir = buildDir
	buildCmd.Stdout = stdout
	buildCmd.Stderr = stderr

	// Use a pipe to capture stdout if stdout/stderr are not directly os.Stdout/os.Stderr
	// ensuring we can flush the buffer in progressWriter
	if stdout != os.Stdout {
		// When using TUI, we might need to ensure line buffering or force flushing
		// But exec.Command doesn't support PTY easily cross-platform.
		// However, make usually buffers output when not connected to a TTY.
		// We can try to force line buffering via stdbuf on Linux if available, or just accept chunks.
		// Since we handle chunks in progressWriter, this should be fine.
	}

	err = buildCmd.Run()
	if err != nil {
		return fmt.Errorf("build error: %w", err)
	}
	fmt.Fprintln(stdout, "Build command finished, copying artifacts...")
	gde.logger.Info("build successful")

	fmt.Fprintln(stdout, "Copying binaries to intermediate location...")
	if err = copyFS(os.DirFS(filepath.Join(buildDir, "build", buildSubdir, "bin")), filepath.Join(buildDir, "out", "dist"), stdout); err != nil {
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

func (gde *GDExtensionBuilder) ensureAndroidNDK(cacheDir string) (string, error) {
	ndkDirName := fmt.Sprintf("android-ndk-%s", androidNDKVersion)
	ndkPath := filepath.Join(cacheDir, ndkDirName)

	if _, err := os.Stat(ndkPath); err == nil {
		gde.logger.Info("found managed android NDK", "path", ndkPath)
		return ndkPath, nil
	}

	url, ok := androidNDKURLs[runtime.GOOS]
	if !ok {
		return "", fmt.Errorf("no android NDK download URL for OS: %s", runtime.GOOS)
	}

	zipPath := filepath.Join(cacheDir, fmt.Sprintf("android-ndk-%s.zip", androidNDKVersion))
	gde.logger.Info("downloading android NDK", "url", url, "dest", zipPath)

	if err := downloadFile(url, zipPath); err != nil {
		return "", fmt.Errorf("failed to download NDK: %w", err)
	}
	defer os.Remove(zipPath)

	gde.logger.Info("extracting android NDK", "src", zipPath, "dest", cacheDir)
	if err := unzip(zipPath, cacheDir); err != nil {
		return "", fmt.Errorf("failed to extract NDK: %w", err)
	}

	return ndkPath, nil
}

func (gde *GDExtensionBuilder) ensureEmscripten(cacheDir string, stdout, stderr io.Writer) (string, error) {
	emsdkDir := filepath.Join(cacheDir, "emsdk")

	if _, err := os.Stat(emsdkDir); err != nil {
		url := "https://github.com/emscripten-core/emsdk/archive/refs/heads/main.zip"
		zipPath := filepath.Join(cacheDir, "emsdk.zip")
		gde.logger.Info("downloading emsdk", "url", url, "dest", zipPath)

		if err := downloadFile(url, zipPath); err != nil {
			return "", fmt.Errorf("failed to download emsdk: %w", err)
		}
		defer os.Remove(zipPath)

		gde.logger.Info("extracting emsdk", "src", zipPath, "dest", cacheDir)
		if err := unzip(zipPath, cacheDir); err != nil {
			return "", fmt.Errorf("failed to extract emsdk: %w", err)
		}

		// Rename emsdk-main to emsdk
		if err := os.Rename(filepath.Join(cacheDir, "emsdk-main"), emsdkDir); err != nil {
			return "", fmt.Errorf("failed to rename emsdk dir: %w", err)
		}
	}

	gde.logger.Info("checking emsdk version", "version", emscriptenVersion)

	emsdkBin := "./emsdk"
	if runtime.GOOS == "windows" {
		emsdkBin = "emsdk.bat"
	}

	// Check if already installed to avoid re-running install (which checks network)
	// This is a heuristic: checks for upstream/emscripten directory
	if _, err := os.Stat(filepath.Join(emsdkDir, "upstream", "emscripten")); err != nil {
		gde.logger.Info("installing emsdk", "version", emscriptenVersion)
		cmd := exec.Command(emsdkBin, "install", emscriptenVersion)
		cmd.Dir = emsdkDir
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to install emsdk version %s: %w", emscriptenVersion, err)
		}
	}

	gde.logger.Info("activating emsdk", "version", emscriptenVersion)
	cmd := exec.Command(emsdkBin, "activate", emscriptenVersion)
	cmd.Dir = emsdkDir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to activate emsdk version %s: %w", emscriptenVersion, err)
	}

	return emsdkDir, nil
}

func downloadFile(url, filepath string) error {
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	_, err = io.Copy(out, resp.Body)
	return err
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)

		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", fpath)
		}

		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			linkTarget, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}

			if err := os.Symlink(string(linkTarget), fpath); err != nil {
				return err
			}
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)

		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}
	return nil
}

func copyFS(src fs.FS, dst string, logWriter io.Writer) error {
	fileCount := 0
	return fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
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
