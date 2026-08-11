# M5.21j：最小可信 RiskWitness

日期：2026-08-11

## 阶段目标与边界

M5.21i 已证明 preference-only Agent 没有产生相对 zero-model 的行为增益，但已有的 ActionKind 集合也
不足以表达 `leader-change-with-inflight-proposal` 是否真的发生。M5.21j 因此只增加一条可信语义
可达性链：family-owned frozen partial order、target-owned milestone projector 与 protocol-neutral
validator。本阶段不调用模型、不运行新 SUT，不修改 Runtime、Adapter、Planner 权限、搜索 backend、
PSS、Coverage 或 Oracle。

## 实现

1. `internal/semantic` 新增 `RiskWitnessSpec/v1` 和 `RiskWitnessResult/v1`。通用代码验证 family/risk/
   milestone identity、DAG、target/execution/projector binding、step ordering 与 canonical digest，并重新
   计算 satisfied/missing/order-violation 和 `reached/not-reached`。target 自报的 status 或篡改 digest
   会被拒绝。
2. `families/raft` 冻结第一个 witness：

   ```text
   workload-invoked-at-coordinator
           < coordinator-changed-while-inflight
           < old-coordinator-restarted-after-change
   ```

   它只是固定 milestone ID 与偏序，不包含 target selector、ActionID 或开放 temporal DSL。
3. `cmd/control-experiment` 的 etcd/raft composition 从实际 `Invoke`、Adapter-owned role/term/incarnation
   evidence、client return step 和 Runtime node transition 投影里程碑。换主必须发生在 workload 尚未
   terminal 时；旧协调者恢复必须使用相同 node ID、更高 incarnation 且晚于换主。
4. 单元 fixture 同时验证完整有序证据得到 `reached`，以及 response 已完成后出现的新 leader 不会被
   称为 `while-inflight`。

## 既有证据重判

M5.21j 只读取 M5.21f 的两个 ActionClass bundle 和 M5.21h 的一个 Uniform bundle。三份均通过原有
bundle validation、strict replay 与 etcd/raft decision projection，然后生成 digest-bound witness：

| execution | workload offered/completed | satisfied | status |
|---|---:|---:|---|
| ActionClass attempt 1 | 0 / 0 | 0/3 | `not-reached` |
| ActionClass attempt 2 | 0 / 0 | 0/3 | `not-reached` |
| Uniform attempt 2 | 0 / 0 | 0/3 | `not-reached` |

三份执行的第一个缺失 milestone 都是 `workload-invoked-at-coordinator`。虽然 trace 中存在 crash、restart、
自然 temporal progress 和 message action，但 workload 只被计划、没有真正 Offer/Invoke，因此不能把
这些独立动作组合成目标风险。归档结果见
[`benchmarks/experiments/etcdraft-v2-risk-witness-m5.21j/`](../benchmarks/experiments/etcdraft-v2-risk-witness-m5.21j/README.md)。

## 证明与未证明

本阶段证明：冻结的协议风险可以由 target evidence 投影成受通用代码验证的有序语义见证；有效、可重放
执行与风险到达是两个独立结论；现有三份 bundle 都没有到达该风险。

本阶段没有证明 Agent、Adaptive、ActionClass 或 Uniform 的效果优势，没有发现协议故障，没有形成
holdout，也没有证明 RiskWitness、PSS 或 Coverage 能预测 defect kill。RiskWitness 不参与 Oracle verdict，
也不是新的覆盖百分比分母。

## 验证

- `go test ./...` 通过，`cmd/control-experiment` 非 race 用时 147.074 秒；
- `go vet ./...`、旧路径/retired experiment/race exact-once 三项审计通过；
- `make test-race-full` 通过且无 data race：method 816.303 秒、execution 379.106 秒、agent
  905.667 秒，其余 package 全部通过；三个分片都未超过各自冻结的 20 分钟上限；
- Python discovery 当前为 0 项测试；新 JSON 工件语法和机械重投影一致性通过。

本轮 race 墙钟显著高于历史记录，应作为后续独立的测试工程成本处理；本阶段没有为缩短时间改变
fixture、测试集合、SUT 执行次数或 timeout。

## 下一阶段

下一步不立即把 witness feedback 交给 LLM。先复用现有 Defect Benchmark 做 holdout readiness audit：
明确当前两个公开校准 root cause 不能充当私有 holdout，机械盘点还缺少的独立 candidate/control pair、
capability 与 trusted monitor。与此同时冻结第二 strict CFT 实现迁移门；在迁移门和最小私有数据集没有
就绪前，不扩大 semantic planner surface 或新增 Agent 角色。
