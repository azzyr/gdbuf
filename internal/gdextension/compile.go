package gdextension

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

const androidNDKVersion = "28.1.13356709"

var zigTargets = map[string]string{
	"linux-x86_64":   "x86_64-linux-gnu.2.28",
	"linux-arm64":    "aarch64-linux-gnu.2.28",
	"windows-x86_64": "x86_64-windows-gnu",
	"windows-arm64":  "aarch64-windows-gnu",
	"macos-x86_64":   "x86_64-macos",
	"macos-arm64":    "aarch64-macos",
}

var androidTargets = map[string]string{"arm64": "aarch64-linux-android24", "x86_64": "x86_64-linux-android24"}

var libExts = map[string]string{"linux": ".so", "windows": ".dll", "macos": ".dylib", "web": ".wasm", "android": ".so"}

type toolchain struct {
	cc, cxx                  []string
	flags, cxxflags, ldflags []string
	ext                      string
	bits                     string
}

// flags mirror godot-cpp's tools/<platform>.py and tools/common_compiler_flags.py
func newToolchain(platform, arch string, threads, debug bool) (toolchain, error) {
	tc := toolchain{ext: libExts[platform], bits: "64"}
	switch {
	case zigTargets[platform+"-"+arch] != "":
		zig := cmp.Or(os.Getenv("ZIG"), "zig")
		if _, err := exec.LookPath(zig); err != nil {
			return tc, fmt.Errorf("zig not found, install it or set ZIG to its path: %w", err)
		}
		target := zigTargets[platform+"-"+arch]
		tc.cc, tc.cxx = []string{zig, "cc", "-target", target}, []string{zig, "c++", "-target", target}
	case platform == "web" && arch == "wasm32":
		if _, err := exec.LookPath("emcc"); err != nil {
			return tc, fmt.Errorf("emcc not found, install emsdk and activate it: %w", err)
		}
		// Godot loads web extensions as emscripten side modules
		tc.cc, tc.cxx, tc.bits = []string{"emcc"}, []string{"em++"}, "32"
		web := []string{"-sSIDE_MODULE=1", "-sSUPPORT_LONGJMP=wasm"}
		if threads {
			web = append(web, "-sUSE_PTHREADS=1")
		}
		tc.flags = slices.Concat(web, []string{"-include", "stdlib.h"})
		tc.ldflags = slices.Concat(web, []string{"-sWASM_BIGINT"})
	case platform == "android" && androidTargets[arch] != "":
		// zig ships no bionic libc
		bin, err := androidToolchainBin()
		if err != nil {
			return tc, err
		}
		target := "--target=" + androidTargets[arch]
		tc.cc, tc.cxx = []string{filepath.Join(bin, "clang"), target}, []string{filepath.Join(bin, "clang++"), target}
	default:
		return tc, fmt.Errorf("unsupported platform %s-%s", platform, arch)
	}

	defines := []string{"GDEXTENSION", "NDEBUG", strings.ToUpper(platform) + "_ENABLED"}
	if platform != "windows" {
		defines = append(defines, "UNIX_ENABLED")
	}
	if threads {
		defines = append(defines, "THREADS_ENABLED")
	}
	opt := "-O3"
	if debug {
		opt = "-O2"
		defines = append(defines, "DEBUG_ENABLED", "HOT_RELOAD_ENABLED")
	}
	for _, d := range defines {
		tc.flags = append(tc.flags, "-D"+d)
	}
	tc.flags = append(tc.flags, opt, "-fvisibility=hidden")
	tc.cxxflags = []string{"-std=c++20", "-fno-exceptions"}
	tc.ldflags = append(tc.ldflags, "-shared", "-fvisibility=hidden")
	switch platform {
	case "linux":
		tc.flags = append(tc.flags, "-fPIC", "-Wwrite-strings")
		// zig's link lacks __cxa_finalize, so static destructors would run after Godot unmaps the library
		tc.ldflags = append(tc.ldflags, "-Wl,-z,nodelete")
	case "android":
		tc.flags = append(tc.flags, "-fPIC")
	case "windows":
		tc.flags = append(tc.flags, "-Wwrite-strings")
		// otherwise the import library lands in dist
		tc.ldflags = append(tc.ldflags, "-Wl,--no-undefined", "-Wl,--out-implib,obj/import.lib")
	}
	if platform != "web" {
		tc.ldflags = append(tc.ldflags, "-s")
	}
	return tc, nil
}

func androidToolchainBin() (string, error) {
	ndk := os.Getenv("ANDROID_NDK_ROOT")
	if ndk == "" && os.Getenv("ANDROID_HOME") != "" {
		ndk = filepath.Join(os.Getenv("ANDROID_HOME"), "ndk", androidNDKVersion)
	}
	if ndk == "" {
		return "", fmt.Errorf("android needs the NDK: set ANDROID_NDK_ROOT, or ANDROID_HOME with ndk/%s installed", androidNDKVersion)
	}
	host := map[string]string{"linux": "linux-x86_64", "darwin": "darwin-x86_64", "windows": "windows-x86_64"}[runtime.GOOS]
	bin := filepath.Join(ndk, "toolchains", "llvm", "prebuilt", host, "bin")
	if _, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("could not find NDK toolchain at %s: %w", bin, err)
	}
	return bin, nil
}

