# ConsensusAtlas

ConsensusAtlas 是一个面向分布式共识实现的 Agent 测试研究系统：Agent 决定“优先测试什么”，统一控制层
决定“当前实际能执行什么”，Replay、语义投影和 Oracle 决定“执行中真实发生了什么”。

当前分支：`feature/agentic-consensus-testing`。

## 当前系统流程

```text
协议实现 + 薄 Adapter + 协议知识 + workload/fault/预算
                         |
                         v
              Adapter qualification
                         |
                         v
       Protocol knowledge + TestHypothesis
                         |
                         v
        Explorer 查看可信语义候选并排序
                         |
                         v
     exact-prefix search + Control Runtime 执行
                         |
              Trace / Evidence / PSS / Risk
                         |
            +------------+-------------+
            |                          |
      机械反馈给 Explorer          fresh Replay
                                       |
                                       v
                              Oracle / evaluator
```

输入包括：

- 被测共识实现及一个薄 Adapter；
- 协议知识、风险目标和语义映射；
- workload、允许的故障范围、搜索与模型预算；
- 可选的模型访问，仅由显式 Agent 命令使用。

处理包括：

- Runtime 统一管理消息、自然时间、节点生命周期、持久化副作用和外部输入；
- Explorer 只能排列可信内核给出的候选，不能创造 enabled Action 或修改 Oracle；
- 执行产生 Trace/Evidence，可信代码计算 PSS、Risk progress 和成本；
- fresh Replay 检查可复现性，Oracle/evaluator 独立给出正式结果。

输出分三类：

- finding：可重放的性质违反或候选检出结果；
- testing evidence：PSS/义务/Risk 的发现与覆盖统计；
- cost：决策数、执行工作量、模型调用与 token。

Coverage/PSS 不是“协议正确率”。Agent 也不能用自己的解释替代正式 verdict。

## 当前已实现到哪里

- etcd/raft 可走完整统一 Runtime、qualified execution、fresh Replay、PSS 和 Agreement Oracle；
- OmniPaxos 证明同一 Action/Runtime 不只适用于 Raft；
- HashiCorp Raft 保留部分能力资格，明确表示统一 Action 不等于统一控制强度；
- deterministic semantic best-first 和单 Explorer 已在真实 etcd/raft prefix 上连通；
- 一次公开 DeepSeek 校准中，Explorer 改变了首个扩展，但没有达到完整 RiskWitness，因此不构成 Agent
  优势证据；
- 当前尚未把 Semantic Explorer 选中的路径重新送入完整 qualified bundle/PSS/Oracle 闭环，也尚未形成
  配置时间内自动循环的最终用户流程。

本轮 A2R 收敛删除了已经被替代的 trace/corpus/PSS 批处理搜索、Agent-v1 frontier campaign、未资格化的
raft-rs 路径以及仓库中的重复大型轨迹。历史内容仍可从 Git 提交 `0106e2c` 恢复。

## 核心目录

```text
internal/control/              Action、Item、Adapter 基础类型
internal/controlruntime/       唯一确定性执行与 Replay substrate
internal/conformance/          Adapter 能力机械检查
internal/controlexperiment/    qualified execution、搜索、episode、Campaign
internal/psscore/              协议状态语义投影
internal/semantic/             RiskWitness 等协议族语义
internal/oracle/               独立运行结果检查
internal/defectbench/          candidate/control 评价
adapters/                      目标专用薄适配
cmd/control-experiment/        etcd/raft 组合与 Agent 实验入口
benchmarks/                    当前输入和紧凑结果；大型运行产物放 ignored artifacts/
docs/                          当前设计与阶段总结
```

## 常用检查

日常小修改先运行相关包：

```bash
go test ./internal/control ./internal/controlruntime
go test ./internal/controlexperiment -run 'Semantic|Stateless'
go test ./cmd/control-experiment -run 'A2b2|A2b3'
```

一个完整阶段结束时再运行：

```bash
make test
go vet ./...
git diff --check
```

完整 race 检查只在明确的发布/里程碑检查中运行，不作为每次小改动的前置步骤。普通测试不会读取 key，
也不会调用外部模型。

## 阅读顺序

1. [当前阶段](docs/CURRENT_STAGE.md)
2. [总体规划](docs/ConsensusAtlas-总体规划.md)
3. [架构](docs/architecture.md)
4. [Control Runtime](docs/control-runtime-v2.md)
5. [A2b3b 真实模型公开校准](docs/stage-a2b3b-etcdraft-real-model-calibration.md)
6. [A2R 架构收敛与减负](docs/stage-a2r-architecture-convergence.md)

历史阶段文档描述当时的实现，不应被当作当前 API。当前事实以 `CURRENT_STAGE.md`、总体规划和可编译代码
为准。
