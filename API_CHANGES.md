# API 调整说明

## 🔄 运行状态接口补充停止态与当前阶段字段 - 2026年3月29日

### 功能概述

`/api/status` 在保留原有兼容字段的基础上，补充了更直接的运行状态字段，用于支撑 Web 面板展示“当前阶段”和“停止中”状态，避免前端只能从 `checking` 和嵌套 `detailStats` 推断运行态。

### 新增字段

状态接口新增以下扁平字段：

- `stopping`: 是否处于停止流程中
- `statusText`: 当前运行状态文案，例如 `空闲`、`检测中`、`停止中`
- `currentStageCode`: 当前阶段代码，例如 `idle`、`subscription`、`alive`、`speed`、`media`
- `currentStageName`: 当前阶段名称，例如 `订阅获取`、`存活检测`、`测速检测`、`媒体检测`


### 字段语义

1. `checking=true` 表示当前检测任务仍在运行。
2. `stopping=true` 表示当前任务已收到停止信号，正在进入收尾或取消流程。
3. `currentStageName` 优先反映真实检测阶段；若仍在拉取订阅，则返回 `订阅获取`。
4. 这些字段是对现有 `detailStats` 的补充，不替代原有详细统计结构。

### 兼容性说明

1. 原有 `checking`、`progress`、`available`、`failed`、`detailStats` 等字段保持兼容。
2. 旧前端或调用方即使不识别新增字段，也不会影响原有解析逻辑。
3. 新前端可以直接使用扁平字段显示状态，无需自行推断阶段名称。

### 相关代码变更

- `core/handlers.go`:
   - `/api/status` 新增运行状态与当前阶段扁平字段
   - `/api/force-close` 在设置 `ForceClose` 前同步标记应用进入 stopping 状态
- `core/app.go`:
   - 新增应用级 `stopping` 运行态标志
- `util/common.go`:
   - 补齐 `SIGINT/SIGTERM` 信号处理，第一次触发停止当前检测，第二次立即退出


## 🔄 订阅获取接口改为动态筛选输出 - 2026年3月23日

### 功能概述

订阅获取不再只依赖固定输出文件内容，而是改为基于本地数据库中的检测结果动态生成订阅内容。现在订阅接口可以根据检测阶段和测速结果按需筛选节点，返回更贴近实际使用需求的订阅结果。

### 主要特性

- **动态获取**: 订阅内容基于本地数据库记录实时生成，而不是仅依赖固定写死的单一输出结果
- **阶段筛选**: 支持按检测阶段筛选节点，控制返回节点至少通过到哪个测试阶段
- **速度筛选**: 支持按最低速度过滤节点，仅返回满足速度要求的结果
- **速度排序**: 返回结果会按 `speedKBps` 从高到低排序，测速更快的节点优先输出
- **批次回退**: 当前批次无可用结果时，会自动回退到上一批次数据
- **接口兼容**: 仍保留原有 `/sub/...` 访问形式，但接口语义从“读固定文件”升级为“按条件返回结果”


### API 变更

#### 订阅接口

保留原有订阅访问路径，例如：

```text
GET /sub/all.yaml
```

同时支持通过查询参数进行动态筛选：

```text
GET /sub/all.yaml?test=1
GET /sub/all.yaml?test=2&speed=500
```

#### 查询参数

1. **`test`**：最低检测阶段
   - `0`：仅要求存活
   - `1`：要求通过测速阶段
   - 更高阶段值可用于后续更严格筛选

2. **`speed`**：最低速度（KB/s）
   - 仅在需要测速结果的场景下生效
   - 返回速度大于等于指定值的节点

### 工作流程

1. 读取本地数据库中的节点检测结果
2. 优先查询当前批次（current batch）数据
3. 若当前批次没有匹配结果，则自动回退到上一批次（previous batch）
4. 根据 `test` 参数过滤未达到指定检测阶段的节点
5. 根据 `speed` 参数过滤测速不足的节点
6. 按 `speedKBps` 从高到低对结果排序
7. 将筛选后的节点动态输出为订阅内容

### 本地数据库结构

当前动态订阅输出依赖本地 bbolt 数据库，数据库文件默认位于输出目录下：

```text
output/subs.db
```

数据库为单文件 KV 结构，目前主要使用一个 bucket：

1. **`nodes`**：保存节点检测结果记录

每条记录会按自增 ID 存储，记录内容为 JSON 序列化后的节点检测结果。核心字段如下：

