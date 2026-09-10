module github.com/meterhome/meternode-agent

go 1.23.0

require github.com/meterhome/meternode-proto v0.0.0

require (
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/shirou/gopsutil/v3 v3.24.5
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	golang.org/x/sys v0.20.0 // indirect
)

require (
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1
)

// The schema module is developed alongside this repo. When meternode-proto is
// split out to its own repository this replace goes away and the require line
// carries a real version.
replace github.com/meterhome/meternode-proto => ../meternode-proto
