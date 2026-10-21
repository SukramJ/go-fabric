import sys
root = sys.argv[1]

p = root + '/README.md'
s = open(p).read()
a = "Depend freely on `bdx/`, `binding/`, `bootid/`, `bridge/`, `cluster/…`,\n"
assert s.count(a) == 1
s = s.replace(a, "Depend freely on `bdx/`, `binding/`, `bootid/`, `bridge/`, `client/…`, `cluster/…`,\n")
open(p, 'w').write(s)

p = root + '/CLAUDE.md'
s = open(p).read()
a = "The request path, top to bottom:\n"
assert s.count(a) == 1
s = s.replace(a, "**`client/` is the client machinery, `controller/` the role.** The\n"
              "discovery browser, exchanges, the PASE/CASE initiator, the client\n"
              "interaction and the peer set (`client/discovery`, `client/exchange`,\n"
              "`client/handshake`, `client/interaction`, `client/peer`) serve both\n"
              "roles — `binding` and `node` use them as much as the commissioner does —\n"
              "so they sit below the roles (ADR 0026 principle 12, brief BX4); the old\n"
              "`controller/<name>` paths forward to them until v0.12.0.\n\n" + a)
open(p, 'w').write(s)