- **基础字段**

   - `id`：记录 ID（自增）
   - `batch`：批次标记，`0` 表示当前批次，`-1` 表示上一批次
   - `testStage`：节点已通过的检测阶段
      - `0`：存活检测
      - `1`：测速检测
      - `2`：媒体检测
   - `speedKBps`：测速结果，单位 KB/s
   - `updatedAt`：记录更新时间
   - `proxy`：原始节点配置内容（用于最终动态输出订阅）

其中，动态订阅接口在输出前会基于 `speedKBps` 做倒序排序；若多个节点测速值相同，则保持稳定顺序。

- **媒体/标签结果字段**

   - `openai`
   - `openaiWeb`
   - `youtube`
   - `netflix`
   - `google`
   - `disney`
   - `gemini`
   - `tikTok`
   - `ip`
   - `ipRisk`
   - `country`

### 批次与回退机制

1. 每次写入新一轮检测结果时，旧的当前批次会被整体标记为上一批次。
2. 新结果写入后统一标记为当前批次。
3. 查询订阅数据时优先读取当前批次。
4. 若当前批次没有满足条件的记录，则自动回退读取上一批次。

这样做的目的，是避免某一轮检测暂时无结果时直接导致订阅输出为空。

### 影响范围

1. **订阅接口语义变化**
   - 原来更偏向“读取固定输出文件”
   - 现在变为“按条件从检测结果中动态生成订阅内容”

2. **筛选能力增强**
   - 调用方可按用途获取不同质量等级的节点
   - 适合按测试阶段、速度门槛生成不同订阅入口

3. **数据来源调整**
   - 订阅输出依赖本地数据库中的检测结果批次
   - 不再只是单一静态文件视角

### 注意事项

1. `speed` 参数只有在节点已经过测速阶段时才有实际意义。
2. 若传入较高的 `test` 或 `speed` 条件，可能返回空结果。
3. 当前批次无匹配节点时会自动回退到上一批次，以减少订阅短时空白。
4. 返回结果默认会把测速更高的节点排在前面，便于客户端优先使用高速度节点。
5. 这类接口虽然保留了订阅路径形式，但本质上已经属于支持条件查询的动态 API。

### 相关代码变更

- `core/handlers.go`:
  - 订阅输出接口增加动态查询参数处理
  - 根据请求参数控制返回的节点范围
- `output/db.go`:
  - 新增基于 `testStage` 和 `minSpeed` 的记录查询逻辑
  - 支持当前批次优先、上一批次回退的读取策略


## 🎯 新增检测后添加功能 (test) - 2025年12月26日

### 功能概述

为 `/api/addConfig` 接口新增可选参数 `test`，支持在添加订阅链接或单个节点前进行存活和速度检测，只有通过检测的节点才会被添加到配置文件中。

### 主要特性

- **智能检测**: 对节点进行存活检测和速度测试
- **自动过滤**: 只添加测速通过的节点到 `all.yaml`
- **超时控制**: 检测总超时时间为 120 秒，超时后返回已完成的部分结果
- **并发检测**: 使用配置的并发数进行并发检测，提高效率
- **详细反馈**: 返回检测统计信息（测试节点数、通过数、失败数、添加数、耗时等）
- **向下兼容**: 默认为 `false`，不影响现有使用方式

### API 变更

#### 请求参数

**原有参数：**
```json
{
  "sub_url": "订阅链接（可选）",
  "ss": "单个节点链接（可选）"
}
```

**新增参数：**
```json
{
  "sub_url": "订阅链接（可选）",
  "ss": "单个节点链接（可选）",
  "test": false  // 新增：是否在添加前进行检测（默认 false）
}
```

#### 工作流程

**当 `test: false` 时（默认）：**
- 单节点：直接解析并添加到 `all.yaml`
- 订阅链接：直接添加到配置文件的 `sub-urls`
- 保持原有行为和响应格式

**当 `test: true` 时：**
1. **单节点流程**：
   - 解析节点链接
   - 创建代理客户端
   - 执行存活检测（`CheckAlive`）
   - 执行速度测试（`CheckSpeed`）
   - 通过检测才添加到 `all.yaml`
   - 返回详细检测结果

2. **订阅链接流程**：
   - 获取订阅内容
   - 解析所有节点
   - 并发检测每个节点（存活+测速）
   - 测速通过的节点添加到 `all.yaml`
   - 至少1个节点通过才添加订阅链接到 `sub-urls`
   - 返回详细检测统计

