.PHONY: build-linux build-windows build-macos build-web build-android deps-update
CMAKE ?= cmake

# Linux
build-linux: build-linux-debug-single build-linux-release-single

build-linux-debug-single:
	$(CMAKE) -B build/linux/debug_single -DCMAKE_CXX_COMPILER=clang++ -DCMAKE_SYSTEM_NAME=linux -DCMAKE_SYSTEM_PROCESSOR=x86_64 -DGODOTCPP_TARGET=template_debug -DGODOTCPP_PRECISION=single -DCMAKE_TOOLCHAIN_FILE=cmake/x86_64-linux.toolchain.cmake
	$(CMAKE) --build build/linux/debug_single
	mkdir -p build/linux/bin
	cp build/linux/debug_single/bin/* build/linux/bin/
	cp build/linux/debug_single/compile_commands.json ./

build-linux-release-single:
	$(CMAKE) -B build/linux/release_single -DCMAKE_CXX_COMPILER=clang++ -DCMAKE_SYSTEM_NAME=linux -DCMAKE_SYSTEM_PROCESSOR=x86_64 -DGODOTCPP_TARGET=template_release -DGODOTCPP_PRECISION=single -DCMAKE_TOOLCHAIN_FILE=cmake/x86_64-linux.toolchain.cmake
	$(CMAKE) --build build/linux/release_single
	mkdir -p build/linux/bin
	cp build/linux/release_single/bin/* build/linux/bin/

build-linux-debug-double:
	$(CMAKE) -B build/linux/debug_double -DCMAKE_CXX_COMPILER=clang++ -DCMAKE_SYSTEM_NAME=linux -DCMAKE_SYSTEM_PROCESSOR=x86_64 -DGODOTCPP_TARGET=template_debug -DGODOTCPP_PRECISION=double -DCMAKE_TOOLCHAIN_FILE=cmake/x86_64-linux.toolchain.cmake
	$(CMAKE) --build build/linux/debug_double
	mkdir -p build/linux/bin
	cp build/linux/debug_double/bin/* build/linux/bin/

build-linux-release-double:
	$(CMAKE) -B build/linux/release_double -DCMAKE_CXX_COMPILER=clang++ -DCMAKE_SYSTEM_NAME=linux -DCMAKE_SYSTEM_PROCESSOR=x86_64 -DGODOTCPP_TARGET=template_release -DGODOTCPP_PRECISION=double -DCMAKE_TOOLCHAIN_FILE=cmake/x86_64-linux.toolchain.cmake
	$(CMAKE) --build build/linux/release_double
	mkdir -p build/linux/bin
	cp build/linux/release_double/bin/* build/linux/bin/

# Windows
build-windows: build-windows-debug-single build-windows-release-single

build-windows-debug-single:
	$(CMAKE) -B build/windows/debug_single -DCMAKE_SYSTEM_NAME=Windows -DCMAKE_SYSTEM_PROCESSOR=x86_64 -DGODOTCPP_TARGET=template_debug -DGODOTCPP_PRECISION=single -DCMAKE_TOOLCHAIN_FILE=cmake/x86_64-windows.toolchain.cmake
	$(CMAKE) --build build/windows/debug_single
	mkdir -p build/windows/bin
	cp build/windows/debug_single/bin/* build/windows/bin/

build-windows-release-single:
	$(CMAKE) -B build/windows/release_single -DCMAKE_SYSTEM_NAME=Windows -DCMAKE_SYSTEM_PROCESSOR=x86_64 -DGODOTCPP_TARGET=template_release -DGODOTCPP_PRECISION=single -DCMAKE_TOOLCHAIN_FILE=cmake/x86_64-windows.toolchain.cmake
	$(CMAKE) --build build/windows/release_single
	mkdir -p build/windows/bin
	cp build/windows/release_single/bin/* build/windows/bin/

build-windows-debug-double:
	$(CMAKE) -B build/windows/debug_double -DCMAKE_SYSTEM_NAME=Windows -DCMAKE_SYSTEM_PROCESSOR=x86_64 -DGODOTCPP_TARGET=template_debug -DGODOTCPP_PRECISION=double -DCMAKE_TOOLCHAIN_FILE=cmake/x86_64-windows.toolchain.cmake
	$(CMAKE) --build build/windows/debug_double
	mkdir -p build/windows/bin
	cp build/windows/debug_double/bin/* build/windows/bin/

build-windows-release-double:
	$(CMAKE) -B build/windows/release_double -DCMAKE_SYSTEM_NAME=Windows -DCMAKE_SYSTEM_PROCESSOR=x86_64 -DGODOTCPP_TARGET=template_release -DGODOTCPP_PRECISION=double -DCMAKE_TOOLCHAIN_FILE=cmake/x86_64-windows.toolchain.cmake
	$(CMAKE) --build build/windows/release_double
	mkdir -p build/windows/bin
	cp build/windows/release_double/bin/* build/windows/bin/

# macOS
build-macos-x86_64: build-macos-x86_64-debug-single build-macos-x86_64-release-single
build-macos-arm64:  build-macos-arm64-debug-single  build-macos-arm64-release-single

build-macos-x86_64-debug-single:
	$(CMAKE) -B build/macos-x86_64/debug_single -DCMAKE_TOOLCHAIN_FILE=cmake/x86_64-macos.toolchain.cmake -DGODOTCPP_TARGET=template_debug -DGODOTCPP_PRECISION=single
	$(CMAKE) --build build/macos-x86_64/debug_single
	mkdir -p build/macos-x86_64/bin
	cp build/macos-x86_64/debug_single/bin/* build/macos-x86_64/bin/

build-macos-x86_64-release-single:
	$(CMAKE) -B build/macos-x86_64/release_single -DCMAKE_TOOLCHAIN_FILE=cmake/x86_64-macos.toolchain.cmake -DGODOTCPP_TARGET=template_release -DGODOTCPP_PRECISION=single
	$(CMAKE) --build build/macos-x86_64/release_single
	mkdir -p build/macos-x86_64/bin
	cp build/macos-x86_64/release_single/bin/* build/macos-x86_64/bin/

build-macos-x86_64-debug-double:
	$(CMAKE) -B build/macos-x86_64/debug_double -DCMAKE_TOOLCHAIN_FILE=cmake/x86_64-macos.toolchain.cmake -DGODOTCPP_TARGET=template_debug -DGODOTCPP_PRECISION=double
	$(CMAKE) --build build/macos-x86_64/debug_double
	mkdir -p build/macos-x86_64/bin
	cp build/macos-x86_64/debug_double/bin/* build/macos-x86_64/bin/

build-macos-x86_64-release-double:
	$(CMAKE) -B build/macos-x86_64/release_double -DCMAKE_TOOLCHAIN_FILE=cmake/x86_64-macos.toolchain.cmake -DGODOTCPP_TARGET=template_release -DGODOTCPP_PRECISION=double
	$(CMAKE) --build build/macos-x86_64/release_double
	mkdir -p build/macos-x86_64/bin
	cp build/macos-x86_64/release_double/bin/* build/macos-x86_64/bin/

build-macos-arm64-debug-single:
	$(CMAKE) -B build/macos-arm64/debug_single -DCMAKE_TOOLCHAIN_FILE=cmake/arm64-macos.toolchain.cmake -DGODOTCPP_TARGET=template_debug -DGODOTCPP_PRECISION=single
	$(CMAKE) --build build/macos-arm64/debug_single
	mkdir -p build/macos-arm64/bin
	cp build/macos-arm64/debug_single/bin/* build/macos-arm64/bin/

build-macos-arm64-release-single:
	$(CMAKE) -B build/macos-arm64/release_single -DCMAKE_TOOLCHAIN_FILE=cmake/arm64-macos.toolchain.cmake -DGODOTCPP_TARGET=template_release -DGODOTCPP_PRECISION=single
	$(CMAKE) --build build/macos-arm64/release_single
	mkdir -p build/macos-arm64/bin
	cp build/macos-arm64/release_single/bin/* build/macos-arm64/bin/

build-macos-arm64-debug-double:
	$(CMAKE) -B build/macos-arm64/debug_double -DCMAKE_TOOLCHAIN_FILE=cmake/arm64-macos.toolchain.cmake -DGODOTCPP_TARGET=template_debug -DGODOTCPP_PRECISION=double
	$(CMAKE) --build build/macos-arm64/debug_double
	mkdir -p build/macos-arm64/bin
	cp build/macos-arm64/debug_double/bin/* build/macos-arm64/bin/

build-macos-arm64-release-double:
	$(CMAKE) -B build/macos-arm64/release_double -DCMAKE_TOOLCHAIN_FILE=cmake/arm64-macos.toolchain.cmake -DGODOTCPP_TARGET=template_release -DGODOTCPP_PRECISION=double
	$(CMAKE) --build build/macos-arm64/release_double
	mkdir -p build/macos-arm64/bin
	cp build/macos-arm64/release_double/bin/* build/macos-arm64/bin/

# Web
build-web: build-web-debug-single build-web-release-single build-web-debug-single-nothreads build-web-release-single-nothreads

build-web-debug-single:
	$(CMAKE) -B build/web/debug_single -DCMAKE_TOOLCHAIN_FILE=cmake/web-wasm32.toolchain.cmake -DGODOTCPP_TARGET=template_debug -DGODOTCPP_PRECISION=single -DGODOTCPP_THREADS=ON
	$(CMAKE) --build build/web/debug_single
	mkdir -p build/web/bin
	cp build/web/debug_single/bin/* build/web/bin/

build-web-release-single:
	$(CMAKE) -B build/web/release_single -DCMAKE_TOOLCHAIN_FILE=cmake/web-wasm32.toolchain.cmake -DGODOTCPP_TARGET=template_release -DGODOTCPP_PRECISION=single -DGODOTCPP_THREADS=ON
	$(CMAKE) --build build/web/release_single
	mkdir -p build/web/bin
	cp build/web/release_single/bin/* build/web/bin/

build-web-debug-single-nothreads:
	$(CMAKE) -B build/web/debug_single_nothreads -DCMAKE_TOOLCHAIN_FILE=cmake/web-wasm32.toolchain.cmake -DGODOTCPP_TARGET=template_debug -DGODOTCPP_PRECISION=single -DGODOTCPP_THREADS=OFF
	$(CMAKE) --build build/web/debug_single_nothreads
	mkdir -p build/web/bin
	cp build/web/debug_single_nothreads/bin/* build/web/bin/

build-web-release-single-nothreads:
	$(CMAKE) -B build/web/release_single_nothreads -DCMAKE_TOOLCHAIN_FILE=cmake/web-wasm32.toolchain.cmake -DGODOTCPP_TARGET=template_release -DGODOTCPP_PRECISION=single -DGODOTCPP_THREADS=OFF
	$(CMAKE) --build build/web/release_single_nothreads
	mkdir -p build/web/bin
	cp build/web/release_single_nothreads/bin/* build/web/bin/

build-web-debug-double:
	$(CMAKE) -B build/web/debug_double -DCMAKE_TOOLCHAIN_FILE=cmake/web-wasm32.toolchain.cmake -DGODOTCPP_TARGET=template_debug -DGODOTCPP_PRECISION=double -DGODOTCPP_THREADS=ON
	$(CMAKE) --build build/web/debug_double
	mkdir -p build/web/bin
	cp build/web/debug_double/bin/* build/web/bin/

build-web-release-double:
	$(CMAKE) -B build/web/release_double -DCMAKE_TOOLCHAIN_FILE=cmake/web-wasm32.toolchain.cmake -DGODOTCPP_TARGET=template_release -DGODOTCPP_PRECISION=double -DGODOTCPP_THREADS=ON
	$(CMAKE) --build build/web/release_double
	mkdir -p build/web/bin
	cp build/web/release_double/bin/* build/web/bin/

# Android
build-android: build-android-debug-single build-android-release-single build-android-debug-single-nothreads build-android-release-single-nothreads

build-android-debug-single:
	$(CMAKE) -B build/android/debug_single -DCMAKE_TOOLCHAIN_FILE=cmake/android.toolchain.cmake -DGODOTCPP_TARGET=template_debug -DGODOTCPP_PRECISION=single -DANDROID_ABI=arm64-v8a -DANDROID_PLATFORM_LEVEL=24 -DGODOTCPP_THREADS=ON
	$(CMAKE) --build build/android/debug_single
	mkdir -p build/android/bin
	cp build/android/debug_single/bin/* build/android/bin/

build-android-release-single:
	$(CMAKE) -B build/android/release_single -DCMAKE_TOOLCHAIN_FILE=cmake/android.toolchain.cmake -DGODOTCPP_TARGET=template_release -DGODOTCPP_PRECISION=single -DANDROID_ABI=arm64-v8a -DANDROID_PLATFORM_LEVEL=24 -DGODOTCPP_THREADS=ON
	$(CMAKE) --build build/android/release_single
	mkdir -p build/android/bin
	cp build/android/release_single/bin/* build/android/bin/

build-android-debug-single-nothreads:
	$(CMAKE) -B build/android/debug_single_nothreads -DCMAKE_TOOLCHAIN_FILE=cmake/android.toolchain.cmake -DGODOTCPP_TARGET=template_debug -DGODOTCPP_PRECISION=single -DANDROID_ABI=arm64-v8a -DANDROID_PLATFORM_LEVEL=24 -DGODOTCPP_THREADS=OFF
	$(CMAKE) --build build/android/debug_single_nothreads
	mkdir -p build/android/bin
	cp build/android/debug_single_nothreads/bin/* build/android/bin/

build-android-release-single-nothreads:
	$(CMAKE) -B build/android/release_single_nothreads -DCMAKE_TOOLCHAIN_FILE=cmake/android.toolchain.cmake -DGODOTCPP_TARGET=template_release -DGODOTCPP_PRECISION=single -DANDROID_ABI=arm64-v8a -DANDROID_PLATFORM_LEVEL=24 -DGODOTCPP_THREADS=OFF
	$(CMAKE) --build build/android/release_single_nothreads
	mkdir -p build/android/bin
	cp build/android/release_single_nothreads/bin/* build/android/bin/

build-android-debug-double:
	$(CMAKE) -B build/android/debug_double -DCMAKE_TOOLCHAIN_FILE=cmake/android.toolchain.cmake -DGODOTCPP_TARGET=template_debug -DGODOTCPP_PRECISION=double -DANDROID_ABI=arm64-v8a -DANDROID_PLATFORM_LEVEL=24 -DGODOTCPP_THREADS=ON
	$(CMAKE) --build build/android/debug_double
	mkdir -p build/android/bin
	cp build/android/debug_double/bin/* build/android/bin/

build-android-release-double:
	$(CMAKE) -B build/android/release_double -DCMAKE_TOOLCHAIN_FILE=cmake/android.toolchain.cmake -DGODOTCPP_TARGET=template_release -DGODOTCPP_PRECISION=double -DANDROID_ABI=arm64-v8a -DANDROID_PLATFORM_LEVEL=24 -DGODOTCPP_THREADS=ON
	$(CMAKE) --build build/android/release_double
	mkdir -p build/android/bin
	cp build/android/release_double/bin/* build/android/bin/

deps-update:
	git submodule update --remote --recursive
