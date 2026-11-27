# Developer Guide

`gdbuf` is a tool designed to bridge the gap between **Google Protocol Buffers** and the **Godot Engine**. Its primary goal is to automate the generation of high-performance, native C++ GDExtension code from `.proto` files, treating Protobuf messages as first-class Godot Resources.

## Architecture Overview

The program operates in a linear pipeline:
1.  **Input Parsing**: Accepts a directory of `.proto` files and optional include directories for import resolution.
2.  **Proto Compilation**: Parses the `.proto` files in-process with `github.com/bufbuild/protocompile` (no `protoc` needed) into descriptors.
3.  **Code Generation**: Parses the descriptor set to understand the message structure, then executes Go `text/template` templates to generate Godot-specific C++ wrappers.
4.  **GDExtension Compilation**: Runs scons on the generated `SConstruct` to compile godot-cpp, the generated wrappers and the embedded **Nanopb** runtime into a shared library. It uses a persistent build cache (in `~/.cache/gdbuf` or local `.gdbuf_cache`) so godot-cpp is not rebuilt every run.

## Build Dependencies

| Platform | User-Provided Dependencies | Embedded in `gdbuf` |
| :--- | :--- | :--- |
| **All Platforms** | `go` (1.25+), `scons` | `godot-cpp`, the `nanopb` runtime |
| **Linux** | `gcc` or `clang++` | - |
| **Windows (Cross)** | MinGW-w64 (`x86_64-w64-mingw32-g++`) | - |
| **macOS** | a Mac with Xcode command line tools, or OSXCross | - |
| **Web (WASM)** | `emcc` from emsdk | - |
| **Android** | NDK 28.1 via `ANDROID_HOME` | - |

## Codebase Structure

### `cmd` / Root
-   **`main.go`**: The entry point. It parses command-line flags, sets up logging, and orchestrates the three main internal packages (`protoc`, `codegen`, `gdextension`).

### `internal/protoc`
-   **Responsibility**: Parses `.proto` files in-process with `protocompile`; `protoc` is not used.
-   **Key Functions**:
    -   `BuildDescriptorSet`: Returns the `FileDescriptorProto`s of the proto files, imports first, as `protoc --descriptor_set_out` ordered them.
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
-   **`buildenv/`**: Contains the "build environment" (godot-cpp submodule, nanopb submodule, `build_profile.json`) embedded into the Go binary.
-   **`gdextension.go`**: Manages the build process. It sets up a persistent build cache to avoid recompiling `godot-cpp` on every run, copies generated sources into it and runs scons.

## Key Concepts

### Type Mapping (`godot.go`)
We map standard Proto types to Godot Variants:
-   `string` -> `godot::String`
-   `map<K,V>` -> `godot::Dictionary` (with custom serialization logic in templates)
-   `oneof` -> Properties for each option, handled via explicit getters/setters.

### Resource Wrapper (`resource.h.tmpl`)
Every Protobuf message is wrapped in a class inheriting from `godot::Resource`.
-   **Properties**: Fields are registered with `ADD_PROPERTY`, making them visible in the Godot Inspector.
-   **Serialization**: Each class gets generated `encode_fields`, `decode_fields` and `clear_fields`, one line per field, calling the helpers in `messages.h.tmpl`. Those write the protobuf wire format straight from the Godot members with nanopb's low-level stream functions (`pb_encode_varint`, `pb_decode_tag`, substreams); nanopb's generator and structs are not used. `codecKind` in `codegen.go` picks the helper (`gdbuf::Kind`) for each field.
-   **Memory Management**: Custom message fields (nested messages) are stored using `godot::Ref<T>`. This ensures that the Godot RefCounted system correctly manages the lifecycle of nested resources, preventing dangling pointers and memory leaks.

## Development Workflow

1.  **Make Changes**: Edit Go code or Templates.
2.  **Rebuild Tool**: Run `make build` to update `bin/gdbuf`.

## Future Improvements
-   **Nested Enums**: Currently top-level enums work best. Nested enums map to `int` but don't generate C++ enum definitions in the wrapper namespace.
-   **Platform Support**: The Go code supports detecting platforms.
    -   **Linux/Windows/macOS**: Built through godot-cpp's scons platform tools.
    -   **Android**: Needs the NDK; it is not downloaded automatically.
    -   **Web**: Needs `emcc`; emsdk is not downloaded automatically.
-   **Extension names**: `--name` equal to a proto file's name (`--name foo` with `foo.proto`) does not compile; the global class and the file's namespace collide.
