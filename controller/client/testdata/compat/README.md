# Compatibility fixtures

The JSON examples of matterjs-server's `docs/websockets_api.md`
(<https://github.com/matter-js/matterjs-server>, Apache-2.0, commit
`a7922e85f4b94a5db94a1dbee1a02c685c2ec71d`, version 1.5.5) that carry
Matter values, copied unchanged from go-fabric-server's
`protocol/testdata/compat` (which extracts one file per fenced `json`
block, named `<command or event>.<kind>.json`).

`json_compat_test.go` round-trips every value in them through
`client.EncodeJSON` and `client.DecodeJSON`: attribute values in the
tag-based form, command fields and event data in the name-based form.
