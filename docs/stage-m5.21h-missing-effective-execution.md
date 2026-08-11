# M5.21h：补齐唯一缺失的 Effective Execution

日期：2026-08-10

## 目标与边界

M5.21g 将两个 frozen PlannerViews 上的 Agent、zero-model 和 adaptive 六个方法视图折叠为三个
effective identities。其中只有 attempt-2 adaptive 的 `admissible-uniform` 没有可信执行证据。
M5.21h 只执行这一个 identity，不调用 LLM，不换 seed、不扩大预算，也不修改 Runtime、Adapter、
workload、PSS 或 Oracle。

独立 CLI 原先允许 B4 strategy 生成 report，却遗漏其 `-bundle-out` 白名单。阶段内把两个已经由唯一
qualified executor 支持的 B4 strategy 加入显式 allowlist，并用纯判定回归锁定范围；没有增加第二套
执行器或放宽 qualification。

## 实际执行

- target：official etcd/raft v2，manifest `ec3d5038...ab3d`；
- PlannerView：`7a130d51...76a5f`；
- effective execution：`089d9fbb...1b3d9`；
- Policy：`admissible-uniform-random-policy/v1`，seed 172；
- decisions：32；instance ceiling：34 primary / 34 replay work；
- actual work：33 primary / 33 replay；
- model calls/tokens：0 / 0；
- report/bundle：`bec73168...6ea41` / `937046e6...4b792`；
- Replay：稳定；Agreement 与 Trace Integrity：均检查、零 finding。

完整压缩证据和机械比较位于
[`benchmarks/experiments/etcdraft-v2-effective-execution-m5.21h/`](../benchmarks/experiments/etcdraft-v2-effective-execution-m5.21h/README.md)。

## 与归档 ActionClass 的诊断比较

两边使用相同 target、Runtime、workload、FaultEnvelope、seed、32 decisions 和 33/33 实际逻辑工作；
唯一有效输入差异是 Policy。

| 指标 | ActionClass | Adaptive/Uniform |
|---|---:|---:|
| 单次 PSS states | 17 | 23 |
| prefix area | 356 | 407 |
| self-normalized area | 0.6544 | 0.5530 |
| crash / drop / duplicate | 1 / 0 / 0 | 1 / 1 / 1 |
| workload completed / pending | 0 / 1 | 0 / 1 |

状态集合交集 8、并集 32，Jaccard 0.25。因而 M5.21g 的 behavior delta 已由真实 trace 证实，不是
identity 元数据差异。Uniform 的状态数更高但 self-normalized area 更低，也再次说明不能选择单个指标
作综合胜负。单 seed、单 attempt 且 monitor 均无 finding，不能支持 adaptive、uniform 或 Agent 的
总体效果结论。

## 证明、未证明与下一步

本阶段证明缺失 identity 可通过同一 qualified executor 执行、Replay 并形成完整 bundle；两种 Policy
在相同非 Policy 边界下产生不同状态集合。没有发现协议故障，没有完成 workload，没有 holdout，
也没有证明任何规划方法更优。

下一阶段只复用已有三个 bundle：共享 attempt-1 ActionClass，加上 attempt-2 ActionClass 或 Uniform，
机械生成两个 2-attempt/66-work method corpus 的聚合 PSS union/curve、workload 和 monitor 对照。
不调用模型、不运行新的 SUT；先判断行为差异在方法序列层面是否仍产生可解释的增量，再决定是否值得
冻结更大的 Agent 试验。
