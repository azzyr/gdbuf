set(CMAKE_SYSTEM_NAME Darwin)
set(CMAKE_SYSTEM_PROCESSOR arm64)

if(NOT DEFINED ENV{OSXCROSS_TARGET})
    message(FATAL_ERROR "OSXCROSS_TARGET is not set.\n")
endif()

set(OSXCROSS_BIN "$ENV{OSXCROSS_TARGET}/bin")

file(GLOB _cxx_candidates
    "${OSXCROSS_BIN}/arm64-apple-darwin*-clang++"
    "${OSXCROSS_BIN}/aarch64-apple-darwin*-clang++")
if(NOT _cxx_candidates)
    message(FATAL_ERROR "Could not find arm64-apple-darwin*-clang++ in ${OSXCROSS_BIN}.\n")
endif()
list(GET _cxx_candidates 0 _cxx)
get_filename_component(_cxx_name "${_cxx}" NAME)
string(REGEX REPLACE "-clang\\+\\+$" "" _triple "${_cxx_name}")

message(STATUS "OSXCross arm64 triple: ${_triple}")

set(CMAKE_C_COMPILER   "${OSXCROSS_BIN}/${_triple}-clang"   CACHE STRING "C compiler")
set(CMAKE_CXX_COMPILER "${OSXCROSS_BIN}/${_triple}-clang++" CACHE STRING "C++ compiler")
set(CMAKE_AR           "${OSXCROSS_BIN}/${_triple}-ar"       CACHE STRING "Archiver")
set(CMAKE_RANLIB       "${OSXCROSS_BIN}/${_triple}-ranlib"   CACHE STRING "Ranlib")

set(CMAKE_SYSROOT "$ENV{OSXCROSS_TARGET}/SDK/MacOSX.sdk")

set(CMAKE_FIND_ROOT_PATH "$ENV{OSXCROSS_TARGET}")
set(CMAKE_FIND_ROOT_PATH_MODE_PROGRAM NEVER)
set(CMAKE_FIND_ROOT_PATH_MODE_LIBRARY ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_INCLUDE ONLY)
