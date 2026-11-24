module thingfabric.dev/go-fabric/wifi/wpasupplicant

go 1.27.4

require (
	github.com/godbus/dbus/v5 v5.2.4
	thingfabric.dev/go-fabric v0.26.0
)

require golang.org/x/sys v0.49.0 // indirect

// In-repo builds use the main module beside this one. A release of this
// module bumps the require above to the main module's tag it ships with.
replace thingfabric.dev/go-fabric => ../..
