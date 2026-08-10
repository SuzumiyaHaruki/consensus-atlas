# M5.21d5：Single-call External Connectivity Calibration

## 目标

M5.21d1–d4 已用离线 transport 证明 durable call、恢复、执行和 terminal accounting。
M5.21d5 只做一次真实外部连通性校准，回答一个很小的问题：

> 已实现的 opt-in Campaign runner 能否在不放宽信任边界的前提下，完成一次
> `durable intent -> one transport -> durable result -> trusted parse/compile -> qualified execution`？

这不是 Agent 效果实验，不与 zero-model、random 或专家方法比较。

## 冻结输入

- strategy：`campaign-etcdraft-agent-v1`；
- target：当前 official etcd/raft v2 composition；
- 公开 policy seed：151；
- attempts：1；
- decisions per attempt：8；
- wall-clock ceiling：180000 ms；
- model allowance：1 call / 8192 total tokens；
- provider/model：代码中固定的 DeepSeek `deepseek-v4-flash`；
- transport：1200 max output tokens、temperature 0、thinking disabled、0 retry；
- key source：`/home/nitro/Desktop/key.txt`，只由已有安全 reader 在 intent durable 后读取。

工件根目录固定为
`benchmarks/experiments/etcdraft-v2-agent-campaign-m5.21d5/`：

- `campaign/`：crash-safe 可恢复目录；
- `summary.json`：Campaign Summary；
- `observation.json`：Campaign Observation；
- `README.md`：校准边界与实际结果。

## 权限与停止规则

1. 运行前只检查 key 的 regular-file、非 symlink、权限和大小，不打印内容；
2. exact intent 未 durable 时不得读 key；
3. 最多一次 HTTP transport，失败或无效响应不重试；
4. 命令结束后不自动 resume；dispatch-only ambiguous 尤其禁止重调；
5. 无论 proposal 被接受或拒绝，只要 durable result 存在就保留实际 work；
6. key 内容不进入命令行、日志、config、intent、result、Summary 或版本库。

## 可接受结果

这是连通性校准，因此下列结果都必须诚实保存：

- `stopped` + one completed attempt：proposal 通过可信编译并完成执行；
- `failed` + durable result：transport/response/proposal/compile 失败，但调用成本可恢复；
- process interruption after dispatch：恢复为 `ambiguous`，记录后停止，不再调用。

成功验收的最低标准是：最多 1 call，Summary/Observation 可自校验，result/work 可恢复，
没有 key 泄漏。proposal 必须成功不是连通性校准的前提。

## 代码与结论边界

- 预期不新增生产 Go 代码；若现有链路失败，先保留工件并分类，不在同一阶段扩张功能；
- 运行后重验 Summary、Observation、Campaign recovery 与仓库 secret scan；
- 不声称 Agent 规划质量、方法优势、缺陷检出能力、Coverage/PSS 完备性或协议正确性；
- 已有 M5.18b1 单次调用只是旧 one-shot 链路的历史校准；本阶段专门验证新的
  durable multi-attempt Campaign 外壳在 attempts=1 时的真实连通性。

## 完成结果（2026-08-10）

M5.21d5 已按冻结输入执行完成，只发生 1 次外部调用：

- model work：2203 input + 214 output = 2417 total tokens；
- Campaign：`stopped` / `attempt-limit`，1 个 committed completed attempt；
- primary/replay：8 decisions，9/9 work units；
- PSS：9 samples / 9 unique states；
- fault usage：1 crash；
- workload：1 planned / 0 offered / 1 pending；
- Agreement 和 TraceIntegrity 各检查 1 次，零触发。

result 为 `completed`，finish reason 为 `stop`。proposal 通过 strict parser 与 hard-baseline
validation，将 `action-class-random` 放在 preference 首位；trusted compiler 选择
`workload-action-class-random-b4`，并用 seed 151 执行。8-decision 轨迹未选中 Invoke，因此
workload pending 是有界调度事实，不是执行错误或正确性结论。

使用 exact config 对 stopped Campaign 做了独立恢复读取，新生成的 Summary/Observation 与
保存文件字节一致，且目录仍只有 1 个 result。全部 JSON 和 content-addressed
artifact 均通过语法校验；实验目录未出现 key exact match、`Authorization`、`Bearer` 或
`key.txt`。本阶段未修改 Go 代码，没有 retry 或第二次外部调用。

工件见
`benchmarks/experiments/etcdraft-v2-agent-campaign-m5.21d5/`。该结果只证明 durable
transport/parser/compiler/executor/accounting 链路已连通。
