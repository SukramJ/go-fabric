import sys
p = sys.argv[1]
s = open(p).read()
a = '\t{pkg: "github.com/SukramJ/go-fabric/client/peer", min: 93},\n'
assert s.count(a) == 1
fw = ''.join(
    '\t{pkg: "github.com/SukramJ/go-fabric/controller/%s", min: 0, why: "BX4 forwarding package (aliases and variables), no statements to cover"},\n' % n
    for n in ['discovery', 'exchange', 'handshake', 'interaction', 'peer'])
s = s.replace(a, a + fw)
b = '\t{pkg: "github.com/SukramJ/go-fabric/script/archcheck", min: 0, why: "doc and test only, no statements to cover"},\n'
assert s.count(b) == 1
s = s.replace(b, b + "\t// BX4's forward generator; main() only writes the files its test compares.\n"
              '\t{pkg: "github.com/SukramJ/go-fabric/script/bx4-forward", min: 74},\n')
open(p, 'w').write(s)
