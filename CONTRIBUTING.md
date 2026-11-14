# Contributing to flashpi-go

Read [docs/decisions.md](docs/decisions.md) first: it records why the SDK is built the way it
is. Decisions about the product (names, behavior, releases) belong to the Flashpi team: ask
before changing them.

## Setup

Go 1.26 or later. The module has no dependencies; staticcheck lives in a module of its own,
`internal/tools` (its `tools.go` makes it a direct requirement, so Dependabot updates it), so it
never becomes a requirement of the SDK.

## Checks

Run the same checks as CI before every commit:

```bash
gofmt -l .
go vet ./... && go vet -tags live ./...
go run ./internal/cmd/openapi check
go run ./internal/cmd/readme check
go build -C internal/tools -o /tmp/staticcheck honnef.co/go/tools/cmd/staticcheck && /tmp/staticcheck ./...
go test -race ./...
```

- `gofmt -l .` must print nothing.
- CI fails when the coverage of the `flashpi` package is under 95 %
  (`go test -race -coverprofile=coverage.out . && go tool cover -func=coverage.out`), and runs
  the tests on Go 1.26 and 1.27, the two supported releases. It also checks that the module has
  no dependencies (`go list -m all`) and that `go mod tidy` changes nothing.
- `operations_test.go` reads `openapi/openapi.json` and calls the method of every operation,
  checking its method, path, body and returned type: a new operation fails it until it has a
  method.
- `go test -tags live -run TestLive -v .` calls the real API in test mode. It needs
  `FLASHPI_TEST_API_KEY` (an `fp_test_` key, read_write) and `FLASHPI_DEMO_ISSUER_ID` (in CI, an
  organization secret and variable shared with this repository), and issues one test invoice on
  the demo issuer, which has a daily quota: run it only when needed.

Without Go on your machine, the official images work as well:

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.26 go test -race ./...
```

## Code conventions

- English for code, identifiers, comments and commit messages. The READMEs are in Spanish
  (`README.md`) and English (`README.en.md`); keep both in step. Every go block in them is
  compiled: after changing one, run `go run ./internal/cmd/readme generate`. A block that starts
  with `package` is a whole program; any other block is the body of a function that returns an
  error, and takes the names earlier blocks declare (`client`, `ctx`, `invoice`, ...) from
  `internal/readme/{es,en}/context.go`. Blocks must be formatted as gofmt formats them.
- Go 1.26 or later, the standard library only.
- Every method takes a `context.Context` first, then its path ids, then its body or its list
  params (a pointer, `nil` for none), then `...RequestOption`. Lists have `List` (an
  `iter.Seq2`) and `ListPage`. Methods mirror the API: `Create`, `List`, `ListPage`, `Get`,
  `Update`, `Delete`, then actions named after the operation (`SendEmail`, `RotateSecret`,
  `Replay`, ...).
- Every error the SDK returns is one of its error types, never a bare error of another package.

## The API contract

`openapi/openapi.json` is a copy of the public document at
https://flashpi.com.ec/docs/openapi.json and `types.gen.go` is generated from it by
`internal/cmd/openapi`. Never edit either by hand:

```bash
go run ./internal/cmd/openapi sync      # download the document and regenerate the types
go run ./internal/cmd/openapi generate  # regenerate from the committed copy
```

The `OpenAPI sync` workflow does this every day and opens a pull request when the contract
changes. New operations or fields may need a method or a hand-written type. The generator
refuses any construct it does not know, so a new kind of schema fails the sync instead of
becoming a wrong type.

## Commits and releases

Commit with [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`,
`docs:`, `test:`, `ci:`, `chore:`). They drive the version and the changelog:
[release-please](https://github.com/googleapis/release-please) keeps a release pull request
open, and merging it tags the version (`vX.Y.Z`) and creates the GitHub release. release-please
writes the version to `CHANGELOG.md`, `.release-please-manifest.json` and the `Version`
constant of `version.go` (sent in the User-Agent; its `x-release-please-version` marker points
release-please at it), and `version_test.go` checks the last two agree. The Go release type needs
no other file.

There is no publish step: the Go module proxy (proxy.golang.org) serves a version from its tag
the first time someone asks for it, and pkg.go.dev lists it after that. A published version
cannot change, so a tag is never moved or deleted (a ruleset protects `v*` tags).

While the version is 0.x, `feat:` raises the minor version and `fix:` the patch version.

The release PR and the OpenAPI sync PR are opened with a token of the organization's GitHub
App, so CI runs on them like on any pull request. (One opened with `GITHUB_TOKEN` would get its
CI only after someone approves it.) The Release workflow also starts the live API test
(`live.yml`) on the release branch each time release-please updates it. A dispatched `ci.yml`
run does not count for a pull request's required checks: to run CI again, re-run the PR's own
checks.

### The GitHub App

An app of the `flashpi1` organization, installed on `flashpi-go` (as on the other SDKs), with no
webhook and these repository permissions: Contents, Pull requests and Issues, read and write.
Its Client ID is the organization variable `FLASHPI_BOT_CLIENT_ID` and its private key the
organization secret `FLASHPI_BOT_PRIVATE_KEY`, shared with this repository. The workflows create
a one-hour token per run with only the permissions each job needs. To rotate the key, generate a
new one in the app's settings, update the organization secret (not a repository one, which would
shadow it here only), then delete the old key.
