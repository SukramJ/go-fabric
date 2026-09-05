# go-fabric

`go-fabric` is a pure-Go implementation of the device side of the Matter
protocol, packaged as a library a host application embeds: TLV codec,
Interaction Model, PASE and CASE session establishment, MRP over IPv6 UDP,
DNS-SD advertisement, the cluster servers a bridge needs, and the endpoint
assembler that turns a host's own device list into a Matter topology. The
host keeps its device model; this module owns the wire format and hands back
the port contracts (`contract/`) a device implements to appear as a bridged
endpoint. It is a semantic port of [matter.js](https://github.com/matter-js/matter.js)
— cluster IDs, revisions, attribute IDs, constraints and wire shape are
mirrored from matter.js HEAD rather than transcribed from the specification,
and the extracted matter.js element model ships embedded so parity is a test,
not a claim.

## Not certified

This is an independent open-source implementation. It is **not certified by
the Connectivity Standards Alliance**, and it is not a project of the
Alliance. No warranty or assurance is made about rights that may be required
to implement it. Using it does not certify anything: it does not assure
compliance with the Matter specification, and it does not convey any right to
describe a device, product or service built with it as Matter compliant,
certified or similar. Certification requires membership in the Alliance and
compliance with Alliance policy; so does any use of Alliance trademarks and
logos. See [`licenses/NOTICE-matter.js.txt`](./licenses/NOTICE-matter.js.txt),
reproduced from matter.js, which states the same terms for the project this
one is ported from.

## Licence

MIT — see [`LICENSE`](./LICENSE). Upstream notices and the dependency
licences are recorded in
[`THIRD-PARTY-NOTICES.md`](./THIRD-PARTY-NOTICES.md).
