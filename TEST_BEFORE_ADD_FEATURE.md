# test 参数功能实现总结

## 功能概述

为 `/api/addConfig` 接口新增了 `test` 参数，支持在添加订阅或节点前进行智能检测。

## 实现要点

### 1. 核心功能

- ✅ 添加 `Test` 请求参数（默认 false）
- ✅ 创建统一的 `testAndAddNodes` 检测函数
- ✅ 实现并发检测（存活+测速）
- ✅ 120秒总超时控制
- ✅ 互斥锁保护文件写入
- ✅ 自动节点去重

### 2. 工作流程

**单节点（test: true）：**
```
解析节点 → 存活检测 → 速度测试 → 通过则添加到 all.yaml
```

**订阅链接（test: true）：**
```
获取订阅 → 解析所有节点 → 并发检测 → 通过的节点添加到 all.yaml → 
至少1个通过则添加订阅链接到 sub-urls
```

**test: false（默认）：**
```
保持原有行为，直接添加
```

### 3. 响应格式

**检测模式响应（test: true）：**
```json
{
  "message": "节点检测并添加成功" / "订阅检测并添加成功",
  "tested_nodes": 100,    // 总检测节点数
  "passed_nodes": 85,     // 通过检测节点数
  "failed_nodes": 15,     // 失败节点数
  "added_nodes": 80,      // 实际添加节点数（排除重复）
  "duration": "45.678s",  // 检测耗时
  "timeout": false,       // 是否超时
  "warning": "..."        // 超时时的警告信息（可选）
}
```

**原有模式响应（test: false）：**
```json
{
  "message": "节点已添加" / "订阅链接已添加",
  "sub_url": "..."  // 仅订阅链接
}
```

### 4. 技术实现

#### 新增函数

1. **testAndAddNodes(proxies []map[string]any) TestResult**
   - 统一的节点检测和添加函数
   - 120秒超时控制（使用 context.WithTimeout）
   - 并发检测（使用 semaphore 控制并发数）
   - 原子计数器统计结果
   - 互斥锁保护文件写入

2. **addSingleNodeFromProxy(proxy map[string]any) error**
   - 从 proxy map 直接添加节点到 all.yaml
   - 自动检查重复（使用 isProxyDuplicate）
   - 支持并发调用（需外部加锁）

3. **parseSubscriptionNodes(data []byte) ([]map[string]any, error)**
   - 解析订阅内容为节点列表
   - 支持 YAML 格式
   - 支持 V2Ray 链接格式
   - 支持正则提取链接

#### 关键改进

- **并发安全**: 使用 `sync.Mutex` 保护文件写入
- **超时控制**: 120秒硬性超时，超时后返回部分结果
- **资源清理**: 正确关闭 ProxyClient，避免资源泄漏
- **向下兼容**: 默认 false，不影响现有使用

### 5. 配置依赖

```yaml
concurrent: 10              # 并发检测数
timeout: 5000              # 单节点超时（毫秒）
alive-test-url: "..."      # 存活测试URL
speed-test-url: "..."      # 测速URL
download-timeout: 10       # 下载超时（秒）
min-speed: 0               # 最低速度要求（KB/s）
total-speed-limit: 0       # 总速度限制（KB/s）
```

### 6. 使用示例

**直接添加（原有方式）：**
```bash
curl -X POST http://localhost:8199/api/addConfig \
  -H "Content-Type: application/json" \
  -d '{"sub_url": "https://example.com/sub"}'
```

**检测后添加：**
```bash
curl -X POST http://localhost:8199/api/addConfig \
  -H "Content-Type: application/json" \
  -d '{
    "sub_url": "https://example.com/sub",
    "test": true
  }'
```

### 7. 注意事项

1. 大量节点检测可能耗时较长（最多120秒）
2. 并发检测会占用系统资源
3. 订阅链接只有在至少1个节点通过时才会添加
4. 节点去重由 `isProxyDuplicate` 自动处理
5. 超时后会返回已完成的部分结果

## 文件变更

- **core/handlers.go**: 
  - 添加导入包（context, sync, time, strings, atomic, ratelimit, convert, util）
  - 修改 `addConfig` 请求结构体（添加 `Test` 字段）
  - 修改单节点分支逻辑
  - 修改订阅分支逻辑
  - 新增 `TestResult` 结构体
  - 新增 `testAndAddNodes` 函数
  - 新增 `addSingleNodeFromProxy` 函数
  - 新增 `parseSubscriptionNodes` 函数

- **API_CHANGES.md**: 
  - 添加完整的功能说明文档
  - 包含使用示例和技术细节

## 测试建议

1. **单节点测试**: 测试有效节点和无效节点
2. **订阅测试**: 测试包含多个节点的订阅
3. **超时测试**: 测试大量节点的超时处理
4. **并发测试**: 测试高并发场景的稳定性
5. **兼容性测试**: 确保不设置参数时保持原有行为

## 完成时间

2025年12月26日