#### 响应格式

**单节点 - test: false（原有格式）：**
```json
{
  "message": "节点已添加"
}
```

**单节点 - test: true（新格式）：**
```json
{
  "message": "节点检测并添加成功",
  "tested_nodes": 1,
  "passed_nodes": 1,
  "failed_nodes": 0,
  "added_nodes": 1,
  "duration": "1.234s",
  "timeout": false
}
```

**订阅链接 - test: false（原有格式）：**
```json
{
  "message": "订阅链接已添加",
  "sub_url": "https://example.com/sub"
}
```

**订阅链接 - test: true（新格式）：**
```json
{
  "message": "订阅检测并添加成功",
  "sub_url": "https://example.com/sub",
  "tested_nodes": 100,
  "passed_nodes": 85,
  "failed_nodes": 15,
  "added_nodes": 80,
  "duration": "45.678s",
  "timeout": false,
  "warning": "部分节点因超时未完成检测"  // 仅在超时时出现
}
```

**错误响应（检测失败）：**
```json
{
  "error": "节点检测失败（检测超时）",
  "tested_nodes": 1,
  "passed_nodes": 0,
  "failed_nodes": 1,
  "duration": "120.000s",
  "timeout": true
}
```

#### 使用示例

**示例 1：直接添加单节点（原有方式）**
```bash
curl -X POST http://localhost:8199/api/addConfig \
  -H "Content-Type: application/json" \
  -d '{
    "ss": "vmess://eyJ2IjoiMiIsInBzIjoi..."
  }'
```

**示例 2：检测后添加单节点**
```bash
curl -X POST http://localhost:8199/api/addConfig \
  -H "Content-Type: application/json" \
  -d '{
    "ss": "vmess://eyJ2IjoiMiIsInBzIjoi...",
    "test": true
  }'
```

**示例 3：直接添加订阅链接（原有方式）**
```bash
curl -X POST http://localhost:8199/api/addConfig \
  -H "Content-Type: application/json" \
  -d '{
    "sub_url": "https://example.com/sub"
  }'
```

**示例 4：检测后添加订阅链接**
```bash
curl -X POST http://localhost:8199/api/addConfig \
  -H "Content-Type: application/json" \
  -d '{
    "sub_url": "https://example.com/sub",
    "test": true
  }'
```

### 技术细节

#### 检测流程

1. **初始化限速桶**: 如果 `checker.Bucket` 未初始化，使用配置的 `total-speed-limit` 初始化
2. **创建超时上下文**: 设置 120 秒总超时
3. **并发控制**: 使用信号量限制并发数为配置的 `concurrent-stage.alive` 生效值
4. **存活检测**: 访问 `alive-test-url`（默认 `http://gstatic.com/generate_204`），检查 HTTP 状态码是否为 2xx
5. **速度测试**: 下载 `speed-test-url` 的内容并计算速度，测速成功后继续添加流程
6. **节点添加**: 测速通过的节点使用互斥锁保护，逐个添加到 `all.yaml`
7. **去重处理**: 使用 `isProxyDuplicate` 检查节点是否已存在，已存在的不重复添加

#### 超时机制

- **总超时**: 120 秒
- **单节点超时**: 使用配置的 `timeout` 值（默认 5000ms）
- **下载超时**: 使用配置的 `download-timeout` 值（默认 10 秒）
- **超时处理**: 
  - 超时后立即停止新任务启动
  - 等待已启动的检测任务完成
  - 返回已完成的部分结果
  - 响应中包含 `timeout: true` 和警告信息

#### 并发安全

- **文件写入**: 使用 `sync.Mutex` 保护对 `all.yaml` 的写入操作
- **计数统计**: 使用 `sync/atomic` 的原子操作进行计数
- **协程同步**: 使用 `sync.WaitGroup` 等待所有检测任务完成

#### 节点去重

- 检测通过的节点在添加时会自动检查是否已存在
- 使用 `isProxyDuplicate` 函数根据协议类型检查关键字段
- `passed_nodes` 表示通过检测的节点数
- `added_nodes` 表示实际添加的节点数（排除已存在的）

### 配置要求

使用此功能需要确保以下配置项已正确设置：

