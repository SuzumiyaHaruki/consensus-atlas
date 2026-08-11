# M5.18b4：双臂 orchestration 与 pair ledger

日期：2026-08-09

状态：已完成；真实模型调用数 0

## 目的

单臂 consumer 已经保证每个 arm 只能消费 freeze 中的 exact request，并经过一次 transport、审计、
strict parse、baseline validation、plan v2、seed-4 execution 和 IntentOutcome。本阶段不改变这条路径，
只把两臂组织成一项可持久化实验：

```text
                  one validated freeze
                          |
              +-----------+-----------+
              |                       |
       no-feedback arm          with-feedback arm
        consume exactly once     consume exactly once
              |                       |
              +-----------+-----------+
                          |
                          v
      one pair ledger: audits + optional instance/outcome
      + source/post-freeze/model work charged per arm
                          |
                          v
         new artifact directory; no overwrite/fallback
```

## 约束

1. 两臂顺序固定为 `no-feedback -> with-feedback`，各调用单臂 consumer 一次。
2. 第一臂失败不能阻止第二臂；失败 audit 必须进入 ledger，不能换 response、backend 或 seed。
3. 每份 arm proposal 仍先单独对 frozen baseline 校验。只有两臂都通过 baseline/compile 后，才额外检查
   pairwise hard-field equality。
4. source fixture 可以物理复用，但 `SourceObservedWork` 完整计入每个 arm。
5. post-freeze work 从该 arm 的 invocation audit 和可选 execution instance 推导；模型调用必须为每臂 1。
6. pair ledger 只保存小型身份和 audit，不嵌入大型 report/bundle body。完整对象独立持久化，以 digest
   与 audit/outcome 相连。
7. persistence 只能创建一个全新目录；不得覆盖或合并已有实验目录。
8. 本阶段没有 CLI、key 读取或真实模型调用。离线 mock 只验证 orchestration 和工件边界。

## 最小 ledger

根对象绑定：

- freeze、follow-up spec、semantic view 和 baseline digest；
- source reuse policy；
- 两个固定 arm record；
- 每臂 invocation audit；
- 可选 execution instance / IntentOutcome digest；
- source、post-freeze 和 charged work；
- pair totals 和 canonical digest。

arm 的 transport/parse/compile/execution failure 使用已有 audit status/failure code，不在 pair 层再造一套
状态机。`intent-not-reached` 继续只存在于 IntentOutcome，不是 arm failure。

## 测试策略

离线 pair 使用一个成功 arm 和一个 baseline-rejected arm：既覆盖完整 execution/outcome 持久化，也证明
失败隔离和计费。单臂测试中的等价成功执行迁移到 pair 测试，避免在同一 package 再增加一次 seed-4
三节点运行。

## 实现结果

- `consumeEtcdraftAgentB4Pair` 固定先消费 `no-feedback`，再消费 `with-feedback`；使用
  `errors.Join` 汇总失败，因此第一臂失败不会跳过第二臂。
- `AgentInvocationAudit` 继续是 transport/parse/compile/execution 状态的唯一来源；pair 层没有新增
  平行状态机，也没有修改单臂 request、parser、compiler 或 executor。
- `etcdraftAgentB4PairLedger` 绑定 freeze/view/feedback/spec/baseline digest、固定两臂、每臂 audit、
  可选 execution/outcome digest、逐臂成本和总成本。source 虽在进程内物理复用，逻辑上完整计入两臂。
- 持久化先重算 ledger 并核对 digest，只允许创建全新目录；根对象、两臂结构化工件与 exact
  prompt/request bytes 分开保存。被拒绝 arm 不伪造 plan 或 execution 工件。
- 篡改 ledger 在创建目录前被拒绝；覆盖已有目录被拒绝；离线回归扫描全部持久化文件，确认 key 不会
  落盘，transport 私有诊断也不会越过既有 audit 边界。

本阶段新增 290 行生产 composition 和 213 行测试，并从旧单臂测试迁走 31 行等价成功样例。生产代码
总量为 23,070 行，测试代码总量为 8,863 行。没有新增通用 package、第二套 executor、临时 schema
或真实 SUT 成功运行；完整 seed-4 成功路径只保留一份。

## 验收结果

- [x] 两个 exact frozen request 各进入 transport 一次，顺序稳定；
- [x] 第一臂 transport failure 后仍执行第二臂，任一 arm 的失败都由自己的 audit 保留；
- [x] ledger 对每臂重复计入 source cost，并只给实际 execution 计一次 post-freeze attempt；
- [x] 成功 arm 的 audit/report/bundle/instance/outcome identity 闭合；
- [x] 失败 arm 保留 audit、intent 和模型成本，不出现 plan/execution artifacts；
- [x] 新目录持久化完整，第二次写入同一路径失败，篡改 ledger 不创建目录；
- [x] 不保存 key 或 transport diagnostic；
- [x] exact-once 清单为 method 6、execution 7、agent 13，共 26 项；
- [x] 最终 `go test -count=1 ./...` 通过，`cmd/control-experiment` 为 101.670 秒；
- [x] `go vet ./...`、race 清单审计、总体规划副本核对和 `git diff --check` 通过；
- [x] 受影响的完整 13 项 agent race shard 通过，无 data-race 报告，耗时 652.141 秒。
- [x] 最终加入 ledger body 自校验后，单独的 pair race 再次通过，无 data-race 报告，耗时
  407.280 秒。

系统现在具备正式双臂调用所需的本地消费与工件边界，但仍没有正式 pair CLI，也没有读取 key 或运行
真实模型。下一阶段应增加一个显式 opt-in 的 pair 入口：必须先完成并验证 freeze，随后才读取 key；
无论某一 arm 成功或失败，都先持久化 pair ledger，再向调用方返回失败。真实调用仍由用户单独授权。
