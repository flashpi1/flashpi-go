// Command openapi keeps the SDK's copy of the Flashpi API contract and the Go types generated
// from it in sync. Run it from the root of the module:
//
//	go run ./internal/cmd/openapi sync      download the public OpenAPI document, then generate
//	go run ./internal/cmd/openapi generate  regenerate types.gen.go from openapi/openapi.json
//	go run ./internal/cmd/openapi check     fail when types.gen.go is out of date (CI)
//
// The source is the public document the docs publish (https://flashpi.com.ec/docs/openapi.json),
// because the API repository is private. FLASHPI_OPENAPI_URL overrides it (HTTPS only), and
// FLASHPI_OPENAPI_FILE reads a local file instead (for example from a flashpi-api checkout).
//
// The generator uses the standard library only. Every component schema becomes a struct (keys
// stay as on the wire in the json tags), a string type with its constants, or, for Event, an
// interface its members implement; every query becomes a params struct. It fails on any
// construct it does not know, so a change in the document is never translated into a wrong type.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const (
	contractPath = "openapi/openapi.json"
	typesPath    = "types.gen.go"
	defaultURL   = "https://flashpi.com.ec/docs/openapi.json"
	modulePath   = "module github.com/flashpi1/flashpi-go\n"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	mode := "generate"
	if len(args) > 0 {
		mode = args[0]
	}
	if len(args) > 1 || (mode != "sync" && mode != "generate" && mode != "check") {
		return errors.New("usage: go run ./internal/cmd/openapi sync|generate|check")
	}
	goMod, err := os.ReadFile("go.mod")
	if err != nil || !bytes.HasPrefix(goMod, []byte(modulePath)) {
		return errors.New("run it from the root of github.com/flashpi1/flashpi-go")
	}
	if mode == "sync" {
		source, contract, err := download()
		if err != nil {
			return err
		}
		var document map[string]json.RawMessage
		if err := json.Unmarshal(contract, &document); err != nil || document["openapi"] == nil || document["paths"] == nil {
			return errors.New("the downloaded file is not an OpenAPI document")
		}
		if !bytes.HasSuffix(contract, []byte("\n")) {
			contract = append(contract, '\n')
		}
		if err := os.WriteFile(contractPath, contract, 0o644); err != nil {
			return err
		}
		fmt.Printf("Copied %s to %s\n", source, contractPath)
	}
	contract, err := os.ReadFile(contractPath)
	if err != nil {
		return err
	}
	types, err := Generate(contract)
	if err != nil {
		return err
	}
	if mode == "check" {
		current, err := os.ReadFile(typesPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if !bytes.Equal(current, types) {
			return fmt.Errorf("%s is out of date. Run: go run ./internal/cmd/openapi generate", typesPath)
		}
		fmt.Printf("%s matches %s\n", typesPath, contractPath)
		return nil
	}
	if err := os.WriteFile(typesPath, types, 0o644); err != nil {
		return err
	}
	fmt.Printf("Wrote %s\n", typesPath)
	return nil
}

// download returns where the contract came from and its text.
func download() (string, []byte, error) {
	if file := os.Getenv("FLASHPI_OPENAPI_FILE"); file != "" {
		data, err := os.ReadFile(filepath.Clean(file))
		return file, data, err
	}
	source := os.Getenv("FLASHPI_OPENAPI_URL")
	if source == "" {
		source = defaultURL
	}
	if !isHTTPS(source) {
		return "", nil, fmt.Errorf("refusing to download the contract over a non-HTTPS URL: %s", source)
	}
	client := &http.Client{
		Timeout: 60 * time.Second,
		// Redirects are followed to HTTPS URLs only.
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if request.URL.Scheme != "https" {
				return fmt.Errorf("refusing a redirect to a non-HTTPS URL: %s", request.URL)
			}
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
	request, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return "", nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("downloading %s: %s", source, response.Status)
	}
	if response.Request.URL.Scheme != "https" {
		return "", nil, fmt.Errorf("refusing a contract served from a non-HTTPS URL: %s", response.Request.URL)
	}
	data, err := io.ReadAll(response.Body)
	return source, data, err
}

func isHTTPS(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}
