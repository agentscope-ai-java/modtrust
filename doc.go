// Package modtrust checks whether a Go build environment actually enforces
// module integrity.
//
// Go's supply-chain protection rests on two settings most teams never
// verify are actually switched on: the module proxy (GOPROXY) and the
// checksum database (GOSUMDB). Either one can be silently disabled or
// narrowed — a proxy outage falling back to a direct VCS fetch, GOSUMDB=off,
// a GOPRIVATE pattern broad enough to cover public code, GOFLAGS quietly
// carrying -mod=mod — and nothing in a normal `go build` warns about it.
//
// modtrust reads the effective `go env` configuration together with its
// provenance (process environment, `go env -w` config file, or built-in
// default), flags the states that weaken or disable verification, checks
// go.sum for completeness against go.mod, and can shell out to `go mod
// verify` to check the module cache. See the package README for the full
// list of checks and their consequences.
package modtrust
