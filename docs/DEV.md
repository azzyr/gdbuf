# Developer Guide

`gdbuf` is a tool designed to bridge the gap between **Google Protocol Buffers** and the **Godot Engine**. Its primary goal is to automate the generation of high-performance, native C++ GDExtension code from `.proto` files, treating Protobuf messages as first-class Godot Resources.

## Architecture Overview

The program operates in a linear pipeline:
1.  **Input Parsing**: Accepts a directory of `.proto` files and optional include directories for import resolution.
2.  **Proto Compilation**: Parses the `.proto` files in-process with `github.com/bufbuild/protocompile` (no `protoc` needed) into descriptors, then runs the Nanopb plugin on them to generate C headers/sources.
3.  **Code Generation**: Parses the descriptor set to understand the message structure, then executes Go `text/template` templates to generate Godot-specific C++ wrappers.
4.  **GDExtension Compilation**: Compiles godot-cpp, the generated wrappers and the embedded **Nanopb** library into a shared library by calling the compiler directly (`compile.go`), no build system. godot-cpp is compiled once per toolchain, flag set and gdbuf version into the build cache (`~/.cache/gdbuf` or local `.gdbuf_cache`); the generated sources are recompiled every run.

## Build Dependencies

| Platform | User-Provided Dependencies | Embedded in `gdbuf` |
| :--- | :--- | :--- |
| **All Platforms** | `go` (1.25+), `python3` with `protobuf` (nanopb generator) | `godot-cpp` and its pre-generated bindings, `nanopb` |
| **Linux / Windows / macOS** | `zig` 0.17 (or `ZIG` set to its path), from any host | - |
| **Web (WASM)** | `emcc` from emsdk; Godot loads web extensions as emscripten side modules | - |
| **Android** | NDK 28.1 via `ANDROID_NDK_ROOT`, or `ANDROID_HOME` with `ndk/28.1.13356709` | - |

Compiler flags mirror godot-cpp's `tools/<platform>.py` and `tools/common_compiler_flags.py`; check them when updating the submodule.

### godot-cpp bindings

godot-cpp normally generates its bindings with Python at build time. `make build` and `make build-all` run `tools/gen_bindings.py` first, which writes them to `internal/gdextension/buildenv/godot-cpp-gen/64` (64-bit targets) and `32` (wasm32); they are gitignored and embedded in the binary, so gdbuf users need no Python. Building gdbuf therefore needs `python3` and `make`; a plain `go build` produces a gdbuf that can only `--generate-only`.

## Codebase Structure

### `cmd` / Root
-   **`main.go`**: The entry point. It parses command-line flags, sets up logging, and orchestrates the three main internal packages (`protoc`, `codegen`, `gdextension`).

### `internal/protoc`
-   **Responsibility**: Parses `.proto` files in-process with `protocompile`; `protoc` is not used.
-   **Key Functions**:
    -   `BuildDescriptorSet`: Returns the `FileDescriptorProto`s of the proto files, imports first, as `protoc --descriptor_set_out` ordered them.
    -   `CompileNanopb`: Builds a `CodeGeneratorRequest` and pipes it to the `protoc-gen-nanopb` plugin the way `protoc` would, then writes the Nanopb C headers/sources (`.pb.h`, `.pb.c`) from its response.
-   **Notes**: Enforces strict include paths (relative to current directory) to avoid aliasing issues.

### `internal/codegen`
-   **Responsibility**: Logic for mapping Protobuf concepts to Godot concepts and generating C++ code.
-   **`codegen.go`**: The main driver. It extracts data from the `FileDescriptorProto`s, populates the `templateData` struct, and executes templates. It handles recursive message flattening (for nested types) and doc comment extraction.
-   **`godot.go`**: Contains the `resolveGodotType` logic. This is the "brain" that decides that `google.protobuf.Timestamp` becomes `int64_t`, or `repeated MyMsg` becomes `godot::Array`.
-   **`templates/`**: Contains the `.tmpl` files.
    -   `gdextension/`: the `.gdextension` manifest.
    -   `src/gen_once/`: Files generated once per run (e.g., `register_types.cpp`).
    -   `src/gen_per_proto_file/`: Files generated for every proto file (e.g., `resource.h` which defines the wrapper classes).
    -   `doc/`: Templates for generating Godot XML documentation.

### `internal/gdextension`
-   **Responsibility**: Builds the final binary.
-   **`buildenv/`**: Contains the "build environment" (godot-cpp submodule, its pre-generated bindings in `godot-cpp-gen/`, nanopb submodule, `build_profile.json`) embedded into the Go binary.
-   **`gdextension.go`**: Manages the build process. It sets up a persistent build cache to avoid recompiling `godot-cpp` on every run, copies generated sources into it, and picks the target, flags and library name.
-   **`compile.go`**: Per-platform toolchains (zig, emcc, NDK clang), the parallel compile with the cached godot-cpp objects, and the link.

## Key Concepts

### Type Mapping (`godot.go`)
We map standard Proto types to Godot Variants:
-   `string` -> `godot::String`
-   `map<K,V>` -> `godot::Dictionary` (with custom serialization logic in templates)
-   `oneof` -> Properties for each option, handled via explicit getters/setters.

### Resource Wrapper (`resource.h.tmpl`)
Every Protobuf message is wrapped in a class inheriting from `godot::Resource`.
-   **Properties**: Fields are registered with `ADD_PROPERTY`, making them visible in the Godot Inspector.
-   **Serialization**: We generate `to_byte_array()` and `from_byte_array()` methods that internally use **Nanopb** functions (`pb_encode`, `pb_decode`) to serialize to/from the standard Protobuf binary format.
-   **Memory Management**: Custom message fields (nested messages) are stored using `godot::Ref<T>`. This ensures that the Godot RefCounted system correctly manages the lifecycle of nested resources, preventing dangling pointers and memory leaks.

## Development Workflow

1.  **Make Changes**: Edit Go code or Templates.
2.  **Rebuild Tool**: Run `make build` to update `bin/gdbuf`.
3.  **Test**: Run `make test-full`. This:
    -   Cleans previous outputs.
    -   Builds the GDExtension for **all supported platforms** (Linux, Windows, Web, Android) using `test-all-platforms`.
    -   Installs the artifacts into a test Godot project.
    -   Runs a headless Godot instance (on the host) to execute GDScript integration tests (`test-godot`).
    -   *Note*: This requires all cross-compilers to be present. For a faster, host-only test (Linux), use `make test-linux`.
    -   **Platform Specific Tests**:
        -   `make test-linux`: Builds and tests for Linux.
        -   `make test-web`: Builds for Web (wasm32).
        -   `make test-windows`: Builds for Windows.
        -   `make test-android`: Builds for Android.

## Future Improvements
-   **Nested Enums**: Currently top-level enums work best. Nested enums map to `int` but don't generate C++ enum definitions in the wrapper namespace.
-   **Platform Support**: The Go code supports detecting platforms.
    -   **Linux/Windows/macOS**: Built with zig from any host.
    -   **Android**: Built with the NDK's clang; the NDK is not downloaded automatically.
    -   **Web**: Built with `emcc`; emsdk is not downloaded automatically and needs Python 3.
-   **Map Values (Structs)**: Support for `map<Key, Message>` with Nanopb needs verification for complex nested types.
