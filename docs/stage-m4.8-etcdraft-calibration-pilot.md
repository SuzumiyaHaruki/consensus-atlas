# M4.8：etcd/raft 候选资格与公开 calibration pilot

> 日期：2026-08-06
> 阶段结论：资格与受控构建/评测管线完成；四个官方候选仍 deferred；无正式方法效果结论

## 已完成

通用 `internal/defectbench` 新增版本化 Candidate Catalog、typed requirements、可信
CapabilitySnapshot 和 QualificationReport。Candidate 不含人工填写的资格状态；只有
QualificationReport 保存机械生成的 `qualified/deferred`。六类集合严格分开：

- controllable inputs；
- observable events；
- Driver capabilities；
- trusted monitors；
- execution outcomes；
- Profile bounds。

`catalogs/etcdraft` 是具体 composition 层。它显式列出当前 Runtime/Driver 可控输入，
只允许 Profile obligation 提供 observable evidence 和范围，不从 obligation event kind
推导输入。通用判定只检查 `Candidate.Requirements ⊆ CapabilitySnapshot`；missing reason
按固定类型序和 ID 排序。commit/reference 仅为审计元数据，资格过程完全离线。

`internal/sutbuild` 和 `cmd/sut-build` 实现受控构建：验证官方 module path/version、原始
source digest、恰好一次文本转换和输出 digest；复制模块到临时目录，只修改副本中的目标
文件，再用临时 `-modfile` local replacement 离线 readonly build。构建输入限制命令身份，
构建后复核 module cache digest；audit 记录 trial、source、binary、command、toolchain 和
由 digest 派生的 opaque SUT build identity。Driver 只增加可由 `ldflags` 设置的 identity
变量；两个 trial 使用相同 Driver 代码。

后续的通用性整理将 linker variable 升级为新 build input v4 的显式、可信 build-pack 字段，
避免 `internal/sutbuild` 知道任何 etcd/raft 包路径。此处保存的旧 build input 仍是不可改写的
公开审计证据；它们不被静默重写，也不能在缺少迁移后的 identity variable 时重新构建。

## 四个真实官方候选

候选目录中的四条记录都引用 etcd/raft 官方提交，而不是为本项目制造的样本：

| commit | 官方行为差异 | 当前机械缺口 | 状态 |
|---|---|---|---|
| `63903dd` | duplicate ReadIndex 请求/上下文 | ReadIndex input、read state、Driver capability、linearizable-read monitor | deferred |
| `76f1249` | log truncation 后 MsgApp 导致 panic | log compaction control、slow-follower inflight bound、process-exit classification | deferred |
| `8ecce32` | CommittedEntries pagination cursor | pagination input/capability/page observation/boundary、continuity monitor | deferred |
| `0675f3d` | Ready.MustSync | unchanged-HardState Ready、MustSync observation、conditional sync、MustSync monitor | deferred |

没有为了增加样本数扩展 Driver 或降低 Oracle。特别是 `0675f3d` 的差异仍被当前“每个
Ready 固定 sync”接入语义遮蔽，不能进入可信评测。

## 公开 calibration pilot

calibration 样本不是历史缺陷：它只在临时 raft 模块副本中使指定 follower 对一条普通
命令形成不同数据。Agreement monitor、Driver、Runtime、Profile 和计划没有任何样本
特判。control 与 calibration 使用相同的 1 run、96 decision、200 primary-work 上限。

实际结果：

| trial | Campaign Oracle | evaluator | decisions | primary/replay | Coverage |
|---|---|---|---:|---:|---:|
| correct control | PASS | control-pass | 41 | 127 / 127 | 21/55，38.67 |
| command divergence calibration | Agreement FAIL | killed | 41 | 127 / 127 | 21/55，38.67 |

evaluator 没有信任 Campaign 保存的结论，而是从真实 trace 重算 Agreement，在 step 118
得到同一日志 index 的两个不同命令值。账本结果为一个 calibration root cause killed、
control false-positive 0、invalid 0。相同 Coverage 与不同外部结果并列展示，再次确认
Coverage 不产生 kill credit。

完整工件位于 `benchmarks/pilots/etcdraft-calibration-v1/`。

## 本阶段没有证明

- 没有任何官方候选 qualified，也没有官方历史缺陷检出结果；
- calibration 只验证 build、execution、Replay、Oracle 和 evaluator 连接；
- 没有运行 Agent、Random/DFS 方法对比，不能声称 Agent 更优；
- 没有证明当前 55 项 Coverage 或 Raft PSS 对 holdout 缺陷有预测力；
- 没有证明 etcd/raft 或 ConsensusAtlas 正确、完备或无缺陷。

## 下一阶段最小可行工作

优先补 `ReadIndex` 输入和 read-state observation，再实现独立的 linearizable-read
monitor。它不要求引入 OS 进程监督，也不需要改变 Ready 持久化语义，是四个候选中
最小、最直接的能力闭环。完成后重新生成 CapabilitySnapshot；只有 `63903dd` 的全部
typed requirements 都被满足时才允许机械转为 qualified。若希望优先支持崩溃候选，
最小替代路径是先实现独立 SUT process outcome classifier，但仍需 log compaction 和
inflight bound，范围更大。