```yaml
concurrent-stage:
   alive: 10                # 并发检测数
timeout: 5000              # 单节点超时（毫秒）
alive-test-url: "http://gstatic.com/generate_204"  # 存活测试URL
speed-test-url: "https://speed.cloudflare.com/__down?bytes=20000000"  # 测速URL
download-timeout: 10       # 下载超时（秒）
total-speed-limit: 0       # 总速度限制（KB/s），0 表示不限制
```

### 注意事项

1. **检测耗时**: 大量节点可能需要较长检测时间，建议前端显示进度或加载提示
2. **资源占用**: 并发检测会占用系统资源，建议根据服务器性能调整 `concurrent-stage` 值
3. **超时设置**: 120 秒超时是硬性限制，超时后未完成的节点会被标记为失败
4. **订阅添加**: 订阅链接只有在至少1个节点通过检测时才会被添加到 `sub-urls`
5. **兼容性**: 不设置 `test` 或设置为 `false` 时，完全保持原有行为

### 相关代码变更

- **core/handlers.go**: 
  - 修改 `addConfig` 函数添加 `Test` 参数支持
  - 新增 `testAndAddNodes` 函数实现并发检测和添加
  - 新增 `addSingleNodeFromProxy` 函数用于从 proxy map 添加节点
  - 新增 `parseSubscriptionNodes` 函数解析订阅内容
  - 新增 `TestResult` 结构体存储检测结果

---

## 🐛 修复远程订阅失败删除问题 - 2025年12月25日

### 问题描述

在之前的版本中，存在一个严重的设计缺陷：

- 本地订阅（`sub-urls`）和远程订阅（`sub-urls-remote`）获取的订阅链接会合并在一起
- 当订阅获取失败达到阈值时，程序会尝试删除失败的订阅链接
- 但删除操作 `RemoveSubUrlFromConfig()` 只能从本地配置文件的 `sub-urls` 段删除
- 导致远程订阅链接删除失败，产生错误日志："删除订阅失败"
- 失败记录不断累积，每次检测都会重复尝试删除，造成资源浪费

### 解决方案

实现了订阅源追踪机制，区分本地订阅和远程订阅，采取不同的失败处理策略。

### 主要变更

#### 1. **proxy/get.go - resolveSubUrls() 函数**

**旧签名：**
```go
func resolveSubUrls() ([]string, int, int)
// 返回：订阅URL列表, 本地订阅数量, 远程订阅数量
```

**新签名：**
```go
func resolveSubUrls() ([]string, map[string]bool, int, int)
// 返回：订阅URL列表, 本地订阅标识映射, 本地订阅数量, 远程订阅数量
```

**变更内容：**
- 新增 `localUrls := make(map[string]bool)` 映射表
- 遍历本地配置 `config.GlobalConfig.SubUrls` 时，将每个 URL 标记为 `localUrls[url] = true`
- 远程订阅获取的 URL 追加到 `urls` 列表，但**不**添加到 `localUrls` 映射
- 通过 `localUrls` 映射可准确判断订阅来源

#### 2. **proxy/get.go - GetProxies() 函数**

**旧签名：**
```go
func GetProxies() ([]map[string]any, []string, []string, error)
// 返回：代理列表, 失败订阅, 成功订阅, 错误
```

**新签名：**
```go
func GetProxies() ([]map[string]any, []string, []string, map[string]bool, error)
// 返回：代理列表, 失败订阅, 成功订阅, 本地订阅标识映射, 错误
```

**变更内容：**
- 调用 `resolveSubUrls()` 时接收新的 `localUrls` 返回值
- 在返回值中传递 `localUrls` 给调用方

#### 3. **check/check.go - 失败处理逻辑**

**旧逻辑：**
```go
for _, failedUrl := range failedSubs {
    failureCount, _ := config.IncrementFailureCount(failedUrl)
    if config.ShouldRemoveFailedSub(failedUrl, failureCount) {
        slog.Warn("已删除失败订阅")
        config.RemoveSubUrlFromConfig(failedUrl)  // 无论来源都尝试删除
    }
}
```

**新逻辑：**
```go
for _, failedUrl := range failedSubs {
    if !localUrls[failedUrl] {
        // 远程订阅 - 仅记录，不删除
        failureCount, _ := config.IncrementFailureCount(failedUrl)
        if config.ShouldRemoveFailedSub(failedUrl, failureCount) {
            slog.Warn("远程订阅失败次数已达阈值", 
                "说明", "该订阅来自远程清单，不会从本地配置中删除，请检查远程清单质量")
        }
        continue
    }
    
    // 本地订阅 - 记录并删除
    failureCount, _ := config.IncrementFailureCount(failedUrl)
    if config.ShouldRemoveFailedSub(failedUrl, failureCount) {
        slog.Warn("已删除失败订阅")
        config.RemoveSubUrlFromConfig(failedUrl)
    }
}
```

