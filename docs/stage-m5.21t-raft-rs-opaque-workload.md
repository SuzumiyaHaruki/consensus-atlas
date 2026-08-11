# M5.21t：raft-rs opaque workload 纵向切片

日期：2026-08-11

## 结论

M5.21t 已完成一次真实的 `输入 → 共识执行 → 结果`：在自然选出的 n1 Leader 上，Runtime 通过已有
`ActionInvoke` 提交一个版本化 opaque input；raft-rs worker 不解释 request ID，只把 proposal bytes
写入日志；随后仅通过既有 Ready effect 与 message action 达成提交；Go Adapter 在原始提交节点输出
唯一 `ClientResult(committed)`。关闭首个 worker 后，新进程严格重放完整轨迹并得到相同结果。

统一 Action/Runtime/PSS/Experiment 生产代码仍为 0 churn，也没有修改 raft-rs 算法源码。这证明最小
控制层不只能够选主，还能承载一个输入到结果的完整路径。

但它也给出明确的成本警告：生产 Binding 从 981 行增长到 1,218 行，单一 workload slice 增加 237
行，并超过最初 minimal-binding 的 1,200 行观察线 18 行。因此本阶段功能通过，但分类为
`growth-review-required`，不能据此声称新目标接入已经足够薄。

## 边界设计

输入有三层身份：

1. Runtime 只保存并认证 `PayloadEnvelope`，不解析 request/value；
2. Go Adapter 将 request ID、origin 和 value 编码成版本化 proposal entry；
3. Rust worker 只处理 opaque proposal bytes，提交时原样返回 entry data，仍不解释上层身份。

Adapter 只在 applied node 等于 proposal origin 时产生 ClientResult。因此同一 entry 在 follower 上应用
不会产生重复客户端结果。测试机械确认 primary 和 replay 中 request-1 都恰好有一个结果。

## 实际结果

- 三节点自然选主后向 n1 提交一个 `opaque-m5.21t` value；
- 总 Runtime decisions：28；
- Invoke：1；ClientResult：1；status：`committed`；
- result 绑定非零 index/term，并恢复原 opaque value；
- trace digest：`0dabc8204c8b174d59b25b8e1bc9ccc6ca0131c4ce7bf5bfc7a69c9dc4a56164`；
- primary worker 关闭后，fresh worker strict Replay 通过且仍只有一个结果。

## 代码成本

| 类别 | M5.21sR | M5.21t | 增量 |
|---|---:|---:|---:|
| target-native Rust worker | 315 | 376 | +61 |
| Rust/Go protocol + client | 182 | 198 | +16 |
| target normalization/application/bookkeeping | 484 | 644 | +160 |
| 生产 Binding 合计 | 981 | 1,218 | +237 |
| conformance test | 168 | 305 | +137 |

最大的增量不在共识调用本身，而在 input/proposal/result 的版本化身份、Invoke eligibility、commit
回传以及 ClientResult 去重边界。这些代码有实际功能，不能简单判为无用膨胀；但 237 行的边际成本也
说明“薄 Adapter”尚未得到令人满意的证明。

## 没有完成

- 没有通用 WorkloadRouter 或 Experiment admission；测试直接在识别出 Leader 后 OfferInvoke；
- 没有 durable crash/restart，worker 退出后不恢复本地存储；
- 没有 raft-rs Core PSS Mapping、RiskWitness、Oracle、Coverage 或 Agent；
- 内部 raft-rs RNG 仍未做 tape audit；单值 election range 只消除了本轨迹的结果差异；
- 没有第二 target Qualification，更没有跨 Raft 家族以外的普适性证据。

## 下一阶段停止线

下一步固定为 M5.21tA 边际接入成本审计，不写新 target capability：

1. 对齐 etcd/raft 与 raft-rs 的 Invoke/proposal/result 路径，区分协议必需、进程桥接和重复 boilerplate；
2. 只有在至少两个真实消费者能使用同一接口时，才允许提取共享 helper；
3. 不通过移动文件或改 LOC 口径降低数字；
4. 审计后再决定 durable restart 是否值得投入，或是否应优先引入非 Raft 第三目标验证抽象。
