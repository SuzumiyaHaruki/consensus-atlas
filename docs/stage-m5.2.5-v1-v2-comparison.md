# M5.2.5 阶段总结：v1/v2 冻结场景对照与删除门

日期：2026-08-07

## 阶段结论

ConsensusAtlas 已完成第一轮真实 v1/v2 迁移对照，但尚未取得完整替换资格。

冻结报告结果为：

```text
passed=3  mismatched=0  deferred=1  qualified=false
suite digest=93e1c3353b86827e8a5560aa65c72f5b3fab798aeb8bf57aa8fb1767b46eaa0f
```

三个可比场景在固定外部期望下全部通过：相同命令 value digest、三个 applied node、零
Agreement/TraceIntegrity violation、各自 replay 稳定。自然换主被明确 deferred，因为 v1 官方
Driver 声明自然选举 timeout replay 不支持；用显式 `Campaign` 替代会改变输入语义。

因此本阶段没有删除 v1。依赖审计显示 PSS、Coverage、Agent、Campaign、自动接入和 Defect
Benchmark 的正式路径仍直接消费 v1 Trace/Engine/Driver。删除门保持关闭是可信结果，不是阶段失败。

## 输入、处理与输出

```text
冻结 scenario ID + command/witness expectation
                  |
      +-----------+-----------+
      |                       |
 legacy v1 execution      Control Runtime v2
 fresh run twice          recorded ActionID replay
      |                       |
      +-----------+-----------+
                  |
 external command digest / applied nodes / safety / replay / witnesses
                  |
                  v
      passed | mismatch | deferred + canonical digest
```

比较器不要求两边具有相同事件类型、term、log index、leader、决策数、trace schema 或配置 digest。
这些内部量不同不能伪装成外部不一致；反过来，仅“两边相等”也不够，每个可比场景还必须命中冻结
的第三方 expectation，防止两个路径以相同方式漏提交后仍通过。

## 冻结场景结果

| 场景 | 冻结期望 | v1 | v2 | 结果 |
|---|---|---|---|---|
| `normal-commit` | `alpha` 在 n1/n2/n3 应用 | fresh fingerprint stable | strict decision replay | passed |
| `message-drop-duplicate-commit` | 复制一条选举消息、丢弃另一条，命令仍在三节点应用 | witness + stable | witness + strict replay | passed |
| `committed-follower-recovery` | follower 恢复到 epoch/incarnation 2 并保留已提交 `alpha` | snapshot verified | application digest verified | passed |
| `natural-leader-change` | 自然时间导致换主 | unsupported | M5.2.4 已验证 | deferred |

所有 passed case 的左右两侧都记录：

- official `go.etcd.io/raft/v3@v3.6.0` implementation identity；
- 各自 configuration digest；
- 命令 ordinal、value SHA-256 和 applied node set；
- Agreement 与 TraceIntegrity violation count；
- replay mode 与稳定性；
- 冻结 witness set；
- summary、case 和 suite canonical digest。

## replay 边界

两个 `stable=true` 的强度并不相同：

- v1：从 fresh Driver 重新执行同一确定性 harness，比较完整 `ExecutionFingerprint`；
- v2：从 fresh Adapter 按记录的每个 ActionID 严格重放并逐决策比较。

报告保留 `replay.mode`，不会把 v1 自动升级为 strict replay。迁移比较只要求每条路径满足它已经
声明的 replay 契约。

## 新增实现

- `internal/migration`：协议无关 Summary/Expectation/Case/Suite、固定 reason code、digest 和篡改
  拒绝；
- `adapters/etcdraftv2/projection.go`：稳定 Evidence/ClientResult 投影，不暴露原生 Raft 类型；
- `migrations/etcdraftv1v2`：唯一允许同时组装 legacy v1 与 Control Runtime v2 的临时边界；
- `cmd/v1v2-compare` 与 `make v1v2-compare-etcdraft`：重新生成确定性冻结报告；
- checked-in migration report，并由测试 fresh rerun 后逐 digest 对照。

## 耦合审查

- `internal/migration` 不 import v1、v2 或 etcd/raft；
- `internal/control*`、`internal/conformance` 与 `adapters/etcdraftv2` 未 import legacy Runtime；
- v1/v2 同时出现只允许在 `migrations/etcdraftv1v2` composition root；
- etcd/raft Evidence 解码留在具体 Adapter，比较内核只接收规范化 Summary；
- 现有 architecture import gate 保持通过。

## v1 删除门

[v1 保留与删除清单](v1-retention-m5.2.5.md)基于 `go list` 直接 import 证据生成。本轮结论：

- 立即可删除项：无；
- legacy Driver/Host/Engine 仍为 runner、Campaign、Agent、autoonboard 和 evaluator 提供执行；
- Oracle/Coverage/PSS/Family/Agent/Benchmark 是应迁移的可信研究逻辑，不是应删除的“旧实现”；
- 历史 scenario/plan/profile/benchmark 工件必须继续可读；
- 临时 migration harness 只在最终删除完成后移除。

## 本阶段没有证明

- 没有证明 v1/v2 完全语义等价；
- 没有比较 ReadIndex、snapshot、成员变更或所有 Ready 切点；
- 没有把 v1 的 fresh rerun 变成 strict replay；
- 没有完成 PSS、Coverage、Oracle、Agent 或 benchmark 到 v2 的迁移；
- `qualified=false`，因此没有授权删除 legacy control plane；
- 单一 etcd/raft Adapter 仍不能支持跨协议普适性主张；
- 没有产生任何 Agent 方法效果结论。

## 下一阶段

进入 M5.3：把现有外部 Conformance 整理成 Adapter qualification 模板和机械报告，明确
capability 的 declared/validated/unsupported 集合及稳定 reason code。M5.3 不增加 Raft 专用行为，
目标是让第二个控制表面不同的 CFT Adapter 可以只实现其声明子集并获得可信准入结果。

第二异构 Adapter 通过后，优先迁移 v2 Oracle/PSS projection，再迁移 Campaign/Agent；每次迁移后
重新运行本删除门，直接消费者归零后才删除 v1 execution packages。

## 阅读顺序

1. [冻结迁移报告](../benchmarks/migrations/etcdraft-v1-v2-m5.2.5/report.json)
2. [报告说明](../benchmarks/migrations/etcdraft-v1-v2-m5.2.5/README.md)
3. `internal/migration/model.go`
4. `migrations/etcdraftv1v2/run.go`
5. `migrations/etcdraftv1v2/legacy.go`
6. `migrations/etcdraftv1v2/controlv2.go`
7. `adapters/etcdraftv2/projection.go`
8. [v1 保留与删除清单](v1-retention-m5.2.5.md)
9. [M5.2.4 proposal/application 总结](stage-m5.2.4-etcdraft-proposal-v2.md)

## 验证结果

- `make v1v2-compare-etcdraft` 可重复生成同一 suite digest；
- `gofmt` 已覆盖新增 `adapters/` 与 `migrations/`；
- `go test ./...` 通过；
- `go vet ./...` 通过；
- `go test -race ./...` 通过；
- Python 7 项单测通过；
- `git diff --check` 通过；
- 两份总体规划字节一致；
- 新增 JSON 报告可解码、逐层 digest 校验，并与 fresh rerun 一致。
