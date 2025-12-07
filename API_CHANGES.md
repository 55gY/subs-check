# API 调整说明

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