**核心改进：**
- 通过 `localUrls[failedUrl]` 判断订阅来源
- 远程订阅失败：记录失败次数 + 警告日志，**不执行删除操作**
- 本地订阅失败：记录失败次数 + 执行删除操作（原有行为）

#### 4. **config/config.example.yaml - 文档更新**

更新 `sub-urls-retry-failed` 配置项注释：

```yaml
# 订阅链接失败重试次数
# 控制订阅链接获取失败后的处理策略：
# > 0: 启用智能重试机制，失败N次后才删除订阅链接（推荐：3-5）
# = 0: 失败1次就立即删除订阅链接
# < 0: 永不删除失败的订阅链接（默认：-1）
# 注意：
#   1. 删除操作会保留配置文件中的注释和格式
#   2. 此功能仅对本地订阅（sub-urls）生效，远程订阅（sub-urls-remote）失败时会记录但不会删除
sub-urls-retry-failed: 3
```

### 影响范围

1. **本地订阅（sub-urls）**：行为完全不变
   - 失败达到阈值仍会自动删除
   - 保留配置文件注释和格式

2. **远程订阅（sub-urls-remote）**：新增保护机制
   - 失败时记录到 `sub_failure_record.txt`
   - 达到阈值时输出警告日志，提示检查远程清单质量
   - **不会**尝试从本地配置文件删除
   - 避免产生 "删除订阅失败" 错误日志

3. **API 兼容性**：Breaking Change
   - `GetProxies()` 函数签名变更，新增返回值
   - 外部调用需要更新以接收新的 `localUrls` 返回值

### 优势

1. **消除错误日志**：不再产生 "删除订阅失败" 的无效错误
2. **提高性能**：避免重复尝试删除不存在的订阅链接
3. **清晰的职责分离**：
   - 本地订阅由用户在配置文件中维护，程序可自动清理
   - 远程订阅由远程清单维护者管理，程序仅消费和报告状态
4. **更好的用户体验**：警告日志明确提示用户检查远程清单质量

### 升级指南

1. **如果您使用了 sub-urls-remote**：
   - 无需修改配置文件
   - 注意查看日志中的远程订阅失败警告
   - 根据警告评估是否需要更换远程订阅清单源

2. **如果您仅使用 sub-urls**：
   - 完全无影响，行为与之前一致

3. **如果您二次开发调用了 GetProxies()**：
   - 需要更新函数调用以接收新增的 `map[string]bool` 返回值
   - 参考 `check/check.go` 中的使用方式

---

## 🧹 移除 save-method 配置项 - 2025年12月25日

### 变更说明

移除了已失效的 `save-method` 配置项。由于项目在 2025年12月7日 已经移除了所有非 local 的保存方法（gist、r2、s3、webdav），该配置项已无实际作用，代码中硬编码为 `local` 方式。

### 影响范围

1. **配置文件变更**
   - 移除 `save-method` 配置项及说明
   - 已存在的配置文件中若包含该字段，程序会自动忽略

2. **代码变更**
   - 删除 `config.Config.SaveMethod` 字段
   - 清理 `save/save.go` 中日志输出的 SaveMethod 引用
   - 保存方式固定为本地文件（`save/method/local.go`）

### 升级指南

1. **更新配置文件**：无需操作，旧配置文件仍可正常使用
2. **输出目录**：通过 `output-dir` 配置指定输出路径（默认为程序所在目录的 config 目录）

---

## 🔴 移除 callback-script 功能 - 2025年12月25日

### 变更说明

出于安全考虑，完全移除了 `callback-script` 配置及其相关功能。该功能允许在节点检测完成后自动执行用户指定的脚本，存在潜在的任意代码执行安全风险（0day漏洞）。

### 影响范围

1. **配置文件变更**
   - 移除 `callback-script` 配置项
   - 已存在的配置文件中若包含该字段，程序会自动忽略，不会报错

2. **代码变更**
   - 删除 `config.Config.CallbackScript` 字段
   - 删除 `utils/callback.go` 文件及其所有脚本执行逻辑
   - 移除检测完成后的回调脚本调用

