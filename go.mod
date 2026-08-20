module github.com/SuzumiyaHaruki/consensus-atlas

go 1.24

require (
	github.com/hashicorp/raft v1.7.3
	go.etcd.io/raft/v3 v3.6.0
)

// Consensus implementations are checked out as pinned Git submodules.  The
// version remains in require for module identity while every local build uses
// the editable source tree below.
replace github.com/hashicorp/raft => ./suts/hashicorpraft

replace go.etcd.io/raft/v3 => ./suts/etcdraft

require (
	github.com/armon/go-metrics v0.4.1 // indirect
	github.com/fatih/color v1.13.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/hashicorp/go-hclog v1.6.2 // indirect
	github.com/hashicorp/go-immutable-radix v1.0.0 // indirect
	github.com/hashicorp/go-metrics v0.5.4 // indirect
	github.com/hashicorp/go-msgpack/v2 v2.1.2 // indirect
	github.com/hashicorp/golang-lru v0.5.0 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	golang.org/x/sys v0.13.0 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
)
