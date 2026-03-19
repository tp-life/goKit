# 代码设计文档 v3：Registry 化与状态机演进方向

本文记录第三阶段代码设计方向，重点是：

1. 把更多 venue-specific / symbol-specific 规则 registry 化；
2. 把执行层从“若干安全补丁”升级到真正状态机。

---

## 1. 目标

v3 的核心目标有两个：

### 1.1 Registry 化

把下列规则逐步统一收口：

- venue profile
- alias registry
- future symbol family / contract family registry

### 1.2 状态机化

把执行层升级成可定义、可验证、可恢复的双腿状态机。

---

## 2. 为什么需要 v3

当前系统虽然已经有：

- `VenueProfileRegistry`
- 基础 alias 归一化
- 一批 execution states

但它们还没有形成完整的“registry + state machine”体系。

因此 v3 需要补：

1. 更完整的 alias / symbol family registry
2. execution state transition graph
3. websocket 事件驱动的状态推进
4. hedge rollback 完成确认

---

## 3. 建议的演进顺序

1. 先补 execution state transition 文档
2. 再补 execution state machine 实现
3. 再把 websocket 用户流接入为主要状态源
4. 最后把 reconcile polling 退化为兜底

---

## 4. 与其他设计文档的关系

- `code_design_v1_scanner_to_planner.md`：解释如何从 scanner 走到 planner
- `code_design_v2_execution_safety.md`：解释执行安全基础版
- 本文：解释 registry 化和状态机的下一阶段设计方向

