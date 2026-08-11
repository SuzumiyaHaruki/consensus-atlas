# M5.16R：冻结后删除 v1 实现锥体

日期：2026-08-08

## 结论

M5.16 已使 execution、PSS、Oracle 和最小 DefectBench 全部拥有 v2 消费者，因此原先“等所有消费者迁完
才能删”的条件已经满足。M5.16R 删除了 v1 可编译实现锥体，并把默认构建入口收敛到当前 v2 主线。
历史 Markdown 和 JSON 工件保留，但不再要求主分支在线重放旧代码。

## 冻结基线

删除前机械盘点：29,931 行生产 Go/Python、12,559 行测试、总计 42,490；legacy production import
edge 为 28。六个 v1 核心包本身约 2,734 行生产代码和 1,210 行测试；从直接 consumer 展开的依赖锥体
约 7,047 行生产代码和 2,257 行测试。

M5.15 冻结 report digest `e680aabd1c98181e2c87fa470610ed300689ed30deff8b2d543c9211a18948d7`，
file SHA-256 `e7a00826bf4881288696ad5d4c6685d7b82bcf684d7276af1bbf5cc7cad247b1`。
M5.16 control/candidate bundle 和 evaluator digest 另见 M5.16 阶段文档。

## 删除范围

- v1 `core/engine/host/driver` 与官方 Raft Driver；
- Raft Family PSS/monitor/coverage matcher；
- v1 Coverage、Campaign、Explorer、Scenario、TestPlan；
- 旧 DefectBench candidate catalog/preflight/submission；
- Contract/onboarding/binding/catalog 与旧 Python Agent；
- blackbox/gateway 探索实现和只服务于它的 architecture tests；
- v1/v2 migration harness、legacy experiment adapter；
- 对应的 runner/campaign/onboarding/blind/migration CLI。

保留：Control Runtime v2、两个真实 Adapter、Qualification、Core PSS、协议无关 discovery、唯一
Experiment/Replay、ExecutionBundle、Oracle、最小 evaluator、source-bound builder 和当前 DeepSeek
Planner transport。

## 删除后结果

| 指标 | 删除前 | 删除后 |
|---|---:|---:|
| production | 29,931 | 14,564 |
| tests | 12,559 | 6,299 |
| total | 42,490 | 20,863 |
| legacy production import edges | 28 | 0 |

默认 `Makefile` 不再引用已删除命令，也不再运行旧 Python tests 或重放 M5.3 legacy qualification。
`make audit-no-v1` 检查旧源码目录没有 Go/Python 源文件，并扫描所有 Go import，阻止旧依赖回流。

本机 `agents/__pycache__` 可能仍存在被忽略的解释器缓存；它不是源码、不会被 Git 收录，也不参与
构建。守卫按源码和 import 判定，不把缓存目录误报成 v1 回归。

## 为什么保留历史文档/JSON

删除实现不等于改写研究历史。M4/M5 早期文档仍用于解释路线为何改变，旧 JSON 仍是当时结果的冻结
证据。它们可能引用已经删除的路径，文档导航明确把它们标为 archive。当前 README、架构和 Makefile
才是可运行入口。

历史 JSON 约 21.7 MB、63 万行，体积主要来自少数重复 trace 工件。M5.16 已停止继续提交完整 bundle，
因此 M5.16R 不冒险批量改写历史证据；后续如需迁移存储，应单独做内容寻址/压缩并保留文件摘要映射。

## 风险控制

- 删除通过 `apply_patch` 落地，未覆盖用户其他修改；
- 删除后立即运行 `go test ./...`，确认当前 v2 路径没有隐藏 v1 依赖；
- 新增 semantic/oracle/evaluator/实际 bundle 集成测试，替代被删旧测试的可信边界；
- 不修改官方 module cache、外部 raft 工作区、key 文件；
- 本阶段未提交、未推送、未调用 LLM。

## 后续纪律

M5.17 只能在当前 14.5k production line 基线上小步增加强 baseline。若新功能要求恢复旧 Campaign、
Coverage 或第二套执行器，应先证明当前 `ExecutionBundle + evaluator` 无法表达真实需求，而不是复制旧
抽象。
