#!/usr/bin/env python3
"""Pre-generate godot-cpp bindings so gdbuf users need no Python. Run by make build."""
import os
import shutil
import sys

root = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "internal", "gdextension", "buildenv")
godot_cpp = os.path.join(root, "godot-cpp")
sys.path.insert(0, godot_cpp)
# a __pycache__ in godot-cpp would be embedded too, and its timestamps change the godot-cpp cache key every release
sys.dont_write_bytecode = True

from binding_generator import _generate_bindings  # noqa: E402
from build_profile import generate_trimmed_api  # noqa: E402

api_file = os.path.join(godot_cpp, "gdextension", "extension_api.json")
out_root = os.path.join(root, "godot-cpp-gen")

shutil.rmtree(out_root, ignore_errors=True)
# 64-bit targets and wasm32
for bits in ("64", "32"):
    api = generate_trimmed_api(api_file, os.path.join(root, "build_profile.json"))
    out = os.path.join(out_root, bits)
    _generate_bindings(api, api_file, True, bits, "single", out)
    for d in ("include", "src"):
        shutil.move(os.path.join(out, "gen", d), os.path.join(out, d))
    os.rmdir(os.path.join(out, "gen"))
