module github.com/SukramJ/go-fabric/wifi/wpasupplicant

go 1.27.4

require (
	github.com/SukramJ/go-fabric v0.22.0
	github.com/godbus/dbus/v5 v5.2.4
)

require golang.org/x/sys v0.49.0 // indirect

// In-repo builds use the main module beside this one. A release of this
// module bumps the require above to the main module's tag it ships with.
replace github.com/SukramJ/go-fabric => ../..
