# Decisions

Decisions about flashpi-go, the Go SDK, newest first. Its design follows the Python SDK,
[flashpi](https://github.com/flashpi1/flashpi-python), and the Node.js SDK,
[@flashpi/node](https://github.com/flashpi1/flashpi-node), whose
[decisions](https://github.com/flashpi1/flashpi-python/blob/main/docs/decisions.md) apply here
unless an entry says otherwise. Entries marked as the owner's were taken with the Flashpi
product owner; the others were chosen while porting, after the Python SDK where the owner had
not decided, and are listed so they can be confirmed or changed before 1.0.

## 2026-11-13: the Go SDK

### The owner's decisions

- **Module and repository.** `github.com/flashpi1/flashpi-go`, package `flashpi`, public
  repository `flashpi1/flashpi-go`, MIT license held by SIGNIADIGITAL S.A. Go 1.26 or later (the
  two supported releases, 1.26 and 1.27, tested in CI). The standard library only: no
  dependencies.
- **Models.** Structs generated from the OpenAPI document, with json tags; optional fields are
  pointers. The generator is a committed program (`internal/cmd/openapi`) with a check mode that
  CI runs.
- **Go idioms.** A `context.Context` is the first argument of every call; lists are walked with
  an `iter.Seq2` (`for doc, err := range client.Documents.List(ctx, params)`); errors are checked
  with `errors.As`, with a typed error per status like Python's classes.
- **Tooling.** go test (with `-race`), go vet, staticcheck and gofmt.
- **Publishing.** Git tags only: the Go module proxy serves a version from its tag. release-please
  creates the tags from 0.1.0. There is no registry and no publish job.
- **Everything else as the Python SDK.** READMEs in Spanish (`README.md`) and English
  (`README.en.md`); the resource style; an automatic Idempotency-Key reused by the retries; up to
  2 retries with backoff and jitter; Retry-After up to 60 s; waiting for a 409
  `idempotency_key_in_progress`; document recovery; a DELETE answered 404 after a lost answer
  counts as done; one deadline per attempt; the API key and header checks; English errors with
  language `es`; the `FLASHPI_API_KEY` fallback; Standard Webhooks verification; release-please
  from 0.1.0; the live API test on the release PR only; the flashpi-bot GitHub App's token for
  the bot's PRs; grouped Dependabot updates (gomod and github-actions).
- **The shape of the API (answered on 2026-11-13, all as recommended).**
  - **Options** are functional options. `WithTimeout`, `WithMaxRetries`, `WithLanguage` and
    `WithHeader` work on the client (`flashpi.NewClient(...)`) and on a call; `WithAPIKey`,
    `WithBaseURL` and `WithHTTPClient` only on the client, and `WithIdempotencyKey`,
    `WithCorrelationID` and `WithResponseInto` only on a call. The compiler refuses the others
    (`ClientOption`, `RequestOption`, and `Option` for both).
  - **Pages and the HTTP answer.** Every list also has `ListPage(ctx, ...)`, which returns a
    `*flashpi.Page[T]` (`Data`, `HasMore`, `NextCursor`, `NextPage(ctx)`), like Python's
    `next_page()`. The HTTP answer (status, headers, request id) of any call is read with the
    option `flashpi.WithResponseInto(&response)`.
  - **Events** are an interface narrowed with a type switch: `*flashpi.DocumentEvent`,
    `*flashpi.CertificateEvent` and `*flashpi.AccountEvent` (the `Event` union of the contract),
    `*flashpi.WebhookEndpointVerificationEvent` for the activation handshake, `*flashpi.ThinEvent`
    for endpoints in thin mode and `*flashpi.UnknownEvent` for a type this version does not know.
  - **null in a PATCH.** The optional members a request may send as null (`tradeName`,
    `description`, the tax profile's resolutions and RIMPE regime, `brandColor`) are
    `flashpi.Nullable[T]`: `flashpi.Null[string]()` sends null (clears the field),
    `flashpi.NullableOf("x")` sends the value, and the zero value is left out (`omitzero`). The
    other optional members are pointers.
  - **Dates.** `date-time` members are `time.Time`; dates without a time (`issueDate`,
    `2026-11-13`) stay strings, as in the requests.

### Chosen while porting (to confirm)

- **The client.** `flashpi.NewClient(options...) (*Client, error)`: a missing or malformed key and
  any refused setting fail there, before anything is sent, and the key is never in the error. The
  services are fields (`client.Invoices`, `client.Issuers.Establishments.EmissionPoints`,
  `client.WebhookEndpoints.Deliveries`, ...) of types named `XxxService`. `client.Environment()`
  is a method. Printing a client (`%v`, `%+v`, `%#v`, `String()`) shows the environment and the
  base URL, never the key, which is kept behind a pointer so printing the struct shows an address.
  There is no `Close`: Go's `http.Client` needs none. There is no separate async client:
  goroutines call the same one, which is safe for concurrent use.
- **Methods.** The operations are `Create`, `List`, `ListPage`, `Get`, `Update`, `Delete`, and
  `Documents.XML`, `Documents.PDF`, `Documents.SendEmail`, `WebhookEndpoints.GetSecret`,
  `RotateSecret` and `Verify`, `Deliveries.Replay`. Path ids come after the context, then the
  body or the list params, then the options. Bodies are values (`flashpi.CreateInvoice{...}`);
  the optional bodies are pointers: `IssuerOnboardings.Complete(ctx, id, nil)` sends `{}` and
  `WebhookEndpoints.RotateSecret(ctx, id, nil)` sends no body. `Documents.SendEmail` always sends
  `{}`. List params are pointers, `nil` for none, and their members are pointers too. Methods
  return pointers (`*flashpi.Document`), `[]byte` for the PDF, a `*flashpi.DocumentXML` (`XML`
  and `Kind`) for the XML and an `Event` for `Events.Get`; `Delete` returns only an error.
- **Lists.** `List` yields `*T` items (an `Event` for events) and, when a page fails, its error
  once, then stops; each `range` starts again from the first page. `NextPage` returns `nil` and
  no error after the last page, and `NextCursor` is `""` there. A page whose `hasMore` is true
  but has no cursor is the last.
- **`WithResponseInto`** is filled only when the call succeeds and its answer was read. On a
  list it holds the answer of the last page read; after a recovered document, the answer of the
  read of that document; after a DELETE that found nothing on a retry, a 204 with the 404's
  headers (without its Content-* headers).
- **Errors.** `*flashpi.Error` stands for Python's `FlashpiError`: every error the SDK returns is
  an `*Error` or can be read as one with `errors.As` (`*APIError`, `*ConnectionError` and
  `*WebhookVerificationError` implement `As` for it), so `IdempotencyKey` is always in reach. The
  status types (`*BadRequestError`, ..., `*InternalServerError`) wrap the `*APIError`, and
  `*TimeoutError` wraps a `*ConnectionError` (and has `Timeout() bool`). Python's
  `APIConnectionError` and `APITimeoutError` are `ConnectionError` and `TimeoutError`: Go has no
  builtins to shadow. `APIError.Errors` is never nil, `APIError.RetryAfter()` returns a
  `time.Duration` and whether the API asked, and `APIError.Header` holds the answer's headers.
  Every argument the SDK refuses (bad ids, options, headers, a body that cannot be JSON, a .p12
  that is missing, empty or over 1 MiB, a nil context) is an `*Error`: Go has no `TypeError` or
  `ValueError`.
- **Cancellation and deadlines.** The call's context bounds the whole call, retries and waits
  included. When it ends, the call stops at once, is not retried, and returns an `*Error` that
  wraps `context.Canceled` or `context.DeadlineExceeded` and carries the Idempotency-Key (Python's
  cancelled task raises `CancelledError` without one). The timeout of an attempt is a context of
  its own, which net/http honors while it resolves, connects, does TLS, writes and reads the
  headers and the whole body, also on a caller's `*http.Client`: unlike Python's sync client,
  nothing runs past it. A caller's `http.Client.Timeout` still applies and counts as a
  `TimeoutError`, which is retried.
- **Answers.** A 2xx answer of a JSON operation without a body, with a body that is not JSON, or
  whose members do not fit their types fails with an `*Error` (Python returns None or the dict as
  it came); unknown members are ignored. A Problem Details body whose members have unexpected
  types is still read: its code, title, detail, request id, document id and field errors are taken
  one by one. When the transport left an answer compressed (a caller that sets its own
  `Accept-Encoding`), gzip and deflate are decoded; any other encoding fails with an `*Error`. A
  3xx answer is an `*APIError`: redirects are never followed, also with a caller's
  `*http.Client`, which the SDK copies and gives a `CheckRedirect` that stops at the first answer.
- **Requests.** Header checks as in Python; text that is not UTF-8 is refused, Go's counterpart
  of the lone surrogates Python refuses. JSON bodies are written without HTML escaping, as
  `JSON.stringify` writes them. Query parameters are sent sorted by name (`url.Values`). The
  User-Agent is `flashpi-go/<version>`. In an issue with `options.waitForAuthorization`, the
  floor of the timeout is read from the serialized body.
- **Webhooks.** `flashpi.UnwrapWebhook` and `flashpi.VerifyWebhookSignature` are package
  functions (no `client.Webhooks`): they take the raw body as `[]byte` and the request's
  `http.Header`, whose names are matched in any case, also in a map that is not in canonical form.
  The tolerance and the clock are `flashpi.WithTolerance` and `flashpi.WithNow`; a negative
  tolerance is an `*Error`, never a skipped time check. A body without `data` (or with
  `"data": null`) is a `*ThinEvent`; a body whose members do not fit the event's types is
  refused as `invalid_payload`. Every webhook event has `Info()` (id, type, environment, time).
- **Generated types.** Go names follow Go's initialisms (`IssuerID`, `SRIMessages`, `RUC`, `RIDE`,
  `RIMPERegime`, `IssuingCA`, `SHA256Fingerprint`, `TaxTypeVAT`), and `EventThin` is `ThinEvent`.
  Twelve value sets that several schemas repeat get one type each (`Environment`,
  `DocumentStatus`, `DocumentType`, `EventType`, `EnabledEvent`, `IDType`, `PaymentMethod`,
  `PayloadMode`, `RIDEStatus`, `RIMPERegime`, `TaxType`, `WebhookDeliveryStatus`), matched by
  their exact values, so a change in one gives it a name of its own instead of folding it in;
  the other enums are named after their schema and member (`DocumentPartyIDType` would be one,
  `StatusStatus` is one). `integer` is `int`, `number` is `float64` (so `Document.Sequential` is
  a `float64`, as the contract says), free-form objects are `map[string]any`. Required members
  are values, so a required string left empty is sent as `""` and the API answers
  `validation_failed`. The list bodies (`DocumentList`, ...) are not generated: `Page[T]` stands
  for them. `ProblemDetails` keeps the members it does not declare in `Extra`. A schema used both
  by requests and answers would need a choice for its nullable members, so the generator refuses
  it, as it refuses any construct it does not know. The list params are named
  `<Resource>ListParams`.
- **README examples.** Every go block of both READMEs is extracted into `internal/readme` by
  `internal/cmd/readme` and compiled, vetted and checked by staticcheck there; CI fails when they
  are out of date or not gofmt-formatted. The counterpart of Python's `tests/examples/readme`.
- **Tooling.** staticcheck (2026.2.2) is pinned in a module of its own, `internal/tools`, built
  in CI and updated by Dependabot, so the SDK's `go.mod` never lists it. CI checks that the module
  has no dependencies, that `go mod tidy` changes nothing, the formatting, both generated sets,
  vet and staticcheck (also with the `live` tag), and the tests with the race detector, with a
  95 % coverage gate on the `flashpi` package, on Go 1.26; then the tests on Go 1.26 and 1.27.
  The live suite is `live_test.go`, behind the `live` build tag
  (`go test -tags live -run TestLive -v .`). `openapi/openapi.json` is part of the module's zip
  (about 440 KB): a Go module cannot leave a file of its tree out.
- **Releases.** release-please's `go` type writes only `CHANGELOG.md`, plus the `Version`
  constant of `version.go` through its `version-file` option; it needs no other version file.
  Tags are `vX.Y.Z` (no component), the form Go modules need, and a tag ruleset keeps them from
  moving or being deleted, since the module proxy keeps every version for ever.
