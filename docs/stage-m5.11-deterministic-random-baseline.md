# M5.11：确定性 Random 非 Agent 基线

日期：2026-08-07

结论：现有 v2 实验执行器新增了一种协议无关、公开 seed、可重算的
`uniform-random-policy/v1`。它只在 Runtime 给出的 canonical enabled Action 集合中选择，不读取协议
字段或 PSS，不持有 Runtime entropy，也没有建立第二套 run/report/replay 路径。首个 etcd/raft Random
报告在与 M5.10 完全相同的逻辑预算下生成并严格重放。

## 1. 策略熵边界

每个 run 的 policy seed 由公开 base seed 和 run number 确定性派生。每一步选择输入只有：

```text
policy seed + decision number + canonical enabled Action-set digest
                              |
                              v
                    SHA-256 deterministic draw
                              |
                    rejection-sampled index
```

拒绝采样避免简单 `% len(enabled)` 的取模偏差。policy seed 保存在 `Policy` 和 config digest 中，但不会
传入 `controlruntime.Config`、Adapter `Reset` 或原生 entropy provider。测试机械检查：

- 相同 base seed 重跑得到相同 report 和每条 trace digest；
- 不同 base seed 得到不同 report digest；
- 不同 policy seed 下 Runtime config 与每条 trace 的 Runtime seed digest 保持相同；
- primary 与 replay 仍分别创建全新 Adapter/Runtime，replay decision 数与 primary 精确相等。

这里证明的是种子接线隔离，不是说不同 schedule 下的原生 entropy tape 必须相同；不同 Action 顺序可能
合法触发不同数量的协议原生随机调用。

## 2. etcd/raft v2 公开开发结果

Random 工件：[report.json](../benchmarks/experiments/etcdraft-v2-random-m5.11/report.json)

| Metric | M5.10 fixed | M5.11 Random seed 1 |
|---|---:|---:|
| Runs | 2 | 2 |
| Decisions per run | 32 | 32 |
| Primary setup / decisions / work | 2 / 64 / 66 | 2 / 64 / 66 |
| Replay setup / decisions / work | 2 / 64 / 66 | 2 / 64 / 66 |
| Per-run unique states | 23 / 23 | 29 / 26 |
| Union unique states | 45 | 53 |
| Prefix area | 1454 | 1843 |
| Strict replay | 2/2 | 2/2 |
| Model calls / tokens | 0 / 0 | 0 / 0 |

Random 的 `self_normalized_area=0.5433372641509434`。两份报告具有相同 Adapter Manifest digest
`ec3d50387c1f17486c2723c75c9ee961a87370ebfadaf3ec30bdcba68579ab3d` 和 Runtime seed digest
`3b5179d6b1beed42782367ed82ac313576efddf990413b2860f11423106b536e`。

Random 工件身份：

- config digest：`c88fa6c8d343934abd0a9c172d9c3accaf75c807449d851c89850836a32652fc`；
- report digest：`40999ceec51dc9a32a3afdc1838e9bf0572fbbf961ef9b1bc54e58f62a00f7a8`；
- 文件 SHA-256：`4a0ce9222a7cdbe2f0bc6d79ecc158f3029514795be84e7b76e447b3c59c3891`；
- 文件大小：176,367 字节。

M5.10 fixed 工件重新生成后仍为
`bf3c2cb50c8cfa37c8c6d9e90cc0a56575f18e3ad3ab9a3a9d07eec957091247`，说明新增 optional policy seed
没有改变历史 priority-policy JSON 或报告身份。

## 3. 这个比较能说明什么

在这一个公开 seed、这个三节点初始状态和 64-decision primary budget 下，Random 的 PSS union 比两个
固定策略多 8 个状态，prefix area 多 389。它证明 Random 策略确实走了不同且可重放的路径，也证明
现有报告足以进行共同逻辑预算下的发现曲线比较。

它不能说明 Random 一般优于固定策略、DFS、专家或 Agent：

- M5.10 的 progress/lifecycle 是行为见证，不是完整搜索方法；
- 当前只有一个公开 base seed，没有重复 seed 分布或置信区间；
- PSS state 数没有完备性分母，也不等于缺陷检出；
- Oracle/Coverage/holdout 均未进入 v2 报告；
- wall time、CPU 与 peak RSS 在两份报告中都明确为 `not-collected`。

## 4. 代码体积与耦合

- 通用 `model.go` 从 256 行增至 306 行：净增 50 行；
- etcd/raft composition command 从 102 行增至 136 行：净增 34 行；
- 本阶段生产 Go 净增 84 行，没有新增 package、Runtime backend、report schema 或 PSS 维度；
- 具体 etcd/raft 只存在于既有 CLI composition root；通用 Random 选择逻辑只读取 Control Action；
- Action、Runtime、Adapter、Sampler、Aggregate、Coverage、Oracle 和 Agent 零修改。

## 5. 下一步：M5.12

在调用真实模型前，冻结受限 Planner Proposal → `Policy` 编译边界，并用无模型 deterministic stub
贯通一次。Proposal 只能声明 priority/rules/public seed，必须经过当前 `Policy.Validate`、共同预算和
fresh replay；非法或运行期不可达 proposal 需要产生显式、计费的失败结果，不能让 Agent 绕过 work
ledger。这个边界通过后，下一阶段才接第一个 LLM Planner。
