# M5.4b.1 行为保持的减负整理

日期：2026-08-07

## 结论

M5.4b.1 没有增加 Action、Item、Adapter 能力或实验结论。在保持 M5.4a 冻结报告和 M5.4b
消息/Apply 闭环不变的前提下，生产 Go 代码净减 104 行，超过 80 行停止门。

这次减少来自删除重复实现，不是压缩格式：

1. probe 与 Runtime Adapter 共用官方节点启动、内存 store、三节点 bootstrap 和记录型 FSM；
2. probe 删除第二套 HashiCorp `Transport`，复用实际 Runtime transport 冻结 RPC；
3. 三个 Adapter 共用 entropy tape 到审计包络的转换；
4. JSON 值到 digest-bound `PayloadEnvelope` 使用一个公共构造器。

## 输入、处理、输出

```text
M5.4a frozen probe + M5.4b runtime integration
                       |
        identify byte-for-byte duplicate mechanisms
                       |
     share constructors without changing public contracts
                       v
same reports, same action counts, same FSM.Apply boundary
```

## 生产代码账本

| 范围 | 整理前 | 整理后 | 变化 |
|---|---:|---:|---:|
| HashiCorp 纵向切片 | 800 | 730 | -70 |
| `internal/control` | 106 | 114 | +8 |
| `internal/controlentropy` | 384 | 401 | +17 |
| fixture Adapter | 585 | 562 | -23 |
| etcd/raft application | 219 | 214 | -5 |
| etcd/raft Adapter | 989 | 958 | -31 |
| **合计** | **3,083** | **2,979** | **-104** |

公共包的少量增加替代了三个实现中的重复逻辑；因此按全仓受影响生产文件计算净值，而不是只报告
某个目录的局部减少。测试和文档不计入生产代码账本。

## 行为冻结门

- M5.4a `report.json` canonical digest 仍为
  `d31033a603185019dd5d35a8942463d31af07b89ec574d849dec59a60ad498b7`；
- M5.4b 已保存报告 canonical digest 仍为
  `23f2a0c1f5a979304cd9059d69fb789eb7576b0590d1c37f156c113e2656dba1`；
- Runtime 集成测试仍执行 2 drop、3 deliver、1 invoke，并到达官方 `FSM.Apply`；
- fixture、etcd/raft 与 HashiCorp Adapter 的错误边界和 entropy audit 形状不变；
- HashiCorp 的 wall clock、随机抖动和操作性时间戳仍然存在，所以 `strict_replay=false` 不变。

## 没有声称

本阶段没有证明新的协议能力、跨实现严格重放、PSS/Coverage 有效性、Agent 优势或缺陷检出能力。
代码减少也不等于架构已经完成；它只解除 M5.4b 的增长停止门。

## 下一阶段

进入 M5.4c 前继续沿用 1,500 行总停止线。M5.4c 只实现最小 lifecycle 切片：保留 store、停止并
重建官方 Raft、验证 incarnation 与消息所有权，再机械生成部分 Qualification。若需要扩展公共
Action/Item，或必须复制第二套 Runtime，应先停止复核设计。

## 验证

- 公共 JSON payload 构造器有等价字节与 digest 单测；
- M5.4a 冻结工件测试与 M5.4b 端到端集成测试通过；
- 全仓 `go test`、`go vet`、`go test -race`、Python、JSON、digest、耦合与 diff 检查见交付记录。
