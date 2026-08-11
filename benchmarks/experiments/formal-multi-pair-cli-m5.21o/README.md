# M5.21o formal multi-pair CLI

`summary.json` 由 `cmd/defect-eval` 集成回归机械复算。测试先用 official etcd/raft Adapter 生成一份
32-decision correct bundle，再通过注入 runner 将同一份 bundle 复用于 3 pair/6 trial，以验证 strict
private input manifest、全量 build/binary preflight、multi-pair evaluation 和 new-output gate。

三个 control 均通过，三个 synthetic candidate 均 survived；这不是实际 candidate binary provenance、
holdout 检出或方法效果。生产 CLI 的 subprocess runner 复用既有 public fresh execution 路径，本 fixture
没有启动六个新进程。真实 private pair 和第二 strict CFT 仍缺失，`formal_ready=false`。
