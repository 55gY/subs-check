# 检测流程计数与日志优化

## [2026年2月11日] 修复媒体计数重复与测速统计重复输出

### 调整内容

修复了检测流程中的两个关键问题：
1. **媒体计数错误**：媒体检测数量大于存活数量的逻辑错误
2. **测速统计重复**：每个 worker 都输出统计日志，造成日志混乱

### 影响范围

- **文件**: `checker/worker.go`
  - 添加全局 atomic 计数器（lines 18-25）
  - 重构 `speedWorker` 函数，删除局部统计（lines 118-150）
  - 移除 `mediaWorker` 中的 `tracker.CountMedia()` 重复调用（line 256）

- **文件**: `checker/pipeline.go`
  - 在测速阶段完成后输出汇总统计（lines 502-508）

### 技术细节

#### 1. 媒体计数修复

**问题根源**：
- Line 109: `tracker.mediaDone.Add(1)` （aliveWorkerCollect 预增加）
- Line 205: `tracker.mediaDone.Add(1)` （speedWorker 预增加）
- Line 278: `tracker.CountMedia()` （mediaWorker 实际处理）
- 结果：每个节点被计数 3 次

**解决方案**：
- 移除 mediaWorker 中的 `tracker.CountMedia()` 调用
- 仅保留预增加模式（lines 109, 205）
- 确保媒体检测数量 ≤ 测速成功数量 ≤ 存活数量

#### 2. 测速统计优化

**问题根源**：
- 每个 speedWorker 维护局部 `speedStats` 结构体
- Worker 退出时各自输出日志
- 100 个 worker = 100 条重复日志

**解决方案**：
```go
// 全局 atomic 计数器（所有 worker 共享）
var (
    speedStatsUnder10   atomic.Int32
    speedStatsUnder50   atomic.Int32
    speedStatsUnder100  atomic.Int32
    speedStatsUnder500  atomic.Int32
    speedStatsUnder1000 atomic.Int32
    speedStatsOver1000  atomic.Int32
)
```

- 所有 speedWorker 使用全局 atomic 计数器
- 删除局部变量和 worker 日志输出
- 在 pipeline.go 中统一输出汇总统计

### 变更前后对比

#### 日志输出（变更前）
```
INF 测速统计 <10KB/s=5 10-50KB/s=12 50-100KB/s=8 ...
INF 测速统计 <10KB/s=3 10-50KB/s=15 50-100KB/s=6 ...
INF 测速统计 <10KB/s=7 10-50KB/s=9 50-100KB/s=11 ...
... (重复 100 次)
```

#### 日志输出（变更后）
```
INF 测速检测完成 发送数=2000 处理数=1500 通过数=1254
INF 测速统计 <10KB/s=150 10-50KB/s=320 50-100KB/s=280 100-500KB/s=350 500-1000KB/s=120 >=1000KB/s=34
```

#### 计数逻辑（变更前）
```
存活=1254, 测速成功=1254, 媒体检测=3762 ❌ 逻辑错误
```

#### 计数逻辑（变更后）
```
存活=1254, 测速成功=1254, 媒体检测=1254 ✅ 符合逻辑
```

### 注意事项

1. **统计一致性**：媒体检测 ≤ 测速成功 ≤ 存活数量
2. **日志简洁性**：测速统计仅输出一次汇总数据
3. **并发安全性**：使用 `atomic.Int32` 确保多 goroutine 环境下计数准确
4. **预增加模式**：保留了预增加计数模式，避免计数盲点

### 相关问题链接

- 原始问题：程序在测速+媒体检测阶段突然重启
- 相关修复：独立 context 隔离（参见之前的流程隔离修改）
- 本次修复：解决计数逻辑错误和日志混乱问题

### 测试建议

运行完整检测流程，验证：
- ✅ 媒体检测数量 ≤ 测速成功数量 ≤ 存活数量
- ✅ 测速统计仅输出一次
- ✅ 统计数据准确（各速度区间总和 = 测速成功总数）
- ✅ 不再出现日志重复输出

