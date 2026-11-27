# gdbuf

Compile Protobuf schemas into a native C++ GDExtension

## Features

- Every message is a `Resource`, editable in the inspector and saved as `.tres`
- `to_byte_array()` / `from_byte_array()` for the wire format
- proto comments become editor docs
- top-level enums live on a global class named after the extension

## Requirements

- go 1.25+
- scons and a C++20 compiler (gcc, clang, msvc, or mingw for windows)
- emscripten for web builds (`emcc` on your PATH)

## Build

```bash
git clone --recursive https://github.com/azzyr/gdbuf.git
cd gdbuf
make build
```

Or grab a binary from the releases.

## Usage

```bash
./bin/gdbuf --proto ./protos --out ./addons/myproto --name myproto --platform linux,windows
```

- `--proto` dir with your `.proto` files, required
- `--include` extra import dir, can repeat
- `--out` where the built extension goes, `./out` by default
- `--genout` where the generated C++ goes, `.` by default
- `--name` extension name, `gdbufgen` by default
- `--platform` `linux`, `windows`, `macos-x86_64`, `macos-arm64`, `web`, `android`, a comma list, or `all`. leave it out to only generate
- `--generate-only` generate C++ but don't compile
- `--cache` build cache dir, keeps godot-cpp objects between runs. `~/.cache/gdbuf` by default
- `--debug` debug build
- `--threaded` threaded web build

## In Godot

Restart the editor after dropping the extension in.

```gdscript
var msg = Player.new()
msg.name = "player 1"
msg.health = 100

var bytes = msg.to_byte_array()

var copy = Player.new()
copy.from_byte_array(bytes)
```

## Docs

- [features & supported types](docs/FEATURES.md)
- [GDScript API](docs/API.md)
- [dev notes](docs/DEV.md)
