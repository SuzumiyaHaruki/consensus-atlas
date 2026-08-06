# M4.11.1 阶段总结：Ready.MustSync 可信边界加固

日期：2026-08-06
状态：完成

## 结论

M4.11 的 candidate/control 区分结果保持不变：语义重构 candidate 被 `ready-must-sync` kill，
官方 control pass，均可严格 replay。M4.11.1 修复的是评测结论周围的类型、证据与复验边界，
不扩展 Profile 分母、不增加 Agent 角色、不改变 Candidate 的 source transformation。

## 已完成

1. `ready-with-unchanged-hard-state` 从 Candidate 的 `controllable_inputs` 删除。它是由受控
   `read-index` 与 Ready 生命周期达到的语义前置状态，而不是一个可直接注入的 Runtime action。
2. `oracle.EvidenceValidator` 使 monitor 对 typed observation 先做可信验证；Ready.MustSync 与
   LinearizableRead 的 malformed evidence 会使 Campaign evidence 不 conform，并使 defect trial
   `invalid`，不会获得 SUT kill credit。
3. `FindingRef` 新增 `detection_granularity=plan-end`。当前 monitor 在 Campaign plan 完成后统一执行，
   `first_kill_primary_work` 因而表示首次可信检出时的完整 plan 成本，不是假装精确到 trace step 的成本。
4. 新增 `reproduce-fresh-clone.sh`。脚本要求 `go1.25.8/linux/amd64` 与预热的
   `go.etcd.io/raft/v3@v3.6.0` readonly module cache，在不覆盖冻结 JSON 的前提下重建 binary，
   并逐字节验证 qualification、build audit、Campaign 与 evaluator report。

## 实际复验

脚本在本机完整通过：4 个 candidates 中 1 个 qualified、3 个 deferred；candidate 为
105 primary/105 replay work 且 killed，control 为 100/100 且 pass；根因结果仍为 1/1 killed、
0 false positive、0 invalid。新的 benchmark digest 是
`c7364626445c6c69f955d2c33d56880abb5186917448f65fedb696e5b9748d5b`。

## 本阶段没有证明

- 没有使 M4.11 变成完整历史 checkout 复现或隐藏 holdout；
- 没有证明 monitor/Driver 对所有 etcd/raft 版本完整；
- 没有证明 Agent 优于 Random、DFS 或专家计划；
- 没有将 plan-end 检测粒度升级为 online trace-prefix 检测；
- 没有调用模型服务、读取 `key.txt`、修改 `/home/nitro/Desktop/raft`。

## 下一步

实现 `Blind Planner Agent v1` 的最小闭环。它只能消费冻结的 Profile、CapabilitySnapshot、
Coverage Debt、受限 Test Plan schema 和机械 finding；M4.9/M4.11 的公开样本只用于验证
“生成—校验—执行—反馈”路径，不作为 Agent 效果证据。