// in the object cache key, so a gdbuf with a different godot-cpp rebuilds it
var buildEnvHash = sync.OnceValue(func() string {
	h := sha256.New()
	fs.WalkDir(buildEnvFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, _ := buildEnvFS.ReadFile(path)
			h.Write([]byte(path))
			h.Write(data)
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
})

func compileAndLink(buildDir, srcDir, outPath string, tc toolchain, stdout, stderr io.Writer) error {
	gen := filepath.Join(buildDir, "godot-cpp-gen", tc.bits)
	if _, err := os.Stat(gen); err != nil {
		return fmt.Errorf("this gdbuf was built without godot-cpp bindings, build it with make: %w", err)
	}
	godotCpp, nanopb := filepath.Join(buildDir, "godot-cpp"), filepath.Join(buildDir, "nanopb")

	var godotSources []string
	for _, dir := range []string{"src", "src/classes", "src/core", "src/variant"} {
		matches, _ := filepath.Glob(filepath.Join(godotCpp, dir, "*.cpp"))
		godotSources = append(godotSources, matches...)
	}
	genSources, _, err := findSources(filepath.Join(gen, "src"))
	if err != nil {
		return err
	}
	godotSources = append(godotSources, genSources...)
	userSources, userDirs, err := findSources(srcDir)
	if err != nil {
		return err
	}
	for _, f := range []string{"pb_common.c", "pb_encode.c", "pb_decode.c"} {
		userSources = append(userSources, filepath.Join(nanopb, f))
	}

	flags := slices.Clone(tc.flags)
	// generated headers include each other relative to their proto directory
	for _, inc := range append([]string{godotCpp + "/gdextension", godotCpp + "/include", gen + "/include", nanopb}, userDirs...) {
		flags = append(flags, "-I"+filepath.ToSlash(inc))
	}
	sum := sha256.Sum256([]byte(strings.Join(slices.Concat(tc.cc, tc.cxx, flags, tc.cxxflags), "\x00") + buildEnvHash()))
	objDir := filepath.Join(buildDir, "obj", hex.EncodeToString(sum[:8]))
	userObjDir := filepath.Join(objDir, "user")

	godotObjects, err := compileAll(godotSources, objDir, tc, flags, stdout, stderr, true)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(userObjDir); err != nil {
		return err
	}
	userObjects, err := compileAll(userSources, userObjDir, tc, flags, stdout, stderr, false)
	if err != nil {
		return err
	}

	// Windows command line limit
	var rsp strings.Builder
	for _, obj := range append(godotObjects, userObjects...) {
		rsp.WriteString("\"" + filepath.ToSlash(obj) + "\"\n")
	}
	rspPath := filepath.Join(objDir, "link.rsp")
	if err := os.WriteFile(rspPath, []byte(rsp.String()), 0644); err != nil {
		return fmt.Errorf("could not write link response file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	absOut, err := filepath.Abs(outPath)
	if err != nil {
		return err
	}
	cmd := exec.Command(tc.cxx[0], slices.Concat(tc.cxx[1:], []string{"-o", absOut, "@" + filepath.ToSlash(rspPath)}, tc.ldflags)...)
	cmd.Dir = buildDir
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not link %s: %w", outPath, err)
	}
	return nil
}

func compileAll(sources []string, objDir string, tc toolchain, flags []string, stdout, stderr io.Writer, cache bool) ([]string, error) {
	objects := make([]string, len(sources))
	for i := range sources {
		objects[i] = filepath.Join(objDir, fmt.Sprintf("%d.o", i))
	}
	done := filepath.Join(objDir, "done")
	if _, err := os.Stat(done); cache && err == nil {
		return objects, nil
	}
	if err := os.MkdirAll(objDir, 0755); err != nil {
		return nil, err
	}
	fmt.Fprintf(stdout, "compiling %d sources in %s\n", len(sources), objDir)

	var g errgroup.Group
	g.SetLimit(runtime.NumCPU())
	var outMu sync.Mutex
	for i, src := range sources {
		g.Go(func() error {
			args := slices.Concat(tc.cxx, flags, tc.cxxflags)
			if strings.HasSuffix(src, ".c") {
				args = slices.Concat(tc.cc, flags)
			}
			cmd := exec.Command(args[0], append(args[1:], "-c", src, "-o", objects[i])...)
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			err := cmd.Run()
			outMu.Lock()
			stderr.Write(out.Bytes())
			outMu.Unlock()
			if err != nil {
				return fmt.Errorf("could not compile %s: %w", src, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	if cache {
		if err := os.WriteFile(done, nil, 0644); err != nil {
			return nil, err
		}
	}
	return objects, nil
}

func findSources(root string) (sources, dirs []string, err error) {
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, path)
		} else if ext := filepath.Ext(path); ext == ".cpp" || ext == ".cc" || ext == ".c" {
			sources = append(sources, path)
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("could not list sources in %s: %w", root, err)
	}
	return sources, dirs, nil
}
