# Single-document JSON decoding

`pkg/types/jsonvalue` supplies three leaf functions:

- `Decode(data, &out)` retains `json.Number` in interface values and accepts
  additive struct fields.
- `DecodeStrict(data, &out)` also disallows unknown struct fields.
- `DecodeObject(data)` additionally requires a JSON object; `{}` is valid and
  null is not an object.

All three require exactly one JSON value followed only by whitespace. Callers
must bound data before decoding. Partial destination updates, duplicate keys,
null and typed-destination behavior follow `encoding/json`. Required fields,
number grammar, nonempty values and schema meaning belong to the caller.

## Existing APIs and scope

`query.ParseJSON` parses a query DSL and converts numbers into integers through
an unexported decoder. It is not a dynamic JSON document decoder.
`transport/api.Decode` is a test assertion helper using `require.TestingT` and
one default `json.Decoder.Decode` call. It neither retains `json.Number` nor
checks for a second value. `metadata.UnmarshalValue` uses ordinary unmarshal
and panics on errors. Changing these APIs would alter their current contracts.

The new leaf package isolates the missing document-decoding contract without
changing those specialized consumers. It adds no scalar conversions, required
field readers, array decoder or text-formatting policy.