### 替代方案

如需在检测完成后执行自定义操作或发送通知，请使用以下方案：

**推荐：使用 Apprise 通知功能**

程序内置了 Apprise 通知支持，可发送通知到 100+ 个平台，包括：
- Telegram: `tgram://{bot_token}/{chat_id}`
- 钉钉: `dingtalk://{Secret}@{ApiKey}`
- 企业微信、飞书、Slack、Discord 等

配置示例：
```yaml
# 填写搭建的 apprise API server 地址
apprise-api-server: "https://notify.xxxx.us.kg/notify"

# 填写通知目标
recipient-url:
  - tgram://xxxxxx/-1002149239223
  - dingtalk://xxxxxx@xxxxxxx

# 自定义通知标题
notify-title: "🔔 节点状态更新"
```

详细配置请参考：
- Apprise 官方文档: https://github.com/caronc/apprise
- 配置示例文件: `config/config.example.yaml`

### 升级指南

1. **如果您正在使用 callback-script**：
   - 请迁移到 Apprise 通知方案
   - 从配置文件中删除 `callback-script` 相关配置

2. **如果您未使用此功能**：
   - 无需任何操作，程序会自动忽略配置文件中的 `callback-script` 字段

---

## 🔴 IP 位置查询 API 调整 - 2025年12月25日

### 主要变更

1. **新增主 API：IPPure**
   - URL: `https://my.ippure.com/v1/info`
   - 返回字段：
     - `ip`: 节点 IP 地址
     - `countryCode`: 国家代码（如 US, HK, JP）
     - `fraudScore`: IP 纯净度评分（0-100）
   - 特点：提供 IP 纯净度评分，用于节点质量评估

2. **原主 API 降级为备用**
   - `https://ip.122911.xyz/api/ipinfo` 从主 API 降级为第一备用 API
   - 调用顺序调整为：
     1. IPPure (新主API)
     2. 原主API (降级为备用)
     3. IPLark
     4. Cloudflare
     5. EdgeOne

### API 函数签名变更

**GetProxyCountry 函数**
```go
// 旧签名
func GetProxyCountry(httpClient *http.Client) (loc string, ip string)

// 新签名
func GetProxyCountry(httpClient *http.Client) (loc string, ip string, fraudScore int)
```

**所有 Get* 函数统一返回 fraudScore**
- `GetIPPure`: 返回实际的 fraudScore
- `GetMe`, `GetIPLark`, `GetCFProxy`, `GetEdgeOneProxy`: 返回 fraudScore=0（备用 API 不提供此数据）

### 节点重命名逻辑变更

**Rename 函数调整**
```go
// 旧签名：基于序号计数
func Rename(name string) string
// 返回：US_1, US_2, HK_1...

// 新签名：基于 IP 纯净度
func Rename(countryCode string, fraudScore int) string
// 返回：US_优秀, HK_中等, JP_极佳...
```

**fraudScore 评级标准**
- 0-10: 极佳
- 11-30: 优秀
- 31-50: 良好
- 51-70: 中等
- 71-90: 差
- >90: 极差

**新增辅助函数**
```go
// 将 fraudScore 转换为中文标签
func GetFraudScoreLabel(score int) string

// 从中文标签反推 fraudScore（用于已有标签时）
func parseFraudScoreFromLabel(label string) int
```

### 节点命名格式变更

**变更前：**
```
HK_1
HK_2
US_1
US_2
```

**变更后：**
```
HK_中等
HK_优秀
US_极佳
US_良好
```

### iprisk 检测逻辑优化

- 不再调用 `platform.CheckIPRisk` 函数
- 直接使用 `GetProxyCountry` 返回的 `fraudScore`
- 通过 `GetFraudScoreLabel` 转换为中文标签
- 性能提升：减少一次 API 调用

### 修改的文件

1. **proxy/info.go**
   - 新增 `GetIPPure()` 函数
   - 修改 `GetProxyCountry()` 返回值签名
   - 更新所有 `Get*()` 函数返回值
   - 调整 API 调用优先级

2. **proxy/rename.go**
   - 修改 `Rename()` 函数签名和逻辑
   - 新增 `GetFraudScoreLabel()` 函数
   - 移除序号计数器逻辑
   - 移除未使用的 `strconv` 导入

3. **check/check.go**
   - 更新 `mediaWorker()` 中的 iprisk 检测逻辑
   - 更新 `updateProxyName()` 函数调用
   - 新增 `parseFraudScoreFromLabel()` 辅助函数

