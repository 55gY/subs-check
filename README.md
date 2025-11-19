# Subs-Check

<p align="center">
  <strong>高性能订阅节点检测工具</strong>
</p>

> **致谢**: 本项目基于 [beck-8/subs-check](https://github.com/beck-8/subs-check) 进行开发和优化。感谢原作者 beck-8 的优秀工作！

<p align="center">
  <a href="#主要特性">特性</a> •
  <a href="#快速开始">快速开始</a> •
  <a href="#配置说明">配置</a> •
  <a href="#使用方法">使用</a> •
  <a href="#内存管理">内存管理</a> •
  <a href="#常见问题">FAQ</a>
</p>

---

## 简介

Subs-Check 是一个轻量级、高性能的订阅节点检测工具，专为代理订阅管理而设计。支持批量检测节点可用性、测速、媒体解锁检测等功能，并提供 HTTP API 接口方便集成。

## 主要特性

### 🚀 核心功能
- **批量节点检测** - 支持并发检测大量节点，可自定义并发数
- **速度测试** - 内置测速功能，自动过滤慢速节点
- **媒体解锁检测** - 支持 Netflix、Disney+、YouTube、OpenAI、Gemini、TikTok 等平台
- **IP 风险评估** - 检测节点 IP 的风险等级
- **智能重命名** - 根据 IP 归属地自动重命名节点
- **去重优化** - 自动去除重复节点

### 🔄 自动化管理
- **定时检测** - 支持 cron 表达式和固定间隔两种定时模式
- **智能失败处理** - 记录订阅获取失败次数，达到配置的重试次数后才自动删除（`remove-failed-sub` + `remove-failed-sub-retry`）
- **热重载配置** - 配置文件修改后自动生效，无需重启
- **订阅结果保存** - 支持本地文件、GitHub Gist、Cloudflare R2、MinIO、WebDAV 等多种存储方式

### 🌐 Web 服务
- **HTTP API** - 内置 Web 服务，提供订阅链接和管理接口
- **Web 管理面板** - 可视化配置管理、实时状态监控、日志查看
- **订阅管理 API** - 支持通过 API 动态添加/删除订阅链接
- **Sub-Store 集成** - 支持嵌入式 Sub-Store 服务

### 💾 内存优化
- **软内存限制** - 限制最大内存使用而不杀死进程
- **智能 GC** - 定期垃圾回收，防止内存积压
- **内存监控** - 实时监控内存使用情况
- **Panic 恢复** - 全面的错误恢复机制，确保稳定运行

## 快速开始

### 方法一：一键安装（Ubuntu/Debian）

```bash
# 下载安装脚本
curl -O https://raw.githubusercontent.com/55gY/subs-check/master/subs-check.sh
chmod +x subs-check.sh

# 以 root 权限运行（会自动下载二进制和配置文件）
sudo bash subs-check.sh
```

安装脚本会自动：
- 从 GitHub Releases 下载最新版本
- 下载默认配置文件
- 创建 systemd 服务
- 设置开机自启动

### 方法二：手动安装

#### 1. 下载预编译二进制文件

从 [Releases](https://github.com/55gY/subs-check/releases) 页面下载适合您系统的版本：

```bash
# 下载并解压（替换为最新版本号）
wget https://github.com/55gY/subs-check/releases/download/v0.0.0-xxx/subs-check-linux-amd64.tar.gz
tar -xzf subs-check-linux-amd64.tar.gz
cd subs-check
```

#### 2. 准备配置文件

```bash
# 创建配置目录
mkdir -p config

# 下载示例配置
wget -O config/config.yaml https://raw.githubusercontent.com/55gY/subs-check/master/config/config.example.yaml

# 编辑配置文件
nano config/config.yaml
```

#### 3. 运行程序

```bash
# 直接运行
./subs-check -f config/config.yaml

# 或后台运行
nohup ./subs-check -f config/config.yaml > subs-check.log 2>&1 &
```

### 方法三：从源码编译

要求：Go 1.24.3+

```bash
# 克隆仓库
git clone https://github.com/55gY/subs-check.git
cd subs-check

# 编译
make build-linux-amd64

# 运行
./build/subs-check-linux-amd64 -f config/config.yaml
```

## 配置说明

### 基础配置

```yaml
# 显示进度条
print-progress: true

# 并发线程数（根据CPU核心数和网络带宽调整）
concurrent: 20

# 定时检测间隔（分钟）
check-interval: 120

# 或使用 cron 表达式（优先级高于 check-interval）
# cron-expression: "0 */2 * * *"  # 每2小时执行

# 成功节点数量限制（0 表示不限制）
success-limit: 0

# 超时时间（毫秒）
timeout: 1000

# 延迟测试 URL
alive-test-url: http://gstatic.com/generate_204
```

### 测速配置

```yaml
# 测速地址（建议使用稳定的大文件下载链接）
speed-test-url: https://github.com/AaronFeng753/Waifu2x-Extension-GUI/releases/download/v2.21.12/Waifu2x-Extension-GUI-v2.21.12-Portable.7z

# 最低速度要求（KB/s，低于此值的节点会被过滤）
min-speed: 512

# 测速超时时间（秒）
download-timeout: 10

# 单节点测速数据大小限制（MB，0 表示不限）
download-mb: 20

# 总下载速度限制（MB/s，0 表示不限）
total-speed-limit: 0
```

### Web 服务配置

```yaml
# 监听端口
listen-port: ":8199"

# 启用 Web UI
enable-web-ui: true

# 子路径（如使用反向代理）
web-ui-subpath: ""
```

访问方式：
- 订阅链接：`http://your-ip:8199/sub/all.yaml`
- 管理界面：`http://your-ip:8199/admin`

### 节点命名配置

```yaml
# 启用智能重命名
rename-node: true

# 节点名称前缀
node-prefix: ""
```

### 媒体解锁检测

```yaml
# 启用媒体检测
media-check: true

# 检测平台列表
platforms:
  - openai      # OpenAI API / ChatGPT
  - youtube     # YouTube Premium
  - netflix     # Netflix
  - disney      # Disney+
  - gemini      # Google Gemini
  - tiktok      # TikTok
  - iprisk      # IP 风险评估
```

### 订阅源配置

```yaml
# 订阅链接列表
sub-urls:
  - "https://example.com/sub1"
  - "https://example.com/sub2"

# 自动删除获取失败的订阅链接
# 启用后，如果订阅链接连续获取失败，会自动从配置文件中删除
# 注意：删除操作会保留配置文件的注释和格式
remove-failed-sub: false

# 订阅链接失败重试次数
# 当订阅链接获取失败时，会记录失败次数，达到此次数后才会删除该订阅链接
# 这样可以避免因为网络临时波动导致的误删除
# 设置为0表示失败1次就删除，设置为负数表示永不删除
remove-failed-sub-retry: 3

# 保留上次成功的节点（在新检测结果前追加）
keep-success-proxies: false
```

**智能失败处理功能说明：**

程序提供了两级保护机制来避免误删除订阅：

1. **失败计数机制** (`remove-failed-sub-retry`)：
   - 记录每个订阅链接的失败次数
   - 只有达到配置的重试次数后才会删除
   - 成功获取后会重置失败计数

2. **失败记录文件** (`sub_failure_record.txt`)：
   - 自动生成失败记录文件
   - 记录每个订阅的失败次数和时间
   - 可以手动查看和编辑

**工作流程：**
- ✅ 订阅获取成功 → 重置失败计数，保留订阅
- ❌ 订阅获取失败 → 失败次数 +1
- � 失败次数 < 重试次数 → 保留订阅，下次继续重试
- �️ 失败次数 ≥ 重试次数 → 删除订阅

**配置示例：**
```yaml
# 启用智能失败处理
remove-failed-sub: true
remove-failed-sub-retry: 3  # 失败3次后才删除

sub-urls:
  - "https://good-sub.com/link1"     # 正常订阅，保留
  - "https://unstable-sub.com/link2" # 偶尔失败，保留并重试
  - "https://dead-sub.com/link3"     # 连续3次失败后删除
```

### 结果保存配置

支持多种保存方式：

#### 1. 本地文件（默认）
```yaml
save-method: local
save-path: "./output"
```

#### 2. GitHub Gist
```yaml
save-method: gist
gist-token: "ghp_xxxxxxxxxxxxx"
gist-id: "your-gist-id"
```

#### 3. Cloudflare R2
```yaml
save-method: cloudflare_r2
r2-account-id: "your-account-id"
r2-access-key-id: "your-access-key"
r2-access-key-secret: "your-secret-key"
r2-bucket-name: "your-bucket"
```

#### 4. MinIO / S3
```yaml
save-method: minio
minio-endpoint: "minio.example.com:9000"
minio-access-key: "minioadmin"
minio-secret-key: "minioadmin"
minio-bucket: "subs-check"
minio-use-ssl: false
```

#### 5. WebDAV
```yaml
save-method: webdav
webdav-url: "https://dav.example.com"
webdav-username: "user"
webdav-password: "pass"
webdav-path: "/subs"
```

### 完整配置示例

详见 [config/config.example.yaml](config/config.example.yaml)

## 使用方法

### 命令行参数

```bash
./subs-check -f config/config.yaml
```

参数说明：
- `-f` - 指定配置文件路径（必需）

### Systemd 服务管理

如果使用 `subs-check.sh` 安装，服务会自动创建：

```bash
# 查看服务状态
sudo systemctl status subs-check

# 启动服务
sudo systemctl start subs-check

# 停止服务
sudo systemctl stop subs-check

# 重启服务
sudo systemctl restart subs-check

# 查看日志
sudo journalctl -u subs-check -f

# 禁用开机自启
sudo systemctl disable subs-check
```

### HTTP API

#### 获取订阅节点
```bash
# YAML 格式（Clash/mihomo）
curl http://localhost:8199/sub/all.yaml

# Base64 格式（通用订阅）
curl http://localhost:8199/sub/all.txt

# mihomo 配置格式
curl http://localhost:8199/sub/mihomo.yaml
```

#### 管理 API

所有管理 API 都需要在请求头中包含 API 密钥：

```bash
# 获取当前配置
curl -H "X-API-Key: YOUR_API_KEY" http://localhost:8199/api/config

# 更新配置文件
curl -X POST -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"content": "your yaml config content"}' \
  http://localhost:8199/api/config

# 添加订阅链接（重要功能）
curl -X POST -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"sub_url": "https://example.com/subscription"}' \
  http://localhost:8199/api/config/add

# 获取运行状态
curl -H "X-API-Key: YOUR_API_KEY" http://localhost:8199/api/status

# 手动触发检测
curl -X POST -H "X-API-Key: YOUR_API_KEY" \
  http://localhost:8199/api/trigger-check

# 强制停止当前检测
curl -X POST -H "X-API-Key: YOUR_API_KEY" \
  http://localhost:8199/api/force-close

# 获取最近日志
curl -H "X-API-Key: YOUR_API_KEY" http://localhost:8199/api/logs

# 获取版本信息
curl -H "X-API-Key: YOUR_API_KEY" http://localhost:8199/api/version
```

**API 密钥说明：**
- 首次运行时会自动生成随机密钥
- 可在配置文件中通过 `api-key` 字段自定义
- 也可通过环境变量 `API_KEY` 设置
- 密钥会在启动日志中显示

#### 使用 Web 管理界面

访问：`http://localhost:8199/admin`

管理界面提供以下功能：
- 📝 **可视化配置编辑** - 直接编辑 YAML 配置文件
- ➕ **快速添加订阅** - 通过表单添加新的订阅链接（`addConfig` API）
- 📊 **实时状态监控** - 查看检测进度、可用节点数等
- 🔄 **手动触发检测** - 不等待定时，立即开始检测
- 📋 **日志查看** - 查看最近的运行日志
- 🔗 **订阅链接展示** - 复制当前服务器的订阅地址

### Docker 运行

```bash
# 使用 Docker
docker run -d \
  --name subs-check \
  -p 8199:8199 \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/output:/app/output \
  ghcr.io/55gy/subs-check:latest \
  -f /app/config/config.yaml

# 使用 Docker Compose
version: '3'
services:
  subs-check:
    image: ghcr.io/55gy/subs-check:latest
    container_name: subs-check
    ports:
      - "8199:8199"
    volumes:
      - ./config:/app/config
      - ./output:/app/output
    command: -f /app/config/config.yaml
    restart: unless-stopped
```

## 内存管理

Subs-Check 提供了三种内存管理方式：

### 1. 软内存限制（推荐）

限制最大内存使用，但不会杀死进程：

```bash
# 命令行方式
export SUB_CHECK_MEM_SOFT_LIMIT=2GB
./subs-check -f config/config.yaml

# Systemd 服务方式
# 编辑 /lib/systemd/system/subs-check.service
[Service]
Environment="SUB_CHECK_MEM_SOFT_LIMIT=2GB"
```

**工作原理：**
- 使用 Go 运行时的 `debug.SetMemoryLimit()` API
- 当内存使用接近限制时，自动增加 GC 频率
- 不会中断服务，只是会更频繁地回收内存

### 2. 硬内存限制

超过限制后自动重启进程：

```bash
export SUB_CHECK_MEM_LIMIT=4GB
./subs-check -f config/config.yaml
```

适合作为兜底保护，防止内存泄露。

### 3. 内存监控

实时监控内存使用情况：

```bash
export SUB_CHECK_MEM_MONITOR=1
./subs-check -f config/config.yaml
```

每 30 秒输出详细的内存统计信息。

### 推荐配置

在 systemd 服务中同时启用软限制和监控：

```ini
[Service]
Environment="SUB_CHECK_MEM_SOFT_LIMIT=2GB"
Environment="SUB_CHECK_MEM_LIMIT=3GB"
Environment="SUB_CHECK_MEM_MONITOR=1"
```

## 高级功能

### 动态订阅管理（addConfig API）

通过 Web API 动态添加订阅链接，无需手动编辑配置文件：

**API 端点：** `POST /api/config/add`

**请求示例：**
```bash
curl -X POST http://localhost:8199/api/config/add \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"sub_url": "https://example.com/subscription"}'
```

**功能特点：**
- ✅ 自动检测重复链接，避免重复添加
- 📝 保留配置文件的原有格式和注释
- 🔄 添加后自动触发配置热重载
- 🎯 精确定位 `sub-urls` 部分并追加

**响应示例：**
```json
{
  "message": "订阅链接已添加",
  "sub_url": "https://example.com/subscription"
}
```

**使用场景：**
1. **自动化订阅管理系统** - 通过脚本批量添加订阅
2. **第三方集成** - 其他系统调用 API 动态添加订阅
3. **Web 管理界面** - 用户友好的订阅管理

**在 Web 管理界面中使用：**
访问 `http://localhost:8199/admin`，在"添加订阅"表单中输入链接即可。

### 智能订阅失败处理（remove-failed-sub）

自动监控和智能处理无法访问的订阅链接，避免因临时网络问题误删除：

**配置方法：**
```yaml
# 在 config.yaml 中启用
remove-failed-sub: true
remove-failed-sub-retry: 3  # 失败3次后才删除
```

**工作机制：**

1. **失败计数跟踪**：
   - 程序会自动生成 `sub_failure_record.txt` 文件
   - 记录每个订阅链接的失败次数和时间
   - 获取成功时会自动重置计数

2. **智能删除策略**：
   - 只有连续失败次数达到 `remove-failed-sub-retry` 配置值才删除
   - 避免因网络波动、DNS 解析问题等临时故障导致的误删
   - 保留配置文件格式和注释

3. **失败记录文件示例**：
   ```
   # 订阅链接失败记录
   # 格式: URL|失败次数|最后更新时间
   
   https://expired-sub.com/link1|3|
   https://unstable-sub.com/link2|1|
   ```

**适用场景：**
- 🔄 **订阅源清理** - 自动移除长期失效的订阅
- 🛡️ **误删保护** - 网络临时问题不会立即删除订阅
- 📊 **维护监控** - 通过失败记录了解订阅源健康状况

**最佳实践：**
```yaml
# 推荐配置
remove-failed-sub: true
remove-failed-sub-retry: 5  # 较高的重试次数，避免误删
```

**日志示例：**
```
WARN 获取订阅链接错误跳过: https://expired.com/sub
INFO 记录订阅链接失败 url=https://expired.com/sub 失败次数=3 删除阈值=3
INFO 已从配置文件中删除失败的订阅: https://expired.com/sub
```

**适用场景：**
- 🔄 **长期运行的服务** - 自动维护订阅列表，无需人工干预
- 🧹 **订阅源管理** - 自动清理过期或失效的订阅
- 📊 **订阅源质量监控** - 结合日志监控订阅的可用性

**注意事项：**
- 删除操作是永久性的，会修改配置文件
- 建议在启用前备份配置文件
- 如果订阅暂时无法访问（网络问题），也会被删除
- 可以通过 API 或 Web 界面重新添加被删除的订阅

### Sub-Store 服务

启用嵌入式 Sub-Store 服务：

```yaml
enable-substore: true
substore-port: ":3000"
```

访问：`http://localhost:3000`

### 回调通知

支持在检测完成后发送通知：

```yaml
# Webhook 回调
callback-url: "https://your-webhook.com/notify"

# 企业微信通知
wechat-webhook: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"
```

### 订阅成功率警告

当订阅成功率低于阈值时发出警告：

```yaml
# 成功率阈值（0.0-1.0）
success-rate: 0.3  # 低于 30% 时警告
```

## 常见问题

### 1. 程序在测速 30% 时异常终止？

**原因：** 可能是内存不足或某些节点触发了 panic

**解决方案：**
- 启用软内存限制：`export SUB_CHECK_MEM_SOFT_LIMIT=2GB`
- 降低并发数：修改配置中的 `concurrent` 参数
- 启用内存监控观察：`export SUB_CHECK_MEM_MONITOR=1`

### 2. 节点检测速度太慢？

**解决方案：**
- 增加并发数（但注意内存和网络带宽）
- 减少或禁用媒体检测功能
- 使用更快的测速地址
- 设置 `success-limit` 限制节点数量

### 3. 某些订阅无法获取？

**可能原因：**
- 订阅链接失效
- 网络问题
- 订阅格式不支持

**解决方案：**
- 检查订阅链接是否有效
- 启用智能失败处理：`remove-failed-sub: true` 和 `remove-failed-sub-retry: 3`
- 查看失败记录文件：`cat sub_failure_record.txt`
- 查看日志获取详细错误信息：`journalctl -u subs-check -f`

### 4. 如何通过 API 添加订阅链接？

使用 `addConfig` API：

```bash
# 单个添加
curl -X POST http://localhost:8199/api/config/add \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"sub_url": "https://example.com/sub"}'

# 批量添加（shell 脚本）
for url in \
  "https://sub1.com/link" \
  "https://sub2.com/link" \
  "https://sub3.com/link"
do
  curl -X POST http://localhost:8199/api/config/add \
    -H "X-API-Key: YOUR_API_KEY" \
    -H "Content-Type: application/json" \
    -d "{\"sub_url\": \"$url\"}"
done
```

### 5. 智能失败处理如何避免误删订阅？

**保护机制：**
- **失败计数** - 只有连续失败多次（默认3次）才删除
- **失败记录** - 自动生成 `sub_failure_record.txt` 记录失败历史
- **重置机制** - 订阅获取成功后自动重置失败计数

**配置推荐：**
```yaml
remove-failed-sub: true
remove-failed-sub-retry: 5  # 失败5次后才删除，减少误删
```

**监控失败状态：**
```bash
# 查看失败记录
cat sub_failure_record.txt

# 监控日志
tail -f /var/log/subs-check.log | grep "失败次数"
```

### 6. API 密钥在哪里查看？

**查看方式：**
```bash
# 查看服务日志
sudo journalctl -u subs-check | grep "api-key"

# 或在启动日志中查找
# 输出类似：启用Web控制面板 api-key=123456
```

**自定义密钥：**
```yaml
# 在 config.yaml 中设置
api-key: "your-custom-key"
```

或使用环境变量：
```bash
export API_KEY="your-custom-key"
./subs-check -f config/config.yaml
```

### 7. Web 服务无法访问？

**检查事项：**
- 端口是否被占用
- 防火墙是否开放端口
- 监听地址是否正确（`:8199` 表示监听所有接口）

### 8. 配置文件修改后不生效？

程序支持热重载，配置文件修改后会自动生效。如果没有生效：
- 检查配置文件语法是否正确
- 查看日志是否有错误提示
- 某些配置项需要重启服务（如 `listen-port`）

### 9. 如何限制测速流量？

使用 `total-speed-limit` 参数：

```yaml
# 限制总下载速度为 50MB/s
total-speed-limit: 50
```

### 10. 节点去重不起作用？

去重基于节点的连接信息（服务器地址、端口、协议等）。如果节点名称不同但连接信息相同，会被识别为重复节点。

## 性能建议

### 并发数设置

根据您的系统配置调整：

- **低配置（1-2 核）**：`concurrent: 10-20`
- **中等配置（4 核）**：`concurrent: 20-50`
- **高配置（8+ 核）**：`concurrent: 50-100`

### 内存限制

建议配置：

- **最小配置**：512MB（不启用测速和媒体检测）
- **推荐配置**：2GB（启用所有功能）
- **大规模订阅**：4GB+（1000+ 节点）

### 测速优化

- 使用稳定的测速地址
- 设置合理的 `download-mb` 限制（建议 10-50MB）
- 避免使用被墙的测速站点
- 考虑自建测速服务器

## 构建和开发

### 编译所有平台

```bash
make build-all
```

### 只编译 Linux AMD64

```bash
make build-linux-amd64
```

### 清理构建文件

```bash
make clean
```

### 使用 GoReleaser

```bash
goreleaser release --snapshot --clean
```

## 许可证

本项目采用 [LICENSE](LICENSE) 许可证。

## 鸣谢

- [Mihomo](https://github.com/MetaCubeX/mihomo) - 核心代理库
- [Gin](https://github.com/gin-gonic/gin) - Web 框架
- [Sub-Store](https://github.com/sub-store-org/Sub-Store) - 订阅管理服务

## 支持

- 问题反馈：[GitHub Issues](https://github.com/55gY/subs-check/issues)
- 功能建议：[GitHub Discussions](https://github.com/55gY/subs-check/discussions)

---

<p align="center">
  如果这个项目对你有帮助，请给个 ⭐️ Star 支持一下！
</p>
