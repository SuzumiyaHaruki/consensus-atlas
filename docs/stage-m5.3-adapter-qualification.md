# M5.3 阶段总结：Adapter 机械准入与接入模板

日期：2026-08-07

## 阶段结论

M5.3 已把 Control Runtime v2 的外部 Conformance 从零散 Go 测试整理成版本化、可生成工件的
Adapter 机械准入门。

官方 etcd/raft v3.6.0 三节点 Adapter 对 `portable-cft-control-v1` 的结果为：

```text
required=8  validated=8  unsupported(optional)=1
undeclared=0  unvalidated=0  failed=0  qualified=true
bundle digest=e528b88d6f9295275ba47eca3b8edfe9dd1765e11a3426d7df178aacf3859723
```

这里的 `qualified=true` 只表示八项公共控制面能力通过冻结 Profile。它不表示协议正确、实现无
缺陷、v1 可以删除或系统已经具有跨实现普适性。进程级隔离没有被测试结果掩盖：optional
`formal-process-isolation` 机械标记为 `ADAPTER_PROCESS_ISOLATION_REQUIRED`。

## 输入、处理与输出

```text
sealed QualificationProfile
        + Adapter Manifest
        + enumerated Unsupported declarations
        + fresh trusted Conformance reports
                         |
             typed requirement comparison
             manifest digest/case/digest checks
                         |
                         v
 validated | unsupported | undeclared | unvalidated | failed
                         |
                         v
       required capabilities all validated?
                         |
                         v
           sealed QualificationReport + bundle
```

资格状态不来自 Adapter、Agent 或人工填写。Manifest 只形成候选声明；能力必须同时满足 Profile 中
的 typed manifest requirements 和固定 Conformance case。Unsupported 只能使用枚举 reason code。

## 新增公共实现

- `internal/conformance/qualification.go`：Profile、typed requirements、五态 capability 结果、稳定
  finding、Unsupported 枚举、canonical digest 和纯机械资格器；
- `internal/conformance/core.go`：不需要协议输入的 stable yield/evidence、pure enabled check 和
  audited entropy replay 外部 suite；
- `internal/conformance/profile.go`：第一个协议无关 `portable-cft-control-v1` 准入 Profile；
- `conformance.Report.Validate()`：拒绝 schema、pass 状态、case、排序和 digest 不一致工件；
- `adapters/TEMPLATE.md`：新实现目录、所有权、Manifest、外部 suite 和禁止修改项；
- Manifest、Qualification Profile 和 Qualification Report 三份 JSON Schema；
- `qualifications/etcdraftv2`：唯一的 etcd/raft M5.3 composition root；
- `cmd/adapter-qualify` 与 `make adapter-qualify-etcdraftv2-legacy`：fresh 执行并生成冻结 bundle。

通用资格器没有 Raft、Ready、term、日志或消息类型分支。Profile 的自然时间要求接受 one-shot、
periodic pulse 或 sleep wakeup 中任一种，避免把 etcd/raft 的 Tick 形状固化成公共接口。

## etcd/raft 外部见证

| Suite | 外部 case | 资格用途 |
|---|---|---|
| Core | `collect-idempotent` | stable yield/evidence |
| Core | `enabled-check-pure` | enabled 查询无可观察副作用 |
| Core | `entropy-audit-stable` | 分域 entropy tape 可审计且 fresh 稳定 |
| Natural lifecycle | `natural-crash-cancels-captured` | crash incarnation 与 replay |
| Natural lifecycle | `natural-released-message-recovery` | 自然时间、消息所有权与 replay |
| Opaque invoke | `opaque-invoke-boundary` | 不解析 payload 的输入与 replay |

一个 case 可以为多个高层准入能力提供必要见证，但每个能力在 Profile 中明确列出全部所需 case，
不能由 top-level capability 名称或描述猜测。

## 可信边界与限制

- CLI 必须在同一可信流程 fresh 执行 suite；JSON digest 只提供完整性和可重现绑定，不是签名；
- `formal-process-isolation` 仍 unsupported。etcd/raft v3.6 使用进程全局 `crypto/rand.Reader` seam，
  当前 mutex 只约束 Adapter 自己，无法约束进程内未知 goroutine；
- Profile 验证控制面，不验证 Agreement、linearizable read、日志提交或协议活性；这些仍属于
  Family Evidence/Oracle；
- 当前只有一个真实 Adapter 通过，fixture 不计独立实现；
- PSS、Coverage、Agent、Campaign 和 benchmark 仍在 v1 路径，删除门没有重新打开。

## 本阶段没有证明

- 没有证明第二个实现无需修改 Runtime；
- 没有证明 HashiCorp Raft 或任意其他实现已经适配；
- 没有证明跨 Raft 实现、跨协议家族或跨语言普适性；
- 没有产生新的缺陷检出或 Agent 方法效果结果；
- `qualified=true` 不是协议正确率或测试全面性百分比。

## 下一阶段

进入 M5.4，优先接入控制表面不同的 HashiCorp Raft：

1. 冻结依赖版本和三节点配置，只允许具体 Adapter import 该实现；
2. 将官方 `Transport` RPC 冻结为 Runtime-owned message；
3. 将 `FSM.Apply`、Log/Stable/Snapshot store 边界投影为 effect/evidence；
4. 将 `Apply` 投影为 opaque invoke；
5. 无可注入时钟的自然时间能力保持 unsupported，不得以 wall-clock sleep 冒充 strict replay；
6. 使用同一 `PortableCFTProfile` 生成资格报告。若需要公共语义改动，必须先记录失败见证并重跑
   etcd/raft 全部 Conformance。

M5.4 的第一结论只能是“Raft 家族内、不同控制表面的适配复用性”；之后仍需非 Raft 实现才能
讨论跨协议家族普适性。

## 阅读顺序

1. [冻结资格工件](../benchmarks/qualifications/etcdraft-v2-m5.3/report.json)
2. [工件说明](../benchmarks/qualifications/etcdraft-v2-m5.3/README.md)
3. `internal/conformance/profile.go`
4. `internal/conformance/qualification.go`
5. `internal/conformance/core.go`
6. `qualifications/etcdraftv2/run.go`
7. [Adapter 接入模板](../adapters/TEMPLATE.md)
8. [Control Runtime v2 设计](control-runtime-v2.md)

## 验证结果

- `go test ./...`：通过；
- `go vet ./...`：通过；
- `go test -race ./...`：通过；
- Python 7 项单测：通过；
- fresh qualification bundle 与 checked-in JSON 逐字节一致；
- Manifest、Qualification Profile、Qualification Report 三份 schema 语法和实例校验：通过；本机
  旧版 `jsonschema` 无本地 2020-12 metaschema，使用其 latest-draft 兼容校验并产生 deprecation
  warning；
- 两份总体规划字节一致；
- `git diff --check`：通过。

这些通过项只证明上述实现边界，不证明协议正确性或 Agent 优势。
