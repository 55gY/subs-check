# API 调整说明

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
