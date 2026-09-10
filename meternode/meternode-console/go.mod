module github.com/meterhome/meternode-console

go 1.23.0

toolchain go1.24.7

require (
	github.com/go-chi/chi/v5 v5.3.2
	github.com/jackc/pgx/v5 v5.7.5
	github.com/meterhome/meternode-proto v0.0.0
	github.com/pressly/goose/v3 v3.24.3
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/crypto v0.38.0 // indirect
	golang.org/x/sync v0.14.0 // indirect
	golang.org/x/text v0.25.0 // indirect
)

require (
	github.com/google/uuid v1.6.0
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
)

// The schema module is developed alongside this repo. When meternode-proto is
// split out to its own repository this replace goes away and the require line
// carries a real version.
replace github.com/meterhome/meternode-proto => ../meternode-proto
