package main

import (
	"strings"
	"testing"
)

func TestExtract(t *testing.T) {
	readme := "# Title\n\n```go\nfmt.Println(flashpi.Version) // os.Exit is only in a comment\nreturn errors.New(\"x\")\n```\n\n```bash\ngo get x\n```\n\n```go\npackage main\n\nfunc main() {}\n```\n"
	files, err := Extract(readme, "README.md", "es")
	if err != nil {
		t.Fatal(err)
	}
	fragment := string(files["block01.go"])
	for _, want := range []string{"package es", "\"errors\"", "\"fmt\"", "\"github.com/flashpi1/flashpi-go\"", "func Block01() error {", "\treturn nil\n}"} {
		if !strings.Contains(fragment, want) {
			t.Errorf("block01.go has no %q:\n%s", want, fragment)
		}
	}
	if strings.Contains(fragment, "\"os\"") {
		t.Error("a package named in a comment was imported")
	}
	if program := string(files["block02/main.go"]); !strings.HasSuffix(program, "package main\n\nfunc main() {}\n") {
		t.Errorf("block02/main.go = %q", program)
	}
	if len(files) != 2 {
		t.Errorf("%d files", len(files))
	}
}

func TestExtractRefusesBlocksGofmtWouldChange(t *testing.T) {
	for _, block := range []string{"x :=  1\n_ = x\n", "package main\nfunc main(){}\n", "if {\n"} {
		if _, err := Extract("```go\n"+block+"```\n", "README.md", "es"); err == nil {
			t.Errorf("%q was accepted", block)
		}
	}
}

func TestRunNeedsAMode(t *testing.T) {
	if err := run(nil); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("got %v", err)
	}
	if err := run([]string{"check"}); err == nil || !strings.Contains(err.Error(), "root") {
		t.Errorf("got %v", err)
	}
}
