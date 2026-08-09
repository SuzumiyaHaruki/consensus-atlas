# M5.18b4：显式 opt-in pair runner

日期：2026-08-09

状态：已完成；真实模型调用数 0

## 目的

pair consumer 和 persistence 已经通过离线验证，但还没有正式入口。本阶段只增加一个显式 opt-in
composition runner，把既有组件按可信顺序连接起来：

```text
validate flags + require a new artifact directory
                         |
                         v
             build and fully validate freeze
                         |
                         v
              read explicitly named key file
                         |
                         v
       consume no-feedback -> with-feedback exactly once
                         |
                         v
       persist pair result before returning arm failure
```

入口不会生成新 request、重写 Agent 回复、修改 Runtime、选择新 seed 或引入第二套执行器。

## 冻结约束

1. strategy 必须显式选择 pair runner，并且同时给出 key 文件和全新 artifact directory；其它输出参数
   不得混用。
2. artifact directory 的存在性检查发生在昂贵 source construction 之前；已有目录立即拒绝，不能覆盖。
3. runner 必须先构造 freeze，再调用完整 `validateEtcdraftAgentB4RequestFreeze`，最后才调用 key reader。
4. key 只传入已有 pair consumer，不进入 freeze、ledger、stdout 或 artifact。
5. pair consumer 返回合法 ledger 和 typed arm failure 时，runner 必须先持久化，再向调用方返回 failure。
6. 若 freeze、key、内部 ledger invariant 或 persistence 失败，不能把它伪装成 arm result。
7. 不自动 retry，不替换 response/backend/seed，不因某一 arm 失败跳过另一 arm。
8. 增加 CLI 和 Make target 不授权本阶段进行真实调用；真实运行仍需用户明确执行 opt-in 命令。

## 最小实现

- 在 `cmd/control-experiment` 增加 composition-specific runner，复用现有 freeze builder、key reader、
  DeepSeek client、pair consumer 和 persistence。
- runner 只接受少量函数依赖，以便离线测试精确观察 freeze/key 顺序；这些依赖不进入公共 Runtime。
- CLI 输出只包含 artifact 路径、两个 arm status、模型调用计数和 ledger digest。
- Make target 要求调用方显式设置 `AGENT_KEY_FILE` 与 `AGENT_ARTIFACT_DIR`，且不被任何测试目标依赖。

## 实现结果

- `workload-agent-b4-pair` 只接受 Agent key/artifact flags 以及公开 decisions/seed，不与
  report/bundle/method 输出混用。非 Agent strategy 提交 Agent flags 会 fail closed。
- runner 先用 `Lstat` 拒绝任何已存在的 artifact path，然后构造并完整复核 freeze。
  context 若已取消，不会继续读取 key。
- 只有上述步骤通过后才调用既有安全 key reader 和 pair consumer。结果具有合法 ledger
  时先持久化，再返回 typed arm failure 或 stdout failure。
- 离线见证固定为第一臂私有 transport failure、第二臂 baseline rejection。两次模型
  attempt 和两份 audit 都进入 ledger，但私有 transport 诊断不进入 error/stdout/artifact。
- 本阶段净增 90 行生产 composition 和 119 行测试；生产/测试代码总量为
  23,160/8,982 行。没有新增公共 package、schema、Runtime、Adapter、PSS、Oracle 或 executor。

## 验收

- [x] freeze builder 完成且完整校验通过后才读取 key；
- [x] freeze 被篡改时 key reader、transport 和 persistence 均不执行；
- [x] 已有 artifact directory 在 source construction 和 key 读取前被拒绝；
- [x] 两个 typed arm failure 仍生成并持久化完整 pair ledger，runner 随后返回失败；
- [x] persisted artifacts、stdout 和返回错误不包含 key 或 transport 私有诊断；
- [x] CLI 参数组合和非 pair strategy 的 Agent flags fail closed；
- [x] `go test -count=1 ./...` 通过，`cmd/control-experiment` 为 99.492 秒；
- [x] `go vet ./...`、26 项 exact-once race audit、旧路径审计、规划副本和 diff 检查通过；
- [x] 受影响 pair race 通过，无 data-race 报告，耗时 426.731 秒；
- [x] 真实模型调用数保持 0。

完成本阶段后，系统具备人工显式启动一次正式双臂实验的入口；它仍不是可按时间持续运行的 Campaign
Coordinator。下一阶段才开始设计多 attempt、wall-clock ceiling、逻辑预算和检查点。

当前持久化边界处理可返回的 typed arm failure。如果 key reader 自身失败，已发生的 pre-key
source work 还没有独立 checkpoint；如果进程被外部强制终止，也不承诺恢复尚未落盘的
中间 arm。这两点留给 Campaign checkpoint 设计，不在单次 runner 内新增并行日志系统。