### 优势

1. **更直观的节点质量标识**：从序号变为质量标签，一目了然
2. **减少 API 调用**：iprisk 检测不再需要额外 API 请求
3. **多 API 容错**：保留 5 个备用 API，确保高可用性
4. **统一数据源**：fraudScore 和位置信息来自同一接口，数据一致性更好

### 向后兼容性

- 配置文件无需修改
- 输出格式兼容（仅节点名称格式变化）
- 不影响现有订阅链接的使用

---

## 🔴 配置项优化 - 2025年12月25日

### 新增功能

1. **sub-urls-min-node-count** (最低节点数量阈值)
   - 新增订阅节点数量检测功能
   - 当订阅解析后的有效节点数量低于此值时，将该订阅记录为失败
   - 设置为 0 表示不启用此检查（默认）
   - 节点数量统计在节点类型过滤（node-type）之后、去重之前执行
   - 统计包含过滤前和过滤后的节点数量，并记录在日志中

### 配置项重命名

为保持配置命名一致性（统一使用 `sub-urls-` 前缀），进行以下重命名：

1. **remove-failed-sub-retry** → **sub-urls-retry-failed**
   - 订阅链接失败重试次数控制
   - 功能保持不变，仅重命名以符合命名规范

### 移除未实现的配置项

为避免用户对未实现功能产生困惑，删除以下配置项及相关代码：

1. **success-rate** (符合条件节点数量的占比)
   - 删除 `config.go` 中的 `SuccessRate` 字段
   - 删除 `config.example.yaml` 中的配置项及注释
   - 该功能从未实现，预期功能为打印低质量订阅链接

2. **keep-success-proxies** (保留成功节点)
   - 删除 `config.go` 中的 `KeepSuccessProxies` 字段和 `GlobalProxies` 变量
   - 删除 `config.example.yaml` 中的配置项及注释
   - 删除 `check/check.go` 和 `app/app.go` 中的相关逻辑
   - 该功能可通过订阅 `http://127.0.0.1:8199/sub/all.yaml` 实现

3. **filter-regex** (过滤正则表达式)
   - 删除 `config.go` 中的 `FilterRegex` 字段
   - 未在代码中实现节点过滤功能

4. **mihomo-api-url** (Mihomo API 地址)
   - 删除 `config.go` 中的 `MihomoApiUrl` 字段
   - 未实现 Mihomo API 集成功能

5. **mihomo-api-secret** (Mihomo API 密钥)
   - 删除 `config.go` 中的 `MihomoApiSecret` 字段
   - 未实现 Mihomo API 集成功能

6. **success-limit** (成功节点数量限制)
   - 删除 `config.go` 中的 `SuccessLimit` 字段
   - 删除 `config.example.yaml` 中的配置项及注释
   - 未在代码中实现节点数量限制功能

**修改的文件：**
- `config/config.go`: 
  - 新增 `SubUrlsMinNodeCount` 字段
  - 重命名 `RemoveFailedSubRetry` → `SubUrlsRetryFailed`
  - 删除 `SuccessRate`、`KeepSuccessProxies`、`GlobalProxies` 等 6 个未使用字段
- `config/config.example.yaml`: 
  - 新增 `sub-urls-min-node-count` 配置说明
  - 重命名 `remove-failed-sub-retry` → `sub-urls-retry-failed`
  - 删除 `success-rate`、`keep-success-proxies` 配置块
- `proxy/get.go`: 实现节点数量检测逻辑，统计过滤前后节点数并记录日志
- `check/check.go`: 移除 `KeepSuccessProxies` 相关代码
- `app/app.go`: 移除 `GlobalProxies` 保存逻辑

---

## 🔴 重大变更 - 2025年12月18日

### 移除的功能

1. **Sub-Store 集成**
   - 移除了嵌入式 Sub-Store 服务
   - 不再生成 `mihomo.yaml` 和 `base64.txt` 文件
   - 删除所有 Sub-Store 相关配置项：
     - `sub-store-port`
     - `sub-store-path`
     - `sub-store-sync-cron`
     - `sub-store-produce-cron`
     - `sub-store-push-service`
     - `mihomo-overwrite-url`

