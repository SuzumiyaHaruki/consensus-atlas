# M5.5a 场景表面与确定性控制分层

日期：2026-08-07

## 结论

旧 `portable-cft-control-v2` 的 8 项 required capability 同时包含协议运行表面和测试框架保证，
因此 `3/8` 容易被误读为协议功能或适配覆盖率。本阶段新增一个向后兼容的派生报告，把两类事实
拆开；没有修改 Manifest、Qualification、Adapter、Runtime 或任何 M5.4 冻结 digest。

## 新模型

五个公共场景表面是：

- `external-input`；
- `message`；
- `lifecycle`；
- `temporal`；
- `durability`。

每个表面分别记录 `present/absent/unknown` 和控制等级：

```text
unavailable < opaque < observable < interceptable < scheduler-owned
```

存在性来自带稳定 evidence code 的 onboarding/Family 知识，只说明目标系统具有该现象。声明最多
只能达到 `interceptable`；`scheduler-owned` 必须由 Adapter Manifest 的 Action/Item 组合推导，
`validated_control` 还必须有对应的可信 Qualification capability 通过。负例证明声明不能自行取得
scheduler credit。

以下测试保证不再当作协议功能：

- stable yield/evidence；
- pure enabled set；
- strict decision replay；
- audited entropy；
- optional process isolation。

## 双实现结果

| 指标 | etcd/raft | HashiCorp Raft |
|---|---:|---:|
| 通用表面存在 | 5/5 | 5/5 |
| Manifest 推导 scheduler-owned | 5/5 | 3/5 |
| 具有独立资格证据的 scheduler-owned | 4/5 | 3/5 |
| required deterministic guarantees | 4/4 | 0/4 |
| process isolation（optional） | Unsupported | Unsupported |

HashiCorp 的输入、消息和生命周期可由 Runtime 调度；timeout 与 durability 存在，但当前控制边界仍
是 opaque。etcd/raft durability 已在 Manifest 中声明 effect/checkpoint 控制，但旧 Profile 没有独立
durability capability，因此本报告保守标为“declared scheduler-owned、validated opaque”。这不是
功能回退，而是发现了旧资格分母遗漏的验证维度。

冻结工件见 [control surface report](../benchmarks/qualifications/control-surfaces-m5.5a/report.json)，
digest 为 `ac8793cf8e635108530a9ddef8564675f9ddfdce9cff22a8814af828e6dda618`。

## 可信边界

- presence 不产生控制分；
- Manifest 声明不自动产生 validated 分；
- Unsupported/unvalidated 不能被 Agent 提升；
- 本报告不是 Coverage、PSS、协议正确率或缺陷剩余概率；
- M5.4e capability matrix 作为历史资格工件保持不变。

## 代码账本

- 新增通用派生与验证逻辑：397 行生产 Go；
- 新增 fresh 双实现与 self-credit 负例：142 行 test-only Go；
- Adapter、Runtime、Action/Item 和官方实现：0 行变化；
- 没有增加 CLI、Agent、PSS、Coverage 或新的 JSON Schema。

该模型已接近 400 行阶段上限；M5.5b 不再扩展 report taxonomy，优先复用这五个表面实现最小
Level-1 黑盒 Target Envelope。

## 下一阶段

M5.5b 只定义进程启动/停止、opaque 网络端点、数据目录和客户端调用的最小 Target Envelope，
并用独立进程 fixture 验证 `observable/interceptable` 两个等级。它不承诺纯黑盒 strict replay，
也不开始 PSS/Coverage/Agent 迁移。

## 复验

```bash
make audit-control-surfaces
go test ./...
```
