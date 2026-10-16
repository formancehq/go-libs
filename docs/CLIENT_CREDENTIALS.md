# Context-aware client credentials

`transport/httpclient.NewClientCredentialsSource(ClientCredentialsConfig)` supplies
`BearerTokenSource` directly to `RetryConfig.Bearer`. Call `Token(ctx)` for a
cached token or one byte-bounded client-credentials exchange. It starts no background
refresh goroutine. The caller supplies a separate token HTTP client with its own
timeout, pacing and retry policy; it must not use this same bearer source.

## Ownership and concurrency

One caller owns each in-flight exchange. Waiters can cancel independently.
Healthy waiters elect a new owner when the previous owner's own context cancels;
a token-client timeout with a healthy owner context is shared with the waiters.
Cached tokens are returned immediately while valid beyond the configured skew.
`Invalidate(rejected)` clears only that exact cached token, so an older rejection
cannot discard a newer token. `GrantedScopes()` returns a copy of the last
successful grant's scopes, including after invalidation or a failed exchange.
The constructor copies the scope requirement slice and HTTP client value;
callers retain ownership of the supplied transport.

## Exchange contract

Form authentication is the default. `ClientCredentialsBasic` uses raw HTTP Basic
credential bytes through the existing OIDC authorization primitive. It does not
apply the extra URL escaping used by x/oauth2's client-credentials helper.
Scope and optional resource strings are sent exactly as supplied. No endpoint
is discovered and no authentication style is probed. All redirects are refused,
including same-origin redirects that might replay credential-bearing forms;
the supplied client's redirect policy is not mutated.

Success means a 2xx JSON response with a nonempty `access_token`. Unknown fields
are accepted. `expires_in` accepts an integer or quoted integer; absent, null
or nonpositive values use `DefaultTTL` (default five minutes). Positive values
that overflow a duration are refused. `ExpirySkew` defaults to thirty seconds;
nonpositive configuration values select these defaults. A lifetime at or below
the skew is refused before caching. `RequiredScopes` are matched exactly in the
granted scope list; optional token-type validation rejects nonempty types other
than Bearer, ignoring case. Endpoint selection and required scopes remain caller
policy.

`MaxBody` bounds successful response bytes, reading at most one sentinel byte
beyond the cap; nonpositive values default to 32 MiB. The body is always closed.
Non-2xx responses return `TokenEndpointError` with an integer status and do not
read or retain the response body. Request, transport and decode errors render
fixed diagnostic text while preserving their causes for `errors.Is`/`errors.As`.
Neither response values nor arbitrary transport errors are rendered. Callers
must separately configure safe HTTP diagnostics: metadata-only debugging does
not read token bodies, while optional payload observers own their admission.

A supplied `Job` describes one endpoint exchange; cache hits and waiters do not
start jobs. Descriptors must use fixed, bounded names. The source does not name
consumers, classify permanent credential failures, or select API replay policy.
The existing retry transport owns its one bearer recovery replay.

## Existing OAuth APIs

`authn/oidc/client.ClientCredentials` remains the one-shot relying-party helper,
backed by x/oauth2. x/oauth2's reusable token source binds a context when created
and has no per-call-context or compare-and-clear contract. Its exchange uses a
fixed response bound, different Basic encoding and expiry fallback semantics;
its retrieval error can render response bodies. The new source is for callers
requiring the contract above; it does not replace OIDC discovery, relying-party
configuration or refresh-token grants. Standard HTTP form encoding and existing
OIDC Basic authorization are reused; no second retry or rate-limit engine is
introduced.
