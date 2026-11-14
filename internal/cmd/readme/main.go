// Command readme keeps the Go examples of the READMEs compiling. Run it from the root of the
// module:
//
//	go run ./internal/cmd/readme generate  write internal/readme/{es,en} from README.md and README.en.md
//	go run ./internal/cmd/readme check     fail when they are out of date (CI)
//
// Every go block of a README becomes a file that go build, go vet and staticcheck check. A block
// that starts with "package" is a whole program, written as it is to its own directory. Any
// other block is a fragment: it becomes the body of a function returning error, with the imports
// it uses, and takes the names that earlier blocks declare (client, ctx, invoice, ...) from the
// package's context.go. Every block must already be formatted as gofmt formats it.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const modulePath = "module github.com/flashpi1/flashpi-go\n"

// readmes maps each README to the package its examples go to.
var readmes = []struct{ file, dir, pkg string }{
	{"README.md", "internal/readme/es", "es"},
	{"README.en.md", "internal/readme/en", "en"},
}

// packages are the imports a fragment may use, by the name it uses them with.
var packages = map[string]string{
	"bytes":   "bytes",
	"cmp":     "cmp",
	"context": "context",
	"errors":  "errors",
	"flashpi": "github.com/flashpi1/flashpi-go",
	"fmt":     "fmt",
	"http":    "net/http",
	"io":      "io",
	"json":    "encoding/json",
	"log":     "log",
	"os":      "os",
	"strings": "strings",
	"time":    "time",
	"url":     "net/url",
}

var (
	qualified = regexp.MustCompile(`(?m)(?:^|[^.\w])([a-z]+)\.[A-Z]`)
	// A comment that is not inside a string such as "http://...".
	lineComment = regexp.MustCompile(`(?m)(?:^|\s)//.*$`)
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 || (args[0] != "generate" && args[0] != "check") {
		return errors.New("usage: go run ./internal/cmd/readme generate|check")
	}
	goMod, err := os.ReadFile("go.mod")
	if err != nil || !bytes.HasPrefix(goMod, []byte(modulePath)) {
		return errors.New("run it from the root of github.com/flashpi1/flashpi-go")
	}
	var stale []string
	for _, readme := range readmes {
		text, err := os.ReadFile(readme.file)
		if err != nil {
			return err
		}
		files, err := Extract(string(text), readme.file, readme.pkg)
		if err != nil {
			return err
		}
		current, err := existing(readme.dir)
		if err != nil {
			return err
		}
		for name, content := range files {
			path := filepath.Join(readme.dir, name)
			if args[0] == "check" {
				if !bytes.Equal(current[name], content) {
					stale = append(stale, path)
				}
				continue
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, content, 0o644); err != nil {
				return err
			}
		}
		for name := range current {
			if _, wanted := files[name]; wanted {
				continue
			}
			path := filepath.Join(readme.dir, name)
			if args[0] == "check" {
				stale = append(stale, path)
				continue
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			_ = os.Remove(filepath.Dir(path)) // a program's directory, once empty
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		return fmt.Errorf("the README examples are out of date (%s). Run: go run ./internal/cmd/readme generate", strings.Join(stale, ", "))
	}
	if args[0] == "check" {
		fmt.Println("internal/readme matches the READMEs")
	} else {
		fmt.Println("Wrote internal/readme")
	}
	return nil
}

// existing returns the generated files of a package directory (not its context.go).
func existing(dir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		name, _ := filepath.Rel(dir, path)
		if entry.IsDir() || !strings.HasPrefix(name, "block") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(name)] = content
		return nil
	})
	return files, err
}

// Extract returns the files of the go blocks of a README, by name.
func Extract(readme, source, pkg string) (map[string][]byte, error) {
	files := map[string][]byte{}
	header := fmt.Sprintf("// Extracted from %s by internal/cmd/readme: edit the README, then run\n// go run ./internal/cmd/readme generate.\n\n", source)
	for index, block := range blocks(readme) {
		number := fmt.Sprintf("%02d", index+1)
		if strings.HasPrefix(block, "package ") {
			formatted, err := format.Source([]byte(block))
			if err != nil {
				return nil, fmt.Errorf("%s, go block %d: %w", source, index+1, err)
			}
			if string(formatted) != block {
				return nil, fmt.Errorf("%s, go block %d is not formatted as gofmt formats it", source, index+1)
			}
			files["block"+number+"/main.go"] = []byte(header + block)
			continue
		}
		var imports []string
		for _, match := range qualified.FindAllStringSubmatch(lineComment.ReplaceAllString(block, ""), -1) {
			if path, ok := packages[match[1]]; ok && !slices.Contains(imports, path) {
				imports = append(imports, path)
			}
		}
		sort.Strings(imports)
		var out strings.Builder
		out.WriteString(header)
		fmt.Fprintf(&out, "package %s\n\n", pkg)
		if len(imports) > 0 {
			out.WriteString("import (\n")
			for _, path := range imports {
				fmt.Fprintf(&out, "\t%q\n", path)
			}
			out.WriteString(")\n\n")
		}
		fmt.Fprintf(&out, "// Block%s is go block %s of %s.\nfunc Block%s() error {\n", number, number, source, number)
		for _, line := range strings.Split(strings.TrimSuffix(block, "\n"), "\n") {
			if line == "" {
				out.WriteString("\n")
			} else {
				out.WriteString("\t" + line + "\n")
			}
		}
		out.WriteString("\treturn nil\n}\n")
		formatted, err := format.Source([]byte(out.String()))
		if err != nil {
			return nil, fmt.Errorf("%s, go block %d: %w", source, index+1, err)
		}
		if string(formatted) != out.String() {
			return nil, fmt.Errorf("%s, go block %d is not formatted as gofmt formats it", source, index+1)
		}
		files["block"+number+".go"] = formatted
	}
	return files, nil
}

// blocks returns the go code blocks of a Markdown text, each ending in a line break.
func blocks(text string) []string {
	var result []string
	var current *strings.Builder
	for _, line := range strings.Split(text, "\n") {
		switch {
		case current == nil && line == "```go":
			current = &strings.Builder{}
		case current != nil && line == "```":
			result = append(result, current.String())
			current = nil
		case current != nil:
			current.WriteString(line + "\n")
		}
	}
	return result
}
