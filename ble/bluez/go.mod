module github.com/SukramJ/go-fabric/ble/bluez

go 1.27.2

require (
	github.com/SukramJ/go-fabric v0.6.0
	github.com/godbus/dbus/v5 v5.2.4
)

require (
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/miekg/dns v1.1.73 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
)

// In-repo builds use the main module beside this one. A release of this
// module bumps the require above to the main module's tag it ships with.
replace github.com/SukramJ/go-fabric => ../..
