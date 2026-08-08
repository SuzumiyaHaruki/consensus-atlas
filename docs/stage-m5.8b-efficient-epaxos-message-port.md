# M5.8b：efficient/epaxos message-port worker witness

日期：2026-08-07

结论：实验通过，但资格保持未授予。EPaxos 的真实协议帧可以由目标专用 codec-aware MessagePort
冻结、稳定编号并 release/drop；纯 byte chunk 或现有 opaque Gateway 不能直接充当消息对象。

## 1. 冻结问题

M5.8a 只允许本轮回答：官方未修改实现的写出边界，能否在不修改 Action、Runtime、Core PSS 或协议
算法的条件下，形成一个可长期保存的完整 peer item。

停止线为新增生产 Go 0 行、test-only Go 不超过 250 行。成功只说明后续 Binding 有可行接缝，不能
自动创建完整 Adapter 或 validated capability。

## 2. 实验结构

固定目标仍为官方 commit `791b115669fca472d3136f6a2eda46c00b3f8251`、tree
`708b8e37f6b50a4e6b6fcbb95045dc53ad7d6344`。

```text
official genericsmr.Replica.SendMsg
                 |
                 v
         bufio.Writer chunks
                 |
                 v
EPaxos Commit codec-aware assembler   <- target Binding responsibility
                 |
                 v
 route + sequence + bytes stable ID
                 |
          freeze / decision
              /       \
         release      drop
            |           |
     exact bytes       zero bytes
```

probe 使用官方 `epaxosproto.Commit` Marshal/Unmarshal，不重写协议消息格式。MessagePort 接收底层
writer chunk，但只有 codec 确认完整消息后才创建 pending frame；`SendMsg` 在 release/drop 决策完成前
不能返回。release 端再次用官方 codec 验证帧被完整消费。

## 3. 结果

| Trial | Frame bytes | 底层 Write | Released bytes | Stable ID |
|---|---:|---:|---:|---|
| small release | 55 | 1 | 55 | `5a6a077b...378f` |
| large release | 5,139 | 2 | 5,139 | `f410f328...7f1c` |
| large drop | 5,139 | 2 | 0 | `f410f328...7f1c` |

大帧正好否证“每次底层 Write 都是一条消息”。release/drop 的大帧内容相同，route/sequence 相同，
因此 stable ID 相同；终态只由显式 decision 区分。race detector 和冻结 JSON 比较通过。

工件见 [M5.8b benchmark README](../benchmarks/feasibility/efficient-epaxos-m5.8b/README.md) 与
[`result.json`](../benchmarks/feasibility/efficient-epaxos-m5.8b/result.json)。

## 4. 对统一控制层的含义

本轮支持以下设计：

- Runtime 继续只认识统一 Message Item，不认识 EPaxos `Commit`、RPC code 或 command；
- 目标 Execution Binding 必须提供 `RPC code -> frame decoder`，并把完整原始 bytes 交给 Runtime；
- stable ID、长期 pending、release/drop 语义仍可复用公共模型；
- codec/framing 属于薄 Binding 的必需耦合，不应提升到公共 Runtime；
- 现有 connection-level Gateway 可承载字节，但没有 codec 时只能保持 interceptable。

因此，“不同系统只写零协议知识的通用网络代理”不可行；“统一 Action/Item + 目标专用薄 framing
Binding”仍然可行。这是预期且可测量的最小接入成本，不是协议核心耦合。

## 5. 仍未证明

- probe 手工构造 `genericsmr.Replica` 的公开 writer 表面，并未启动完整三节点 EPaxos；
- `epaxos.NewReplica` 会立即启动 goroutine，完整 Binding 尚未证明可在自动连接边界无竞争安装；
- 当前只实现 `Commit` codec，完整目标需要覆盖 source-bound RPC code registry；
- MessagePort 尚未接入 Control Runtime，所以 `runtime-owned-message` 仍未取得 Qualification；
- stable yield、自然时间、durable restart 和 strict replay 仍为 missing/Unsupported；
- 本轮不产生 PSS、Coverage、Agent、Oracle 或缺陷发现结果。

## 6. 体积与复验

- 新增生产 Go：0 行；
- test-only Go：212 行，未超过 250 行停止线；
- probe SHA-256：`511659094ee53280fe3f64b4ad8ee6806177e576cedc86ad88b68d50bd04f235`；
- 使用 `go run -race`，结果与冻结 JSON 逐字节比较；
- EPaxos 未进入仓库 Go module 或生产依赖图。

复验命令：

```bash
make probe-efficient-epaxos-message-port EPAXOS_GOPATH=/path/to/efficient-epaxos-gopath
```

## 7. 下一步

按已冻结路线进入 M5.9 v1 消费者迁移/删除门，不继续扩张 EPaxos Adapter。M5.9 先统计
PSS/Coverage/Agent/benchmark 对 legacy execution 的实际依赖，区分：可直接删除、需迁移、只读历史
工件三类，再以测试保持为前提分批删除。

EPaxos 后续最小 Binding 已被本轮允许，但只有在 M5.9 减负后恢复；届时第一项必须解决完整构造器的
无竞争安装和 RPC registry，不得把 test-only witness 宣称为正式资格。
