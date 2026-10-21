import sys
p = sys.argv[1]
s = open(p).read()
old_start = s.index("## The controller-side stack\n")
old_end = s.index("The order of [`examples/reference-controller`]")
new = """## The controller-side stack

```
client/discovery         DNS-SD browse for commissionable and operational nodes
controller/ble           BLE scanner, the Central seam, the BTP commissioning channel (transport/btp)
client/exchange          unsecured and secure sessions, exchanges, MRP (initiator side; none over BTP)
client/handshake         PASE and CASE as initiator, CASE responder for reconnects
controller/ca            the fabric's root CA, NOC issuance
controller/attestation   DAC → PAI → PAA, Certification Declaration, revocation hook
controller/dcl           CSA Distributed Compliance Ledger client (opt-in)
controller/commissioner  the commissioning flow (ControllerCommissioningFlow), incl. the Wi-Fi / Thread network steps
client/interaction       Read, Write, Invoke, Subscribe as a client
client/peer              peer sets, address monitoring, session resumption
controller/client        a typed image of each remote node, the registry, sqlstore
```

The five `client/` packages are the client machinery, not the role: the
device side uses them too (`binding` sends to bound peers through them,
`node` builds its client stack from them), so they sit in the
model-and-protocol layer and `binding` and `node` import them downward
(ADR 0026 principles 1 and 12). They lived under `controller/` until
v0.10.0; the old import paths forward to them until v0.12.0.

"""
s = s[:old_start] + new + s[old_end:]
open(p, 'w').write(s)
