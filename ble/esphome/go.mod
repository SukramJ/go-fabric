module github.com/SukramJ/go-fabric/ble/esphome

go 1.27.3

require (
	github.com/SukramJ/go-fabric v0.13.7
	github.com/flynn/noise v1.1.0
	github.com/miekg/dns v1.1.73
	golang.org/x/crypto v0.58.0
)

require (
	filippo.io/nistec v0.0.4 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.60.1 // indirect
)

// In-repo builds use the main module beside this one. A release of this
// module bumps the require above to the main module's tag it ships with.
replace github.com/SukramJ/go-fabric => ../..
