package gdextension

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const manifestName = "gde-protobuf.gdextension"

var libraryExts = []string{".so", ".dll", ".dylib", ".wasm"}

// Finalize copies the docs and writes the .gdextension listing every library found in outputDir/dist.
func Finalize(genDir, outputDir string) error {
	if err := copyFS(os.DirFS(filepath.Join(genDir, "doc_classes")), filepath.Join(outputDir, "doc_classes")); err != nil {
		return fmt.Errorf("could not copy docs: %w", err)
	}

	header, err := os.ReadFile(filepath.Join(genDir, "out", manifestName))
	if err != nil {
		return fmt.Errorf("could not read generated manifest: %w", err)
	}

	var files []string
	entries, err := os.ReadDir(filepath.Join(outputDir, "dist"))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not read dist directory: %w", err)
	}
	for _, e := range entries {
		files = append(files, e.Name())
	}

	content := string(header) + strings.Join(libraryEntries(files), "\n") + "\n"
	return os.WriteFile(filepath.Join(outputDir, manifestName), []byte(content), 0644)
}

// lib<name>.<platform>.<target>.<arch>[.nothreads].<ext>
func libraryEntries(files []string) []string {
	type lib struct{ base, target, file string }
	var libs []lib
	targets := map[string]int{}
	for _, f := range files {
		ext := filepath.Ext(f)
		if !slices.Contains(libraryExts, ext) {
			continue
		}
		parts := strings.Split(strings.TrimSuffix(f, ext), ".")
		if len(parts) < 4 {
			continue
		}
		platform, target := parts[1], strings.TrimPrefix(parts[2], "template_")
		arch, threads := "", ""
		for _, p := range parts[3:] {
			switch p {
			case "nothreads":
				threads = ".nothreads"
			case "dev", "simulator":
			default:
				arch = p
			}
		}
		base := platform + "." + arch + ".single" + threads
		libs = append(libs, lib{base, target, f})
		targets[base]++
	}

	var lines []string
	for _, l := range libs {
		key := l.base
		if targets[l.base] > 1 {
			key += "." + l.target
		}
		lines = append(lines, fmt.Sprintf("%s = \"./dist/%s\"", key, l.file))
	}
	return lines
}
