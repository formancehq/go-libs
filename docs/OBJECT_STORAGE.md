# Bounded object access

`pkg/storage/object` supplies read-only access through `Bucket.ListPage`,
`Bucket.Stat` and `Bucket.Read`. `NewS3` takes an AWS SDK client and bucket name;
`NewAzure` takes an Azure Blob SDK client and container name. `OpenDirectory`
keeps an `os.Root` descriptor until `Close`. These constructors accept `Limits`;
zero fields select defaults of 256 MiB per object, one million listed objects,
and ten thousand pages for `ListAll`. Negative limits are invalid.

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

`ReadCurrent` reads exact metadata and that conditional generation, then checks
body size. It requires a revision and does not fall back to an unconditional
read. Directory revisions identify size and modification time; they do not
constitute a content digest. Applications needing detection of same-size,
same-time edits must retain an independent digest. Reads additionally compare
file identity and metadata before and after reading. Escaping keys, special
files and symlink escapes are rejected. Listings skip symlinks and bound both
visited entries and accumulated results. Directory cursors are positional, not
snapshots; mutations can change enumeration positions.

`ListAll` refuses repeated cursors and bounds even empty advancing pages. A
failed listing returns no partial inventory. `Page.ServerTime` is only a single,
uncached provider Date header; missing or cached dates remain unavailable.
Neither cloud listings nor local pagination assert snapshot consistency or
modification-time ordering. Consumers own checkpoint and discovery policy.
