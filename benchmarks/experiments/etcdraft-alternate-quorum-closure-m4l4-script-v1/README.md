# etcd/raft 正确干预后的执行闭合

本实验只运行确定性脚本，没有调用模型，也没有恢复 DFS。它从与 M4l3 相同的
三节点 etcd/raft 状态开始，执行：

```text
Invoke(a9d6-write-1)
→ Drop MsgAppResp n2→n1
→ 仅沿 n1↔n3 复制链闭合
→ 完成 n1/n3 必要 Ready persist/advance
→ Commit + Apply(a9d6-write-1)
→ fresh Replay + Oracle
```

## 结论

- `Invoke` 是 decision 28，目标 `MsgAppResp n2→n1` 在 decision 33 被唯一选中并丢弃。
- 公共固定顺序在原来的 11-decision 闭合预算内没有完成请求。它额外处理了 n2 的
  `raft-ready-advance`，并在第一次 n3 acknowledgement 后进入旧
  `MsgVote/MsgVoteResp` 链；这五个非因果 decision 把第二次 n3 复制循环挤出了预算。
- 这不是永久卡死：把公共预算扩到 32 后，它在 17 个 decision 后也能返回。
- 闭合 composition 从真实 Drop Action 和最新 Adapter evidence 推导出 leader=n1、
  dropped follower=n2、alternate follower=n3；节点身份不是测试直接传给选择器的。
- etcd 专属闭合选择器只允许 n1↔n3 的 `MsgApp/MsgAppResp` 和 n1/n3 的必要 Ready
  effect。它在 Drop 后使用精确的 12-decision 预算完成，同一 RequestID 在 decision 45
  以 `committed` 返回；`workload-invoked → MsgAppResp-dropped → decision-advanced`
  三个 Risk milestone 全部达到。
- 所有选择层级都只有一个候选；没有出现无法唯一选择的 frontier。若同一优先级
  出现多个候选，公共层会以 `closure-underdetermined` 正常停止并返回当前 frontier，
  而不是按 ActionID 猜测或把它记成 Runtime failure。
- qualified fresh Replay 稳定；`trace-integrity`、Agreement、etcd/raft client
  application binding 和 log progress 四个 monitor 均执行，Oracle violation 为 0。
- 该脚本使用普通 V2 qualified Bundle；没有伪造 Agentic MethodSpec digest。
- 公共层保留不可变权威 frontier，并只把深拷贝交给选择器；选择器无法通过原地修改
  Action kind 绕过闭合阶段的非干预边界。

完整的 12 个闭合 Action 及 decision 编号见 [summary.json](summary.json)。普通回归
测试从初始状态重新构造整个过程，不读取本目录作为 fixture：

```bash
go test ./cmd/control-experiment \
  -run '^TestEtcdraftAlternateQuorumClosureAfterDroppedAppendResponse$' \
  -count=1 -v
```

M4l5 又以零模型 planner 把同一 5-step 干预计划送入正式 Scenario Agent Episode
主路径。Target composition 在真实 Drop 记录后激活 closure factory，得到完全相同的
12 个自动闭合 decision、RequestID、Risk、fresh Replay 和四个 Oracle；该回归为：

```bash
go test ./cmd/control-experiment \
  -run '^TestEtcdraftAlternateQuorumClosureRunsThroughScenarioAgentEpisode$' \
  -count=1 -v
```

另一个零模型回归覆盖跨轮修订：第一轮 closure 歧义后，第二轮 `revise` 只执行
非干预 Action，公共层仍从已晋升的可信执行记录恢复原干预并继续专属闭合；预算
停止返回 `closure-budget-exhausted`，不会退回公共固定顺序。

## 能证明什么

这条垂直切片首次同时证明：语义 selector 能唯一选择干预；Target-local 后端能在
公共 Action 之上完成与 accepted hypothesis 一致的协议 Risk；最终 Trace 能由公共
Runtime fresh Replay，并由独立 Oracle 重算。公共层还会拒绝闭合器选择 Drop、Crash
等新干预 Action。

它没有发现 etcd/raft 缺陷，也没有证明该选择器适用于其他协议、其他 Raft 状态或
任意消息丢失。选择器是 Target composition，不是新的通用调度器。
