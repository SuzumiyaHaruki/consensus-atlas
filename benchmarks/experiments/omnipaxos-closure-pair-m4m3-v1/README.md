# M4m3：OmniPaxos Scenario-only closure 短配对

本实验在固定 existing Risk 下只调用 Scenario Agent，比较 `public-fixed` 与
`target-local`。它验证真实模型能否使用 OmniPaxos leaf semantics，选择当前 enabled
的 operation-carrying replication Drop，并触发已经由 M4m2/M4m2R 验证的 handoff。
它不重新测试 Risk Agent，也不是正式方法优越性或缺陷发现实验。

## 固定项与顺序

- 基线提交：`d6d3392ea30871d268a551e44c6ebf0fb0cca65d`；
- fixed Risk：`message-loss-before-decision`，每个 Episode 针对当前 Target 重新资格审查；
- DeepSeek 官方 `deepseek-v4-flash`，temperature 0、thinking high；
- 相同协议知识、root、workload、Prompt v17、调用/token/decision/时间预算；
- 新 registry 检查 trace-integrity、agreement、omnipaxos-client-decision-binding；
- 两组镜像顺序：A/B、B/A，完整顺序见 `run-plan.json`；
- `accept-sync` 与 `accept-decide` 都是合法 operation-carrying 干预，不固定节点 ID。

唯一实验变量是由实际 Target composition 机械绑定进 MethodSpec 的 `closure_mode`。
所有已完成 Episode 都进入结果，不以成功运行替换方法失败；传输歧义单独报告。

## 运行命令

先构建只读 worker：

```bash
cargo build --locked --quiet --manifest-path adapters/omnipaxosv2/worker/Cargo.toml
```

四个 Episode 必须按以下顺序运行，且每个使用新的空目录：

```bash
go run ./cmd/control-experiment -strategy agentic-episode-v1 -target omnipaxos-v2 \
  -worker adapters/omnipaxosv2/worker/target/debug/consensus-atlas-omnipaxos-worker \
  -campaign-dir artifacts/agentic/omnipaxos-closure-pair-m4m3-v1/g01-public-fixed \
  -semantic-input plans/agent/omnipaxos-agentic-calibration-v1.json \
  -risk-input plans/agent/omnipaxos-message-loss-risk-v1.json \
  -agent-key-file /home/nitro/Desktop/ds.txt -agent-provider deepseek \
  -agent-model deepseek-v4-flash -capability-feedback structured-gaps \
  -closure-mode public-fixed

go run ./cmd/control-experiment -strategy agentic-episode-v1 -target omnipaxos-v2 \
  -worker adapters/omnipaxosv2/worker/target/debug/consensus-atlas-omnipaxos-worker \
  -campaign-dir artifacts/agentic/omnipaxos-closure-pair-m4m3-v1/g01-target-local \
  -semantic-input plans/agent/omnipaxos-agentic-calibration-v1.json \
  -risk-input plans/agent/omnipaxos-message-loss-risk-v1.json \
  -agent-key-file /home/nitro/Desktop/ds.txt -agent-provider deepseek \
  -agent-model deepseek-v4-flash -capability-feedback structured-gaps \
  -closure-mode target-local

go run ./cmd/control-experiment -strategy agentic-episode-v1 -target omnipaxos-v2 \
  -worker adapters/omnipaxosv2/worker/target/debug/consensus-atlas-omnipaxos-worker \
  -campaign-dir artifacts/agentic/omnipaxos-closure-pair-m4m3-v1/g02-target-local \
  -semantic-input plans/agent/omnipaxos-agentic-calibration-v1.json \
  -risk-input plans/agent/omnipaxos-message-loss-risk-v1.json \
  -agent-key-file /home/nitro/Desktop/ds.txt -agent-provider deepseek \
  -agent-model deepseek-v4-flash -capability-feedback structured-gaps \
  -closure-mode target-local

go run ./cmd/control-experiment -strategy agentic-episode-v1 -target omnipaxos-v2 \
  -worker adapters/omnipaxosv2/worker/target/debug/consensus-atlas-omnipaxos-worker \
  -campaign-dir artifacts/agentic/omnipaxos-closure-pair-m4m3-v1/g02-public-fixed \
  -semantic-input plans/agent/omnipaxos-agentic-calibration-v1.json \
  -risk-input plans/agent/omnipaxos-message-loss-risk-v1.json \
  -agent-key-file /home/nitro/Desktop/ds.txt -agent-provider deepseek \
  -agent-model deepseek-v4-flash -capability-feedback structured-gaps \
  -closure-mode public-fixed
```

中断后只能对同一个 Episode 目录、同一参数增加 `-campaign-resume`；不能换目录重发
已经落入 durable journal 的调用。

## 记录与判定

每个 Episode 从可信 summary/Bundle 提取：干预 leaf/source/target/item、是否精确匹配
enabled Action、handoff、handoff 后是否直接闭合、Risk、RequestID、recorded fresh Replay、
三个 Oracle、calls/tokens、Runtime/search/qualified work 和停止原因。完成四轮后再生成
`final-summary.json`；当前目录在外部调用前只包含预注册计划，不包含结果。
