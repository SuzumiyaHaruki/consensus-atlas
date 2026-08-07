# HashiCorp Raft M5.4a control-surface probe

This public probe starts one official `github.com/hashicorp/raft v1.7.3` node
with a fixed three-voter configuration. The implementation's natural election
timer causes real outbound `RequestVote` calls through a custom Transport. The
probe freezes one call for each remote voter as existing Control Runtime v2
`ItemMessage` values and sorts them before sealing the report.

Reproduce from the repository root:

```bash
go run ./cmd/hashicorpraft-probe
```

The checked report is a control-surface observation, not a qualification or a
strict replay result. HashiCorp Raft v1.7.3 owns the wall-clock timer and
package-level random staggering; message release/delivery and `FSM.Apply` are
reserved for M5.4b.
