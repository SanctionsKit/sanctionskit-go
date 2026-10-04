# SanctionsKit for Go

A server-side Go client for the [SanctionsKit API](https://www.sanctionskit.com/product/api).
It screens a subject against selected coverage, retrieves retained results and
evidence, and checks source availability. Requires Go 1.26 or newer. The client
uses only the Go standard library.

Install version 0.1.0:

```sh
go get github.com/SanctionsKit/sanctionskit-go@v0.1.0
```

## Start with synthetic records

Create a free [developer workspace](https://www.sanctionskit.com/signup?workflow=api)
and a sandbox key with `screenings:write` and `results:read`. The sandbox needs no
card and uses fictional records. Production screening requires a plan and payment
method. Keep API keys on the server.

```go
client, err := sanctionskit.New(os.Getenv("SANCTIONSKIT_API_KEY"), nil)
if err != nil {
    return err
}

// Save this key with the business operation before sending the request.
// A retry of that operation must keep the same key and body.
requestKey := os.Getenv("REQUEST_KEY")
result, err := client.CreateScreening(ctx, sanctionskit.ScreeningRequest{
    Subject: sanctionskit.Subject{
        Name: "Alex Morgan", EntityType: "person", BirthDate: "1984",
    },
    Package: "sandbox@1", Retention: "standard",
    Reference: "example-customer-001",
}, requestKey)
if err != nil {
    return err
}
evidence, err := client.GetEvidence(ctx, result.Data.ID)
if err != nil {
    return err
}
// evidence.RawJSON contains the exact downloaded JSON bytes.
// Store evidence only in your application's authorized storage.
_ = evidence
```

The complete runnable program is in [examples/screen](examples/screen/main.go).
From this checkout, set `SANCTIONSKIT_API_KEY` and a saved `REQUEST_KEY`, then run
`go run ./examples/screen`. This makes hosted sandbox requests using your key.
The tests described below use local fixtures instead.

`potential_match` calls for review. `no_match` means no candidates under the
selected coverage and matching rules. Neither approves a customer or provides
legal clearance. An error is an incomplete request, never a no-match result.
Preserve the result's coverage, versions and interpretation limits.

## Methods and response shapes

| Method | Scope | Response |
| --- | --- | --- |
| `CreateScreening(ctx, input, requestKey)` | `screenings:write` | `Response[ScreeningResult]`, decoded from `data` |
| `GetResult(ctx, id)` | `results:read` | `Response[RetainedResult]`, decoded from `data` |
| `GetEvidence(ctx, id)` | `results:read` | `EvidenceResponse`, the direct evidence document |
| `ListSources(ctx)` | `sources:read` | `Response[[]Source]`, decoded from `data` |

Each response includes `RequestID` and the original `RawJSON`. Raw JSON preserves
fields added by the service and publisher-specific records. Retained subject,
reference and request fields can be nil when inputs were not retained. The
client never reconstructs them. Evidence is not a signed attestation.

Supply exactly one coverage selector: `Package` or `Sources`. The synthetic
sandbox uses `sandbox@1`. For production, check source availability and supported
entity types with `ListSources`; a catalog entry alone does not establish usable
coverage. An approved policy is `&sanctionskit.PolicyReference{ID: id, Version: version}`.
Organization policies and retention requirements remain enforced by the server.
This client does not include policy management, batch orchestration or monitoring.

## Transport and errors

All methods accept `context.Context`. The default timeout is 30 seconds and the
default response limit is 16 MiB. `Options` can set a timeout up to 120 seconds,
a response limit up to 64 MiB, and a custom HTTP client. The client copies supplied
HTTP settings and always refuses redirects, including redirects on the same host.
Custom transports are under your control; do not add credential or body logging.

The default base is `https://www.sanctionskit.com/api/v1`. HTTPS is required for
other hosts; plain HTTP is accepted only for loopback development. Base URLs with
credentials, queries, fragments or traversal paths are rejected.

There are no application-level automatic retries. Go's standard HTTP transport
may recover certain connection failures. If your application retries a screening,
keep its original key and body. Do not generate a new key just because a response
was lost. Non-success HTTP responses return `*APIError`, with `StatusCode`, `Code`,
`RequestID` and `RetryAfter`. Arbitrary server messages and bodies are omitted from
errors. Use `errors.As` to inspect the type.

Validation, transport and malformed-success failures return `*ClientError`.
Context cancellation and deadlines remain recognizable with `errors.Is`.
An expired retained result is an API error, commonly HTTP 410. A redirected,
oversized or malformed response is never returned as successful evidence.

## Local checks

```sh
go test ./...
go test -race ./...
go vet ./...
```

The transport tests use `httptest` on loopback with an invented key. Public OpenAPI
examples provide synthetic screening and evidence fixtures. These tests do not
use a hosted account, live sanctions records, real credentials or customer data.
Local test success is separate from a hosted sandbox run and package publication.

[API quickstart](https://www.sanctionskit.com/docs/quickstart) |
[Screening requests](https://www.sanctionskit.com/docs/screenings) |
[Evidence](https://www.sanctionskit.com/docs/evidence) |
[Idempotency](https://www.sanctionskit.com/docs/idempotency)

MIT licensed. Maintained by SanctionsKit. Contact support@sanctionskit.com.
