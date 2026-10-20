#!/usr/bin/env python3
"""BX3 sweep 5: remove the GOFABRIC_DUMP_SUBSCRIBE /tmp dump (G1-17). Idempotent.
Run from the repository root."""
import sys

p = "bridge/subscribe_dispatch.go"
s = open(p).read()
if "dumpSubscribeEnabled" not in s:
    print("dump: already removed")
    sys.exit(0)

# the strict-validation pre-flight was gated on the same env var
a = s.index("\t\t// chip-strict pre-flight: optional TLV strict validation")
b = s.index("\t\t// Diagnose hook: structured log line + optional disk dump")
s = s[:a] + s[b:]

c0 = s.index("\t\t// Diagnose hook: structured log line + optional disk dump")
c1 = s.index('\t\tb.logger.Debug("matter.tx.im.subscribe.chunk",')
s = s[:c0] + (
    "\t\t// Diagnose hook: one debug line per Subscribe-initial chunk. A byte\n"
    "\t\t// dump of the chunk belongs in a test, not in the library.\n"
) + s[c1:]

d = (
    "\t\tif dumpSubscribeEnabled() {\n"
    "\t\t\t_ = os.WriteFile(fmt.Sprintf(\"/tmp/subscribe-init-sub%d-chunk%d.bin\", subID, i), body, 0o600)\n"
    "\t\t}\n"
)
assert d in s
s = s.replace(d, "")

h0 = s.index("// dumpSubscribeEnabled reports")
h1 = s.index("\n}\n", h0) + 3
s = s[:h0] + s[h1:]
s = s.rstrip("\n") + "\n"
while "\n\n\n" in s:
    s = s.replace("\n\n\n", "\n\n")
for imp, use in (('\t"os"\n', "os."), ('\t"github.com/SukramJ/go-fabric/tlv"\n', "tlv.")):
    body = s.replace(imp, "")
    if use not in body and imp in s:
        s = body
open(p, "w").write(s)
print("dump: removed")
