import sys
p = sys.argv[1]
s = open(p).read()
a = "- Docs: a comment on 104 mutexes names what each guards (`// mu guards …`, ADR 0026 principle 6).\n"
assert s.count(a) == 1
changed = (
    "- The client machinery moved out of `controller/` into a role-neutral `client/` tree (gate 1, G1-12, brief BX4; ADR 0026 principles 1 and 12): `binding` and `node` use it as much as the controller role does, and now import it downward. Same package names, same identifiers, files moved unchanged:\n"
    "  - `controller/discovery` → `github.com/SukramJ/go-fabric/client/discovery`\n"
    "  - `controller/exchange` → `github.com/SukramJ/go-fabric/client/exchange`\n"
    "  - `controller/handshake` → `github.com/SukramJ/go-fabric/client/handshake`\n"
    "  - `controller/interaction` → `github.com/SukramJ/go-fabric/client/interaction`\n"
    "  - `controller/peer` → `github.com/SukramJ/go-fabric/client/peer`\n"
    "  The old paths keep compiling (see Deprecated); the rest of `controller/` — `client`, `commissioner`, `ca`, `dcl`, `attestation`, `ble`, `thread` — is the role and stays.\n"
)
s = s.replace(a, a + changed)
b = "`wire.FanStepRequest` (the `fan.Stepper` port's) stay.\n"
assert s.count(b) == 1
dep = (
    "- `controller/discovery`, `controller/exchange`, `controller/handshake`, `controller/interaction`, `controller/peer`: forwarding packages over their `client/` successors (brief BX4) — a type alias per type, the same constants, and variables for the variables and functions (an error sentinel is the same value, `errors.Is` holds; methods come with the alias). Change the import path to `github.com/SukramJ/go-fabric/client/<name>`; the forwards are removable in `v0.12.0`.\n"
)
s = s.replace(b, b + dep)
open(p, 'w').write(s)
