module github.com/SukramJ/go-fabric/examples/ble-wifi-light

go 1.27.4

require (
	github.com/SukramJ/go-fabric v0.22.1
	github.com/SukramJ/go-fabric/ble/bluez v0.22.1
	github.com/SukramJ/go-fabric/wifi/wpasupplicant v0.22.1
)

require (
	filippo.io/nistec v0.0.4 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/godbus/dbus/v5 v5.2.4 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/miekg/dns v1.1.73 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	modernc.org/libc v1.79.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.61.0 // indirect
)

// An example is built from a clone: every go-fabric module comes from the
// repository beside it. The example is not tagged.
replace (
	github.com/SukramJ/go-fabric => ../..
	github.com/SukramJ/go-fabric/ble/bluez => ../../ble/bluez
	github.com/SukramJ/go-fabric/wifi/wpasupplicant => ../../wifi/wpasupplicant
)