2. **多存储方式支持**
   - 仅保留 `local` 本地存储方式
   - 移除的存储方式：
     - GitHub Gist (`gist`)
     - Cloudflare R2 (`r2`)
     - MinIO/S3 (`s3`)
     - WebDAV (`webdav`)
   - 删除相关配置项：
     - `github-gist-id`, `github-token`, `github-api-mirror`
     - `worker-url`, `worker-token`
     - `s3-endpoint`, `s3-access-id`, `s3-secret-key`, `s3-bucket`, `s3-use-ssl`, `s3-bucket-lookup`
     - `webdav-url`, `webdav-username`, `webdav-password`

3. **Docker 支持**
   - 移除 Dockerfile 和容器化支持
   - 建议使用二进制文件直接运行或通过 systemd 管理

4. **未使用的代码**
   - 移除 `check/decay.go`（衰减函数库）
   - 移除 `check/platform/cloudflare.go`（已弃用的 Cloudflare 检测）
   - 移除 `check/platform/iprisk.go`（未集成的 IP 风险检测函数）
   - 移除 `utils/shuffle.go`（通用 Shuffle 函数）
   - 移除 `subs-check.sh`（一键安装脚本）

5. **配置变更**
   - 默认 platforms 中移除 `iprisk`（IP 风险检测仍可通过 `rename-node: true` 自动获取）
   - Result 结构体中移除 `Cloudflare` 字段

### 迁移指南

**配置文件更新：**
```yaml
# 旧配置（不再支持）
save-method: gist / r2 / s3 / webdav
sub-store-port: "127.0.0.1:8299"

# 新配置（仅支持）
save-method: local
output-dir: "./output"
```

**输出文件：**
- 仅生成 `all.yaml`
- 不再生成 `mihomo.yaml` 和 `base64.txt`

**依赖清理：**
- 运行 `go mod tidy` 清理 `github.com/minio/minio-go/v7` 等未使用依赖

---

## 修改时间
2025年12月7日

## 修改内容

### 1. `/api/config/add` API 功能扩展

**原功能：** 仅支持添加订阅链接

**新功能：** 同时支持添加订阅链接和单个节点

### 2. 请求格式变更

#### 方式一：添加订阅链接（原有功能）
```json
{
  "sub_url": "https://example.com/subscription"
}
```

#### 方式二：添加单个节点（新增功能）
```json
{
  "ss": "vmess://eyJ2IjoiMi..."
}
```

### 3. 支持的节点协议

通过 `ss` 字段可以添加以下格式的节点链接：

- **VMess**: `vmess://...`
- **VLESS**: `vless://...`
- **Shadowsocks**: `ss://...`
- **ShadowsocksR**: `ssr://...`
- **Trojan**: `trojan://...`
- **Hysteria**: `hysteria://...`
- **Hysteria2**: `hysteria2://...`
- **TUIC**: `tuic://...`
- **Juicity**: `juicity://...`

### 4. 工作流程

1. **解析节点链接**：使用 mihomo 的 convert 包解析节点链接
2. **读取现有配置**：从 `output/all.yaml` 读取现有节点列表
3. **去重检查**：根据节点名称 (name) 和服务器地址 (server) 判断是否重复
4. **添加节点**：如果不重复，则添加到 `all.yaml` 的 proxies 列表
5. **保存文件**：更新并保存 `all.yaml` 文件

### 5. 响应说明

**成功响应：**
```json
{
  "message": "节点已添加"
}
```

**错误响应：**
```json
{
  "error": "节点已存在"
}
```
或
```json
{
  "error": "解析节点失败: ..."
}
```

### 6. 代码修改文件

1. **app/server.go**
   - 修改 `addConfig()` 函数，添加 `SS` 字段支持
   - 新增 `addSingleNode()` 函数处理单节点添加逻辑

2. **proxy/get.go**
   - 新增 `ParseSingleNode()` 导出函数，用于解析单个节点链接

### 7. 使用示例

```bash
# 添加订阅链接
curl -X POST http://localhost:8199/api/config/add \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"sub_url": "https://example.com/sub"}'

# 添加单个节点
curl -X POST http://localhost:8199/api/config/add \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"ss": "vmess://eyJ2IjoiMiIsInBzIjoi576O5Zu9..."}'
```

## 注意事项

1. `ss` 字段名称固定，但支持所有协议的节点链接（不仅限于 Shadowsocks）
2. 节点去重基于 `name` 和 `server` 两个字段
3. 如果 `all.yaml` 文件不存在，会自动创建
4. 添加的节点会立即生效，无需重启服务
