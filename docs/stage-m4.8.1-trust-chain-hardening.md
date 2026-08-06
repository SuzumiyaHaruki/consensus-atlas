# M4.8.1 阶段总结：可信证据链加固

日期：2026-08-06
状态：完成
公开复现实验：`benchmarks/pilots/etcdraft-calibration-v2/`

## 阶段结论

M4.8.1 已把“能运行的 calibration”收紧为“工件和执行来源可机械核对的 calibration”。
核心结果没有变化：control 为 `control-pass`，命令数据分歧 calibration 为 `killed`，
两者同为 41 decisions、127 primary work、127 replay work 和 21/55 Coverage；变化的是
Evaluator 不再信任提交者自报的 trace、Ledger、SUT identity 或 report 来源。

## 完成内容

### 1. 唯一的持久化轨迹身份

`core.CanonicalTraceDigest` 对开放 JSON 状态执行 `UseNumber` 规范化，`TraceRecord` 解码不再
把大整数无声转换为 `float64`。Coverage、Campaign 和 defect evaluator 使用同一函数。
Evaluator 会重建 `setup + measurement` trace 并逐条匹配 Ledger run 和 evidence reference。

### 2. 资格作用域

CapabilitySnapshot 新增 Family，资格器拒绝 protocol/Family 不匹配；Candidate Catalog、
requirements 和 snapshot set 在求 digest 前排序。etcd/raft composition 还要求
`profile.ID == raft-campaign/<spec.ID>`，并从已编译 Profile 中验证每个有向容量义务。
旧的 `election-rounds<=2` 自报事实改成 `election-rounds>=1/2` 这类可提供容量事实。

### 3. 构建与执行绑定

BuildAudit 新增输入/输出 module-tree digest，构建后复查整个 module cache tree。
Go replacement 使用稳定的仓库相对 staging identity；同一 control 重建两次时 binary 与
Audit 的 SHA-256 均完全相同。私有 Variant 固定 BuildAudit 和 binary digest。

Submission v2 要求 Campaign report、BuildAudit、binary、Profile 和 plans。Evaluator：

1. 校验 private manifest、BuildAudit 结构和 audit 文件 digest；
2. 校验实际 binary digest、module/source identity 和 Driver Manifest；
3. 不向 SUT 继承用户环境，以两分钟上限亲自重跑 binary；
4. 要求重跑 Campaign report 与提交 report canonical digest 相同；
5. 从重跑 trace 重新执行 trusted Oracle，再写 root-cause ledger。

### 4. 负例

已验证以下输入不能取得有效结果：

- protocol 或 Family 作用域错误；
- Campaign bounds 与编译 Profile 不一致；
- Ledger trace digest、evidence reference 或 Coverage score 被修改；
- 非目标 module 文件改变但目标文件不变；
- BuildAudit/binary 缺失或与 private Variant 互换；
- 提交 Campaign report 与 trusted rerun 不一致。

## Calibration v2

| trial | status | finding | canonical trace | build-bound |
|---|---|---|---|---|
| `trial-41c7a1` | `control-pass` | 无 | `3055c07e…184` | 是 |
| `trial-a93d5e` | `killed` | Agreement step 118 | `5a0c0c92…fb46` | 是 |

Finding trace 与 calibration Campaign Ledger run digest 均为 `5a0c0c92…fb46`。Evaluator
汇总为 1/1 calibration root cause killed、0/1 control false positive、0 invalid。

## 没有证明的内容

- 公开 calibration 不是 holdout，不能证明任何 Agent 搜索优势；
- 四个官方历史候选仍为 deferred，没有正式 defect result；
- Coverage/PSS 仍不能直接产生 kill，也不能表示协议正确概率；
- 当前 trusted runner 未提供通用 OS syscall/network sandbox，只能执行来源和 digest 已冻结的
  curated binary；任意第三方 SUT 进入该路径前需要独立隔离层；
- 稳定 binary digest 的结论限定于相同源码、spec 和 toolchain；跨 toolchain 不要求相同。

## 下一阶段

进入 M4.9：按官方 `9b9d6ee + 63903dd` 场景实现 ReadIndex input、read-state observation、
Driver capabilities 和独立 linearizable-read monitor。只有机械资格变为 `qualified`，且
完整触发边界能够重放，才运行第一个官方历史候选 pilot。此阶段仍不接入新的 Agent 角色。

## 验证记录

- `go test ./...`：通过；
- `go vet ./...`：通过；
- `go test -race ./...`：通过；
- Python Agent 单测：10/10 通过，未调用模型 API；
- JSON 语法：57 个文件通过；
- JSON schema：14 个 schema 自检、16 个关键实例校验通过；
- `git diff --check`：通过；
- 两份 `ConsensusAtlas-总体规划.md`：字节一致；
- calibration v2 trusted rerun：`control-pass/killed`、0 false positive、0 invalid；
- swapped binary 与 tampered report smoke test：均被 evaluator 拒绝。
