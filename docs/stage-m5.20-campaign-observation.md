# M5.20：跨 Attempt Campaign Observation

日期：2026-08-09

状态：设计冻结，待实现

## 目的

M5.19d 已能运行或恢复一个 Campaign，并输出可信的 terminal outcome、成本和内容寻址 artifact，
但用户仍必须逐个解码目标工件，才能回答“执行期间看到了多少 Core PSS 状态、使用了哪些环境扰动、
完成了多少 workload、可信 monitor 是否触发”。本阶段增加一个离线派生的
`CampaignObservation/v1`，把这些证据机械投影为可直接阅读的分栏结果。

```text
validated CampaignRecovery + committed artifact bytes
                         |
                         v
        target-owned artifact projector (etcd/raft first)
          decode + identity check + artifact.validate
                         |
                         v
            protocol-neutral attempt projections
                         |
                         v
              generic Campaign Observation
        +----------------+---------------+----------------+
        |                |               |                |
 terminal/cost      Core PSS union   fault/workload   monitor index
```

## 冻结边界

1. `CampaignSummary`、Coordinator、checkpoint store 和 Runtime 继续冻结；它们不解析目标 artifact，
   也不导入 etcd/raft 类型。
2. 目标投影器只能通过 `CampaignRecovery.ReadAttemptArtifact(ordinal)` 读取 committed artifact；每份
   artifact 都要重新执行严格 JSON 解码、request/config/spec/target identity 校验和目标已有的
   `artifact.validate`。不能读取 orphan、pending 或任意路径。
3. 通用聚合器只接受与 Summary attempt 一一对应的协议无关 projection。ordinal、artifact digest、
   outcome、WorkLedger 或 evidence identity 任一不一致都拒绝生成 Observation。
4. terminal 栏机械复制并复核 Summary 的 status、stop reason、四类 outcome 计数、累计 WorkLedger 和
   elapsed；`stopped` 不等于 pass，`failed` 也不伪造成零成本 attempt。
5. Core PSS 栏复用现有 `protocolstate.Aggregate`：每个有可信 bundle 的 attempt 是一个 run，初始样本
   不消耗 decision，随后每个 charged decision 对应一个样本。输出状态并集、witness、按累计 charged
   decisions 的发现曲线和无分母的自归一面积；它不是完整性百分比。
6. fault 栏只逐项求和 bundle 已验证的 `FaultUsage`。workload 栏只求和 planned/offered/completed/
   pending，并按实际 result status 计数；这些是执行统计，不是协议正确性 verdict。
7. monitor 栏保存“哪些可信 monitor 在哪份 committed artifact 上运行”以及触发索引。第一版
   etcd/raft composition 固定运行 trace-integrity 和 agreement；integrity 触发归为 evidence-invalid，
   其他触发归为 requirement-violation。零触发只表示这些 monitor 在已执行轨迹上未触发。
8. Observation 只保存唯一 PSS witness、逐 decision 曲线和小型 attempt 索引，不复制 Report、Bundle、
   完整 trace 或 artifact bytes。大型证据仍只保存在 Campaign 内容寻址目录。
9. 各栏保持独立，不把 PSS 状态数、fault 数量、workload 完成量或 monitor 结果合成为自定义综合分数；
   不引入义务分母，也不宣称完备性。
10. 本阶段不改变搜索策略、不接入 Agent、不读取 key、不调用模型，并且不增加第二套 executor。

## `CampaignObservation/v1` 最小内容

- Campaign/Summary identity：Campaign ID、config/target/spec digest、Summary digest；
- terminal：状态、停止原因、attempt/outcome 计数、总 WorkLedger、elapsed；
- attempts：ordinal、artifact digest、outcome、是否有 execution evidence、可选 bundle digest、
  charged decisions、该 attempt 的 PSS sample/state/new-state 数；
- Core PSS：PSS ID、evidence attempt 数、跨 attempt `protocolstate.ExperimentSummary`；
- faults：evidence attempt 数和四类累计使用量；
- workload：有 workload 的 attempt 数、planned/offered/completed/pending 合计、按实际 status 的稳定排序计数；
- monitors：checked 计数和按 attempt/monitor/step 稳定排序的触发索引；
- canonical digest。

没有可信 execution evidence 的 Campaign 允许生成 Observation，但 PSS 必须为空，fault/workload/monitor
计数必须为零；这种结果只能说明 terminal 执行情况，不能表现为“覆盖率为零的完整测试”。

## 最小验收

- [ ] 通用 Observation 可 canonical seal/revalidate，且不依赖 etcd/raft 包；
- [ ] projection 数量、顺序、artifact/outcome/work 绑定和 Campaign/Summary identity 篡改均被拒绝；
- [ ] 两个真实 committed attempts 的 Core PSS union/curve 可从 bundle 机械重算；
- [ ] fault/workload 合计和 monitor trigger index 可逐 attempt 追溯；
- [ ] failed/running、零 attempt 和无 evidence 边界不会被误写成 pass/fail 或覆盖率结论；
- [ ] runner 在 Summary 之外以全新文件持久化 Observation，失败时不覆盖旧文件；
- [ ] 普通全量、vet、受影响 race、依赖审计、规划镜像和 `git diff --check` 通过；
- [ ] 不读取 key、不调用模型、不提交大型实验 artifact。

## 明确不在本阶段完成

- 由 Agent 根据 Observation 选择下一 attempt；
- PSS 完整性分母、义务组合或单一“全面度”分数；
- 第二协议 projector 或跨协议合并 PSS；
- 新 monitor、缺陷根因归并或历史 candidate/control 评测；
- 声称 etcd/raft、ConsensusAtlas 或某种搜索方法正确、完备或更优。
