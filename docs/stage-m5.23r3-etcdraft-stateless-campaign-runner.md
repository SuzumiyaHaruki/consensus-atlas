# M5.23R3：etcd/raft Stateless Campaign runner

日期：2026-08-12

状态：完成

## 本阶段解决的问题

M5.23R2 已冻结 protocol-neutral `StatelessCampaignSpec/v1`，但真实 canonical/uniform 仍只能由阶段测试
逐方法执行，不能通过同一个可恢复 Campaign 入口运行。本阶段把冻结的 etcd/raft root corpus、Stateless
Search、qualified execution、strict Replay、read-only discovery 和 Campaign store 串成 target-local runner。

没有调用模型、读取 key、构造 candidate/control，也没有改变 Adapter、Runtime、PSS mapper 或 Oracle。

## 输入、处理和输出

输入：

- 显式指定的 M5.23e `StatelessRootCorpus` 文件；
- `canonical` 重复次数，或 seeded-uniform 的 attempt 数与首个 seed；
- Campaign 目录、wall-clock 运维上限以及 summary/observation 输出路径。

处理：

1. 重新生成并严格验证 64-decision source bundle，核对 corpus/source/Manifest digest；
2. 在执行前冻结完整 `StatelessTraversalMethod` 序列；
3. 每个 attempt 在 0/28/54 三个 root 上执行 depth=2、每 root 6 items 的 exact-prefix Search；
4. 18 个 WorkItem 全部返回原 etcd/raft qualified executor，分别执行 primary 与 fresh Replay；
5. target composition 用 `CorePSSMapper` 重算并强验证 discovery，再提交 sealed attempt artifact；
6. Campaign 在每个 terminal attempt 后提交内容寻址 artifact/checkpoint；恢复时按原 request 重新严格解码；
7. 从已验证 artifact 机械生成 denominator-free observation。

输出：

- 通用 `CampaignSummary/v2`；
- `StatelessCampaignObservation/v1`，列出每 attempt 的方法 identity、PSS 集合、18 次 qualified execution、
  search/primary/replay/model 成本和跨 attempt corpus-novel PSS 并集；
- Campaign 目录中的 config、checkpoint 与内容寻址 attempt artifact。

Observation 不包含 Coverage 百分比、完备度或 correctness verdict；strict reader 会拒绝额外的
`coverage_percent` 等未知字段。

## 运行入口

```bash
make experiment-etcdraft-v2-stateless-canonical-campaign
make experiment-etcdraft-v2-stateless-uniform-campaign
```

CLI strategy 分别为：

- `campaign-etcdraft-stateless-canonical-v1`；
- `campaign-etcdraft-stateless-uniform-v1`。

corpus 必须通过 `-stateless-corpus` 显式提供。canonical 不接受非默认 seed；两条路径都拒绝 Agent key、
model budget、bundle/method 输出等无关参数。

## 真实验证结果

- canonical 的进程级恢复测试：先创建 sequence=0 Campaign，再由 CLI `-campaign-resume` 续跑，
  3 roots × 6 items = 18 次 qualified execution 全部完成，artifact 可按原 request 严格重读；
- seeded-uniform seed `01`：同一 corpus、bounds、executor 和 artifact 路径完成 18 次 qualified execution；
- 两条真实集成测试分别约 26 秒；无模型调用；
- 预算从 source work、root 最大 decision、depth、items 和 search ceiling 机械推导，不使用阶段观测值
  反向设定实际成本；真实 WorkLedger 仍保存方法间的微小成本差异；
- Campaign observation tamper 负例拒绝未知字段和虚构 coverage 数字。

完整验证：`go test ./...` 通过（`cmd/control-experiment` 274.654 秒），`go vet ./...` 通过，通用
Stateless Campaign 定向 race 1.171 秒通过。包含 uniform 真实 attempt 与恢复 CLI 的三项 target race
在 8 分钟停止线上超时；超时堆栈位于正常 exact-prefix trace JSON clone/replay，没有 race report。
依照既定停止线没有延长或重跑，因此该 race 结果记为未知，而不是通过或已发现并发问题。

M5.23e 的冻结校准仍给出 canonical 15、uniform seed 01 为 13 个 corpus-novel PSS；这些数只证明
runner 保持原发现语义，不是方法排名或测试完备度。

## 代码边界

- protocol-neutral durable projection：`internal/controlexperiment/stateless_campaign_observation.go`；
- etcd/raft 输入装配、预算推导、真实 attempt 与 CLI composition：
  `cmd/control-experiment/stateless_campaign_etcdraft.go`；
- 现有 Campaign Coordinator/store 没有复制；
- source/search/execution/model 继续由 R2 artifact 统一计费；失败 attempt 保存真实可获得的部分成本，
  不发明 discovery。

## 当前没有证明

- 没有证明 canonical、uniform 或 Agent 谁更好；
- 没有非公开 candidate/control 或缺陷检出；
- PSS 并集不是固定分母 coverage，也不预测 holdout 检出；
- 只有 etcd/raft target-local runner；OmniPaxos 尚未进入 Campaign；
- 旧 macro Campaign Planner 仍有两个 CLI 消费者，因此本阶段没有删除它。

## 下一阶段

M5.23R4 对旧 `campaign-etcdraft-v1` 和 `campaign-etcdraft-agent-v1` 做机械消费者迁移审计：

1. 无模型确定性用途迁移到本阶段的 canonical/uniform runner；
2. restricted Agent 只能替换同一 Stateless frontier order provider，必须复用相同 spec、artifact、恢复和
   observation；
3. 两个旧入口无现行消费者后，删除 planned-attempt/Campaign Planner/macro Intent 专用闭包；
4. 删除前必须保留 durable model call 的 ambiguous-dispatch、exact-byte request 和完整模型成本语义，
   不得为了瘦身降低 Agent 恢复可信度。

M5.24 非公开方法评价继续等待该迁移完成。
