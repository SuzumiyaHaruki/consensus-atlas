# M5.23f：受限 Search Agent 权限协议

日期：2026-08-12

## 结论

M5.23f 冻结了第一个直接进入 exact-prefix stateless traversal、但不能生成 Action 的 Search Agent 边界。
可信代码以 versioned `StatelessFrontierOrderRequest` 向 planner 提供：

- digest-bound `ProtocolKnowledgePack`；
- 当前 `ActionFrontierView` 及其完整 ActionRef；
- 只包含已经完成结果的可选 discovery history；
- 唯一可变字段声明 `action_ids`。

planner 响应只包含 request/view identity 和 ActionID 数组。可信 validator 要求它是当前 frontier ActionID
集合的完整 permutation，再映射回原 ActionRef；proposal 没有 node/message/payload/timer/PSS/Coverage/
Oracle 字段，因而不存在改写这些内容的接口。

## 执行与审计

`ExploreBoundedStatelessDFSWithAgent` 在每个实际展开的 frontier 调用 planner，保存 request/proposal/model
work record，并把 validated order 交回原 bounded DFS。离线 `Validate` 重算：

- method、knowledge、request、view 和 proposal identity；
- ActionID 集合完全相等且无重复；
- WorkItem 对每个 frontier 的实际顺序是已接受 proposal 的前缀；
- planner 调用、接受数量和 model work 总账一致。

Agent 不能选择 root、改变 depth/item/work ceiling、跳过 child Replay、写 PSS discovery 或产生 verdict。

## etcd/raft no-model 校准

在 M5.23c–e 使用的同一 28-decision root 上，确定性 reverse-frontier fixture 被调用 2 次并接受 2 个提案。
它生成的 6-item order 与 canonical 不同，搜索仍为 443 work；末尾 WorkItem 编译为 exact Policy，经原
qualified executor 执行并 strict Replay 稳定。重复运行逐字段一致。

内部 fixture 机械拒绝五类越权：遗漏 Action、额外 Action、重复 Action、虚构 Action、未知 JSON 字段。
模型调用为 0，所以该结果只证明权限链路，不证明真实 LLM 的语义判断。

## 已证明与未证明

已证明：Agent 风格的 planner 可以实际影响 stateless traversal 顺序；影响范围被限制为 trusted frontier
permutation；请求、提案、实际 WorkItem 顺序和成本可离线核对；旧 canonical/uniform 冻结实验未漂移。

未证明：真实 Agent 优于 baseline；Agent 提案能增加 PSS/Coverage 或缺陷检出；当前 knowledge pack 足够；
多 root 下实际成本可直接比较；任何共识实现正确。该阶段不是 Agent 效果实验。

验证通过：`go test ./...`、`go vet ./...`、M5.23c–f 冻结回归、通用 Agent order validator
race，以及真实 etcd/raft M5.23f 定向 race（99.108 秒）。M5.23e 已记录的大型 race 超时
未重跑。

## 下一阶段：M5.23g

运行一次真实、严格有界的 Agent multi-root pilot，并与 canonical 和预先固定 uniform seeds 使用同一个
M5.23e root corpus、相同 hard ceilings 和完整实际成本账本。每个 Agent 请求只能看到当前 frontier、冻结
knowledge 和此前已完成的 discovery history，不能看到当前 child 的未来结果。模型调用必须 opt-in、持久化、
逐次计费且失败不重试。评价报告同时给出 corpus-novel PSS 与实际工作量，但不以该 pilot 宣称普遍优势；
M5.23g 完成后关闭 M5.23，正式外部有效性进入 M5.24。
