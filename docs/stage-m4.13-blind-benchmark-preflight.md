# M4.13 阶段总结：Blind Benchmark 预检与 Exposure Audit

日期：2026-08-06
状态：工具链完成；正式 private holdout 尚未创建

## 结论

M4.12 已限制 Planner 请求，但正式方法比较还需要验证“从 private Manifest 派生的公开
Blind Manifest 与每个实际送入 runner/Planner 的 JSON 工件没有复制 private variant 元数据”。
本阶段新增 curator-side exposure audit，而不创建、猜测或提交新的历史 candidate。

## 已完成

1. `Manifest.Validate` 拒绝把 private `variant.id` 直接复用为公开 `trial_id`。
2. `Manifest.Blind()` 的输出可由 `AuditExposure` 逐字段重算；修改 budget、trial 集或 digest
   会产生 `blind-manifest-mismatch`。
3. 审计递归检查公开 JSON 的字符串值和 object key，拒绝 private variant ID、root cause、category、
   source/SUT/build digest 和 private allowed monitor 的直接泄露。
4. 审计报告只保存 benchmark identity、公共工件 digest、位置编号和稳定 code；不会回显触发
   finding 的 private 值。
5. 新增 `cmd/blind-audit`：curator 在私有环境以 private Manifest、Blind Manifest 和可重复的
   `-public` Planner transcript/request/submission 输入运行它。Blind Manifest 本身自动纳入审计。
6. `benchmarks/exposure-audit-schema-v1.json` 固定审计工件形状。

## 建议的 curator 工作流

```text
private Manifest + qualified builds
        │ defect-eval -blind-out
        v
Blind Manifest + planned public JSON snapshots
        │ blind-audit (must pass)
        v
sealed runner/Planner experiment
        │ trusted defect-eval
        v
root-cause / false-positive evaluation
```

审计只能发现 private Manifest 中已枚举字符串的直接 JSON 泄露；它不证明模型无法根据公开
协议知识、Profile 名称或 capability 组合推断语义，也不替代隔离 runner、最小权限文件系统和
正式 exposure review。

## 本阶段没有证明

- 没有新增或冻结任何正式 historical defect/control；
- 没有调用模型、读取 `key.txt` 或运行方法比较；
- 没有证明 Random、DFS、专家或 Blind Planner 的优劣；
- 没有把 public M4.9/M4.11 pilot 转为 holdout。

## 下一步

在仓库外创建至少 3–5 个独立、mechanically qualified 的 candidate/control pairs，并对每个
private Manifest 运行 `defect-eval -blind-out` 与 `blind-audit`。只有每个 scope 通过 audit 且
预算/方法/报告协议预注册后，才执行第一次受限模型实验。
