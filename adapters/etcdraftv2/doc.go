// Package etcdraftv2 adapts the official etcd/raft RawNode API to the
// protocol-neutral Control Runtime v2 contract.
//
// The current slice supports configuration-driven static node sets, bootstrap
// Ready persistence/Advance, one Tick per PeriodicPulse, Runtime-owned Ready
// message delivery, and durable-image power-loss/restart. It is not yet a full
// etcd/raft Adapter qualification because external proposals, application
// durable data, snapshots, and process isolation are absent.
package etcdraftv2
