# M4.11 阶段总结：etcd/raft Ready.MustSync 语义重构闭环

日期：2026-08-06
状态：完成（M4.11.1 可信边界加固后冻结）

## 阶段结论

M4.11 完成了第二条独立实现的 monitor/evaluator 路径：围绕 etcd/raft 官方历史修复 `0675f3d`
的 `Ready.MustSync` 条件，构造一个公开、受控、digest-bound 的语义重构样本。candidate
不是完整历史 checkout，而是在当前本地 `go.etcd.io/raft/v3 v3.6.0` module tree 上进行一处
精确反向转换：

```text
rd.MustSync = MustSync(r.hardState(), rn.prevHardSt, len(rd.Entries))
        ->
rd.MustSync = MustSync(rd.HardState, rn.prevHardSt, len(rd.Entries))
```

control 是未修改的官方 v3.6.0 module tree。两者使用同一 Driver、Profile、Test Plan、
trusted monitor、受控构建规则和 evaluator。最终 evaluator 结果为 1/1 semantic root cause
killed、0 false positive、0 invalid。

这证明的是一条新的 Ready/持久化语义评测链可以端到端工作；它不是第二个与 M4.9 同等级的
精确历史回归复现，也不是 Agent 方法效果结论。

## 已落地能力

1. Driver 新增 opt-in `ReadySyncPolicy`。默认仍是保守 sync；只有 Profile digest 绑定
   `runtime_profile=etcdraft-ready-must-sync-v1` 时，Driver Manifest 才声明
   `conditional-ready-sync` 与 `ready-must-sync-observation`。
2. `ready-must-sync` observation 在 `Acknowledge` 边界输出，记录 native `Ready.MustSync`、
   entries 数量与 HardState 是否为空。默认 Driver 路径不产生该证据。
3. `families/raft.ReadyMustSync` monitor 独立于实际 host Sync 操作，只检查保存 trace 中的
   typed observation：`must_sync=true && entries=0 && hard_state_empty=true` 即为 violation。
4. monitor 注册表拆分为 trusted default、registered qualification/evaluator、以及基于
   Driver Manifest 的 campaign opt-in 集合。M4.9 默认路径不会因为新增 monitor 改变。
5. Coverage Profile v2 的 `runtime_profile` 进入 canonical digest；未知 runtime profile 会被
   binding 拒绝，避免通过环境变量暗换 Driver 语义。
6. Candidate qualification 只从 opt-in capability snapshot 导出
   `ready-must-sync` observation；实际可调用输入是 `read-index`。`unchanged HardState` 是必须由
   受控 trace 到达的语义前置条件，不能被伪装成静态 controllable input。
7. monitor 的 typed evidence 先经过可信 schema validation。缺字段、非法编码或相互矛盾的
   Driver evidence 使 trial `invalid`，不会被 `ready-must-sync` 或 `linearizable-read` 计作 SUT kill。
8. `first_kill_primary_work` 明确使用 `detection_granularity=plan-end`：当前 evaluator 在完整
   Campaign plan 后运行 monitor，故不将 trace 中较早的表现错误表述为在线检出时刻。
9. 提供 `reproduce-fresh-clone.sh`；在冻结 Go/toolchain 与 readonly module cache 条件下重建 binary，
   并逐字节验证 qualification、Campaign 与 evaluator 工件。

## 实际结果

| trial | 外部结果 | runs / decisions | primary / replay work | Coverage |
|---|---|---:|---:|---:|
| `trial-ready-must-sync-candidate-v1` | `killed` | 1 / 1 | 105 / 105 | 2/3，75.00 |
| `trial-ready-must-sync-control-v1` | `control-pass` | 1 / 1 | 100 / 100 | 2/3，75.00 |

candidate 的可信 finding 是 `ready-must-sync` 在 step 65 报告：
`Ready.MustSync is true with no Entries and an empty HardState`。保存的 campaign trace 中还
有 step 82、88、92、96 的同类表现；evaluator 只把它们归为同一个
`ready-must-sync-empty-hardstate` root cause。control 没有 finding。

两侧 Coverage 完全相同，故 Coverage 不参与 kill 判定。

## 资格与工件

`qualification-report.json` 中四个公开候选的机械结果为：

| candidate | 状态 |
|---|---|
| `etcdraft-0675f3d-ready-must-sync` | `qualified` |
| `etcdraft-63903dd-duplicate-read-index` | `deferred` |
| `etcdraft-76f1249-log-truncation-panic` | `deferred` |
| `etcdraft-8ecce32-committed-pagination` | `deferred` |

M4.11 工件目录：`benchmarks/pilots/etcdraft-ready-must-sync-v1/`。

关键身份：

| 字段 | digest |
|---|---|
| Profile | `b9881db6422d734eaeaad64beddc5b03d2280c98f7bc76459a1abeaf88d93de0` |
| Candidate Catalog | `74e3271412979747511f58b75e90f4698c14da71df15065080b5fc7ffd9efeff` |
| CapabilitySnapshot | `11f0072245ebc4a56152f723e7d5cc2d7d30c25bade16ce4183e37767562cbf2` |
| Benchmark | `c7364626445c6c69f955d2c33d56880abb5186917448f65fedb696e5b9748d5b` |
| candidate source | `591bd2db43799b87b6401db31bc400e12872e6a6c989953f6f429eaaaea73384` |
| control source | `64bf7a0ef14de2a4044c02d1ecabb4c2210946c147fb52e2080edb76f4155dbb` |

## 本阶段没有证明

- 没有证明 `0675f3d` 的完整历史版本已被逐字节复刻；
- 没有证明 Agent 优于 Random、DFS 或专家计划；
- 没有证明 Coverage/PSS 能预测 holdout 缺陷检出；
- 没有得到安全性或活性层面的 Raft 正确性结论；
- 没有调用模型服务、没有读取 `key.txt`、没有修改 `/home/nitro/Desktop/raft`。

## 阅读顺序

1. `benchmarks/pilots/etcdraft-ready-must-sync-v1/summary.json` 与本文件；
2. `benchmarks/pilots/etcdraft-ready-must-sync-v1/qualification-report.json`；
3. `profiles/raft/ready-must-sync-v1.json`；
4. `plans/defectbench/etcdraft-0675f3d-v1.json`；
5. `benchmarks/pilots/etcdraft-ready-must-sync-v1/build-input/` 与 `build-audit/`；
6. `benchmarks/pilots/etcdraft-ready-must-sync-v1/campaign/`；
7. `benchmarks/pilots/etcdraft-ready-must-sync-v1/evaluator/report.json`。

## 下一阶段

M4.11.1 已完成完整验证与 M4.9 兼容性回归；新 opt-in Ready monitor 没有污染默认 Runtime 路径。
下一项应是 `Blind Planner Agent v1`：对 Agent 隐藏 defect identity、source transformation 和
trigger，只提供冻结 Profile、CapabilitySnapshot、Coverage Debt、受限 Test Plan schema 与机械
finding。M4.9/M4.11 只用于闭环调试，不能形成 Agent 效果结论；在至少三类隐藏 candidate/control
样本准备好前，不报告 Agent 相对 Random/DFS/专家的优势。
