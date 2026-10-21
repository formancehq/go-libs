# Bounded object access

This extension-first prototype keeps the released
`pkg/storage/s3.NewAWSClient(config, optFns)` signature, SDK defaults and existing
flag API unchanged. The bounded S3 implementation lives in that existing domain;
native Azure Blob and rooted directories are separate packages.

| Package | Constructor / responsibility | Production SDK closure |
| --- | --- | --- |
| `storage/s3` | `NewBucket(client, bucket, limits)`, alongside unchanged `NewAWSClient` | AWS SDK; no Azure SDK |
| `storage/azureblob` | `NewBucket(client, container, limits)` for the native Azure SDK | Azure SDK; no AWS SDK |
| `storage/directory` | `NewBucket(root, limits)` reopens per operation; `Open(root, limits)` pins `os.Root` until `Close` | No cloud SDK |
| `storage/object` | SDK-free `Reader`/`Bucket`, requests/results, error identities, `ListAll` and `ReadCurrent` | No SDK |

The old unreleased `object.NewS3`, `object.NewAzure`, `object.NewDirectory` and
`object.OpenDirectory` entry points are removed, not retained as aliases or
forwarders. A source consumer of that proposal must update its imports and
constructors. No released constructor or consumer is changed by this prototype.

Constructors accept `object.Limits`; zero fields select defaults of 256 MiB per
object, one million listed objects and ten thousand pages. Negative limits are
invalid. `NormalizeLimits` is the shared validation/default algorithm. Each
backend exposes its normalized `Bounds()` through the common Bucket contract,
so aggregate callers can lower but cannot raise configured budgets. Directory's
`ListAll` capability preserves its one-traversal behavior rather than repeatedly
walking the root in remote-style pages. The generic algorithm recognizes that
whole-list capability without importing a backend, checks its aggregate output
bound and suppresses partial output on failure. The shared bounded-stream
and HTTP range/clock implementations remain internal to the storage domain;
there are no new per-backend forwarding interfaces or credential layers.

S3 listing requests URL-encoded keys and decodes them exactly once when the
response declares URL encoding. This preserves control characters, literal plus
and percent characters, and Unicode through subsequent reads. Continuation
tokens remain opaque and are never decoded.

Compose credentials and SDK endpoints outside the bucket. AWS
`iam.LoadConfig(ctx, Options, ...SDKOptions)` retains the SDK default credential
chain when no static credentials are supplied and rejects partial static pairs.
Existing flag APIs remain compatible. Azure clients can be composed with token
credentials, shared keys, SAS URLs or connection strings using the SDK. The
storage package never logs configuration or payloads.

`ReadRequest.MaxBytes` can lower the configured byte ceiling. `PrefixBytes`
requests a leading range; the result explicitly reports whether it contains the
whole object. Bodies are bounded while reading, closed on failure, and never
returned after an invalid range or failed revision condition. `Revision` is an
opaque ETag for cloud backends; no quoting or normalization is applied. Errors
retain the underlying SDK cause along with neutral size, permission, credential
and revision verdicts for consumer policy.

`ReadCurrent` reads exact metadata, applies its revision when present and checks
body size. Metadata revisions are optional for every backend; consumers requiring
a named generation must reject a missing validator. Directory revisions identify
size and modification time; they do not
constitute a content digest. Applications needing detection of same-size,
same-time edits must retain an independent digest. Reads additionally compare
file identity and metadata before and after reading. Escaping keys, special
files and symlink escapes are rejected. Listings skip symlinks and keys that
Stat/Read cannot accept, including backslash-containing names, before directory
recursion. Skipped names still count toward the visited-entry budget; listings
bound both visited entries and accumulated results. Directory cursors are
positional, not snapshots; mutations can change enumeration positions.

`ListAll` refuses repeated cursors and bounds even empty advancing pages. A
failed listing returns no partial inventory. Canceled calls do not dispatch
listing, and cancellation during a successful whole-list or paginated call
suppresses its inventory. `Page.ServerTime` is only a single,
uncached provider Date header; missing or cached dates remain unavailable.
Neither cloud listings nor local pagination assert snapshot consistency or
modification-time ordering. Consumers own checkpoint and discovery policy.

S3 marks definitive credential and permission error codes with the SDK-independent
`storage.ErrCredentials` and `storage.ErrPermission` sentinels. Refreshable token,
clock-skew and unknown errors remain unmarked. Bodiless S3 metadata failures and
Azure authorization errors retain the SDK error without adding terminal verdicts.
Applications retain their own retry and stop decisions.

## Scope and adoption decision

The common contract is for a caller that performs bounded inventory listing,
exact metadata lookup and a conditional read without choosing backend-specific
SDK operations. Native Azure revisions/continuations and local root identity
are not emulated through S3. Their common invariant is refusal of usable partial
results after byte, inventory or generation admission fails; lifecycle,
checkpoint, discovery ordering and provider policy remain outside this API.
One application's use of several backends does not demonstrate independent
service needs or establish shared maintenance ownership.

This is a local extension-first design candidate, not approval of the package
layout or a published replacement. The package-placement follow-up does not
further change the prototype's module manifests. Relative to the PR base, the
prototype adds Azure SDK dependencies and their checksums. Removing SDK imports
from a directory-only compiled closure does not remove cloud SDKs from the
selected module graph. Concrete application adoption, owner disposition,
provider-specific SDK compatibility and operational costs remain qualification
steps. Directory-only use still selects its Unix dependency where applicable;
no zero-dependency claim is made for every backend/platform.
