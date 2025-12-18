# API 调整说明

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
