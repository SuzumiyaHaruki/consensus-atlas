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
`final-summary.json`。

## 实际结果

四个 Episode 已按预注册顺序在提交 `bf58831d61b606926fbb59e42a347f2cd316ba70`
上完成。没有传输歧义或恢复调用；Risk provider 始终为 0。总计 10 次 Scenario 调用、
181,093 observed tokens。

| Episode | 模式 | 调用 / tokens | Scenario decisions | 实际 Drop | Handoff | Risk / Replay / Oracle |
|---|---|---:|---:|---|---|---|
| g01 | public-fixed | 2 / 37,066 | 5 | `prepare n1→n2` | 否 | reached / stable / 0 |
| g01 | target-local | 3 / 45,506 | 7 | `accept-sync n1→n2` | 是 | reached / stable / 0 |
| g02 | target-local | 3 / 47,290 | 7 | `accept-sync n1→n2` | 是 | reached / stable / 0 |
| g02 | public-fixed | 2 / 51,231 | 5 | `prepare n1→n3` | 否 | reached / stable / 0 |

target-local 两轮都在真实 enabled 的 operation-carrying `accept-sync` 上执行 Drop，随后
从同一 Drop step 发生可信 handoff，并且没有再调用模型或增加干预：

- g01：closure 投递 `prepare→promise→accept-sync→accepted`；
- g02：Agent 已完成 prepare/promise，closure 只需投递 `accept-sync→accepted`。

两轮都使同一 `omnipaxos-a9e1-request` completed。保存的 fresh Replay 均为 stable；
独立 saved-Bundle audit 校验 Bundle/projection，并重算 `trace-integrity`、`agreement`、
`omnipaxos-client-decision-binding`，四轮均为 0 violation。

## 解释边界

这个结果支持一个窄结论：真实 Scenario Agent 能利用 OmniPaxos leaf semantics 找到
operation-carrying 干预，Target-local closure 能从该点直接闭合，并形成同一请求、
Replay 和三个 Oracle 的证据链。这使相同接口的真实模型证据从 etcd/raft 扩展到了
第二个协议。

它不支持“target-local 已总体优于 public-fixed”。四轮机械 Risk 都 reached，且
target-local 的模型调用、tokens、Scenario decisions 和 qualified work 都略高。更重要
的是，两个 public-fixed Episode 丢弃的是 `prepare`，并不满足预注册的 operation-carrying
干预口径；之所以仍被当前 Risk 判为 reached，是因为该 Risk 的可执行谓词只要求
`message-dropped + operation-stage=inflight`，弱于自然语言 summary 中的
“operation-carrying”。因此结果必须同时报告：

- 机械 Risk reached：public-fixed 2/2，target-local 2/2；
- 预注册 operation-carrying 干预：public-fixed 0/2，target-local 2/2；
- target-local handoff 并直接闭合：2/2。

该差异是后续 Risk 表达改进的具体输入，不能在本实验结束后追溯修改判定规则。
完整数字见 `final-summary.json`；四个确定性 gzip Bundle 与四个 Oracle audit 是最小
可复核证据。没有保存或提交 provider journal，也不以 Agent 自报 verdict 产生 finding。
