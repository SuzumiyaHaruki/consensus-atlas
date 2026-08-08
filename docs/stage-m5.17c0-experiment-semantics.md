# M5.17c0 Experiment 语义加固

日期：2026-08-08

## 结论

M5.17c0 已完成 Experiment 层的三项语义修正，同时保持 Control Runtime、Trace schema、Action identity
和 M5.15–M5.17b 冻结工件不变：

1. 所有 Policy 都只接收同一条 `Runtime enabled -> FaultEnvelope admissible` 前沿；
2. v2 运行显式区分 `budget-exhausted`、`quiescent` 与 `configured-stop`，未完成 workload 成为可重放
   的测量结果，而不是框架失败；
3. workload 目标解析从 Core PSS Mapper 移到 target-owned `WorkloadRouter`，PSS 不再兼任控制 API。

这一步没有增加搜索算法、Agent、Coverage 或第二套执行器。v1 report/bundle 的可选新字段全部省略，已有
冻结摘要由回归测试逐项保持。

## 共同 admissible frontier

每个 decision 现在固定经过：

```text
Runtime.EnabledActions
        |
        +--> runtime_enabled_digest
        v
FaultEnvelope + accumulated usage
        |
        +--> admissible_digest
        v
Policy.selectAction
        |
        +--> selected ActionID
        v
Runtime.Select
```

priority、action-class random、uniform random 和 trace mutation 不再各自拥有不同的 envelope 行为。
Runtime enabled 集不被修改；v2 `SelectionAudit` 分别记录两层 digest 和最终 ActionID，bundle 再把
Runtime digest/ActionID 与 Trace record 绑定。Fault usage 也从 Trace 重新计数，不能只相信报告字段。

## 终止与 workload 结果

Experiment v2 将 decision budget 解释为上限，而不是要求每次运行必须填满：

- `budget-exhausted`：实际决策数等于上限；
- `quiescent`：重放后的 admissible frontier 为空；
- `configured-stop`：配置的 workload 完成条件满足，且实际决策数小于上限。

合法目标终止必须通过 fresh Runtime replay 重算。Adapter/Runtime 错误、不可重放、资格不符、Evidence
损坏、路由异常和投影失败仍然是 execution failure/invalid，不会包装成 quiescent。

v2 workload 报告保存 `planned/offered/completed/pending/results/final_route`。`ExpectedStatus` 只保留为
输入声明，不再在 Oracle 前把实际 `rejected`、pending 或停滞轨迹删掉；v1 严格路径仍保持原先的状态
匹配和完整完成要求。

## WorkloadRouter 边界

通用 Experiment 只定义 selector、候选节点和 logical time，不理解 leader、term、view 或 quorum。
etcd/raft 的 `official-etcdraft-v2/workload-router-v1` 从 Adapter-owned public Evidence 中选择所有运行中的
`StateLeader`。零候选、多候选和候选已停止分别记录为 `no-candidate`、`ambiguous`、
`target-not-running`，都不是 Router 自行宣布的协议 verdict。

对于每个已经进入 Trace 的 `Invoke`，fresh replay 会再次在该 decision 之前的 Evidence 上调用 Router，
并要求其给出与冻结 Action target 相同的唯一候选。这样 Router identity 不是装饰字段，非确定或漂移的
目标解析不能获得 replay-stable 结果。

## etcd/raft 实际见证

两次运行都使用官方 `go.etcd.io/raft/v3 v3.6.0`、同一 Qualification、single-write workload、固定
Runtime seed 和 strict replay；完整 report/bundle 写到临时或 ignored `artifacts/`，不提交重复 Trace。

| decision 上限 | 实际决策 | 终止 | workload | Core PSS states | primary/replay work |
|---:|---:|---|---|---:|---:|
| 96 | 42 | `configured-stop` | 1/1 committed | 31 | 44 / 44 |
| 1 | 1 | `budget-exhausted` | 0 offered，1 pending，`no-candidate` | 2 | 2 / 2 |

稳定身份：

- 42-decision report `9411bb31...51245b0d`，bundle `ed44b5be...4155bf69`；
- 1-decision report `1570598d...2e0bff6`，bundle `28d271c1...30fc071`。

协议无关测试 Adapter 还实际产生了 0-decision `quiescent`：fresh replay 同样得到空 enabled frontier，
报告保留 1 个初始 Core PSS 状态。该 fixture 只验证终止语义，不是共识实验结果。

## 代码体积与兼容性

在 M5.17bR2 的 14,068 行 production / 6,232 行测试基础上，本阶段为 14,603 / 6,466，增加 535 / 234
行。新增代码集中于 Experiment v2 report、路由边界和验证见证，没有修改 Control Runtime 核心。

M5.15 report、M5.16 bundles、M5.17a action-class 和 M5.17b mutation 的冻结 digest 测试全部保持通过。
旧 v1 workload 继续要求全部 offered/completed 且实际 status 等于声明值。

## 当前没有证明

- 没有证明三种终止覆盖所有未来协议的目标停止条件；
- 没有证明 WorkloadRouter 或 Core PSS 对 etcd/raft 语义完备；
- 没有新增 candidate/defect verdict，42 决策完成也不是正确性结论；
- 没有 PSS-guided corpus、qualified uniform-random 方法实验或 Agent；
- 没有证明状态发现数能预测 holdout 缺陷检出；
- 没有第二个 strict deterministic target。

## 下一步

进入 M5.17c1，只补 corpus 的可信前提：

1. mutation source 从单一 fixed composition 泛化为显式、可计费的 source corpus；
2. 用 occurrence position 消歧重复 ActionID；
3. 让 PSS feedback 只能从绑定 Runtime/Evidence/Mapper identity 的 bundle 重算；
4. 建立最小 MethodLedger，统一记录 source、proposal、失败、primary/replay 和模型/资源成本。

完成这些前提后才实现 PSS-guided corpus；Guarded TestIntent Agent 仍在 M5.18b。

## 验证

- `make test`；
- `go vet ./...`；
- `go test -race ./...`；
- 两份总体规划字节一致；
- `git diff --check`。
