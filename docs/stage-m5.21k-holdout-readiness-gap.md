# M5.21k：Formal Holdout Readiness Gap

日期：2026-08-11
状态：完成只读机械盘点；`ready=false`

## 目标

M5.21j 之后不继续扩大 LLM pilot，而是先回答一个更基础的问题：当前仓库是否已经具备公平、非公开、
多根因的方法评测条件。本阶段不创建私有样本，不运行目标系统，也不把历史公开工件重新标成 holdout。

## 输入、处理、输出

```text
输入
  7 份公开 evaluator manifest
  + 当前 BundleBenchmark / defect-eval 行为
  + HashiCorp Raft strict QualificationBundle
                         |
                         v
处理
  当前 manifest: strict decode + Validate + digest
  归档 manifest: strict decode + pair/root-cause 计数 + file SHA
  + formal classification rejection
  + multi-pair CLI rejection
  + retired blind Go source absence
  + required qualification gap derivation
                         |
                         v
输出
  digest-bound readiness report
  + stable findings
  + ready=false
```

实现只增加一份 `cmd/defect-eval` 集成回归和一份小型 JSON 报告，没有新生产包、CLI、schema 文件、
执行器或长期 readiness 框架。回归会重新扫描 `benchmarks/pilots/*/evaluator/manifest.json`，新添或修改
manifest 都会使归档报告失配，必须重新审查。

## 机械结果

| 范围 | Pair | 不同 root-cause label | 正式 eligible |
|---|---:|---:|---:|
| 当前可验证公开 manifest | 3 | 1 | 0 |
| 旧格式公开归档 | 4 | 3 | 0 |
| 仓库公开合计 | 7 | 3 | 0 |
| 非公开正式集 | 0 | 0 | 0 |

公开集合的三个 label 是集合去重结果，不是因果独立性证明。三个当前 manifest 反复使用同一个
command-data calibration 根因；旧 ReadIndex、Ready.MustSync 和 calibration 工件已经公开且依赖退役格式，
所以都不能用来构成 blind holdout。

当前代码还存在四个明确边界：

1. `BundleBenchmark` 只接受 `public-calibration-only`；
2. `cmd/defect-eval` 的 fresh/legacy 入口均要求恰好一组 pair；
3. M4 的 preflight、blind audit、blind replay 已没有可编译 Go 源码；
4. fresh evaluator 的 composition 仍直接绑定 etcd/raft projector。

第二个 strict CFT 门也未通过。HashiCorp Raft qualification 的 required 能力为 8 项、validated 为 3 项；
缺少：

- `audited-entropy-replay`；
- `natural-temporal-progress`；
- `pure-enabled-check`；
- `strict-decision-replay`；
- `strict-yield-evidence`。

可选的 `formal-process-isolation` 不计入 required 缺口。

## 可信边界

报告范围被明确限定为 `current-repository`。`formal_private_pairs=0` 表示当前仓库和当前编译评测面没有
正式私有 pair，不表示用户或其他隔离环境不可能持有私有样本。报告不保存私有身份，也没有尝试从公开
工件猜测未来 holdout。

`ready=false` 由以下稳定 finding 共同解释：私有数据不存在于当前范围、formal classification 不受支持、
fresh evaluator 仅支持单 pair、blind surface 已退役、正式 root cause/control 数量不足，以及第二 strict
CFT target 未通过。公开样本数量再多也不能改变 formal eligible 分母。

## 已证明与未证明

已证明：

- 当前七份 evaluator manifest 的格式分层、文件身份与公开计数可复算；
- 当前 formal classification 和多 pair CLI 会被拒绝；
- HashiCorp Raft 的五项 required 缺口可从 QualificationBundle 机械派生；
- 当前正式方法评测尚不具备资格。

未证明：

- 仓库外不存在私有 candidate/control；
- 三个公开 label 在因果上彼此独立；
- Agent、adaptive、Random 或其他方法谁更优；
- PSS、RiskWitness 或 Coverage 能预测缺陷检出；
- 任一共识实现或 ConsensusAtlas 正确、完备或无缺陷。

本阶段新增模型调用为 0，新 SUT execution 为 0。

## 下一阶段

下一步实现最小 `FormalBenchmarkContract/v1`，范围冻结为：

1. 私有 manifest 与 Agent-facing opaque trial view 的数据分离；
2. 至少三组 pair 的通用多 pair evaluator 输入，不把 pair 数写死在 CLI flag；
3. target composition 通过已注册 projector/monitor 接口提供，不在通用 contract 中导入 etcd/raft；
4. exposure audit 只比较冻结私有原子与公开工件，不恢复旧 submission/runner 体系；
5. 没有真实私有样本时只做 synthetic contract fixture，不宣称 holdout ready。

完成该合约后，再分别处理私有样本 curating 和第二 strict CFT 接入；两者都未完成前不扩大 Agent 角色。
