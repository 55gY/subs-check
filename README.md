# Subs-Check

<p align="center">
  <strong>高性能订阅节点检测工具</strong>
</p>

> **致谢**: 本项目基于 [beck-8/subs-check](https://github.com/beck-8/subs-check) 进行开发和优化。感谢原作者 beck-8 的优秀工作！
>
> 本仓库为 [55gY/subs-check](https://github.com/55gY/subs-check)，已进行了大量优化和功能精简。

<p align="center">
  <a href="#主要特性">特性</a> •
  <a href="#快速开始">快速开始</a> •
  <a href="#项目运行流程">流程</a> •
  <a href="#配置说明">配置</a> •
  <a href="#主要功能说明">功能</a> •
  <a href="#内存管理">内存管理</a> •
  <a href="#附加文档">文档</a>
</p>

---

## 简介

Subs-Check 是一个轻量级、高性能的订阅节点检测工具，专为代理订阅管理而设计。支持批量检测节点可用性、测速、媒体解锁检测等功能，并提供 HTTP API 接口方便集成。

**本仓库已重构**：采用扁平化 1 级目录结构，编译通过，功能保持可用。详见 [FUNCTION_COMPARISON.md](FUNCTION_COMPARISON.md) 和 [FEATURE_CHANGES.md](FEATURE_CHANGES.md)。

## 主要特性

### 🚀 核心功能
- **批量节点检测** - 支持并发检测大量节点，可自定义并发数
- **速度测试** - 内置测速功能，自动过滤慢速节点
- **媒体解锁检测** - 支持 Netflix、Disney+、YouTube、OpenAI、Gemini、TikTok 等平台
- **IP 风险评估与纯净度评分** - 使用 IPPure API 检测节点 IP 风险等级和纯净度评分
- **智能重命名** - 根据 IP 归属地和纯净度结果自动重命名节点
- **去重优化** - 自动去除重复节点

### 🔄 自动化管理
- **定时检测** - 支持 cron 表达式和固定间隔两种定时模式
- **远程订阅源** - 支持从远程清单集中维护订阅链接
- **节点数量检测** - 可检测订阅是否被清空或节点过少
- **智能失败处理** - 本地订阅失败达阈值后自动删除，远程订阅失败仅记录不删除
- **热重载配置** - 配置文件修改后自动生效，无需重启
- **订阅结果保存** - 保存到本地文件并提供订阅接口

### 🌐 Web 服务
- **HTTP API** - 内置 Web 服务，默认端口 `8199`
- **Web 管理面板** - 可视化配置管理、实时状态监控、日志查看
- **订阅管理 API** - 支持通过 API 动态添加订阅链接或节点

### 💾 内存优化
- **软内存限制** - 限制最大内存使用而不直接杀死进程
- **智能 GC** - 定期垃圾回收，防止内存积压
- **内存监控** - 实时监控内存使用情况
- **Panic 恢复** - 全面的错误恢复机制，确保稳定运行

## 快速开始

### 一键安装（Ubuntu/Debian）

```bash
bash <(curl -Ls https://raw.githubusercontent.com/55gY/subs-check/HEAD/subs-check.sh)
```

安装脚本会自动：
- 从 GitHub Releases 下载最新版本
- 自动检测仓库默认分支
- 下载默认配置文件
- 创建 systemd 服务
- 设置开机自启动

## 项目运行流程

1. 使用一键脚本安装程序与默认配置
2. 按需参考 [config/config.example.yaml](config/config.example.yaml) 生成正式配置
3. 程序按“存活检测 → 测速 → 媒体检测 → 输出结果”顺序执行
4. 可通过 Web 面板、状态接口和订阅接口查看结果

## 配置说明

配置项说明以 [config/config.example.yaml](config/config.example.yaml) 为准，示例配置中的注释已经覆盖大部分常用场景。

建议重点关注这些配置组：

- `concurrent-stage`：分阶段并发控制
- `network`、`speed-test-url`、`total-speed-limit`：测速相关
- `platforms`、`media-check`：媒体检测相关
- `sub-urls`、`sub-urls-remote`、`sub-urls-retry-failed`：订阅源管理相关
- `listen-port`、`enable-web-ui`、`api-key`：Web 服务相关

如果你觉得示例配置里的某项注释还不够清晰，优先补充到 [config/config.example.yaml](config/config.example.yaml)，README 不再重复展开所有配置细节。

## 主要功能说明

### 节点检测流程

- 存活检测：先筛掉不可连接节点
- 速度测试：对存活节点进行测速
- 媒体检测：按配置的平台顺序进行解锁检测
- 输出结果：生成可直接使用的订阅结果并支持 Web/API 获取

### Web 与动态订阅

- 提供订阅接口、状态接口和配置管理接口
- 支持通过 `POST /api/config/add` 动态添加订阅链接或节点
- 支持动态订阅筛选输出与当前批次/上次批次回退

详细接口说明见 [API_CHANGES.md](API_CHANGES.md)。

### 节点命名与过滤

- 支持按 IP 归属地重命名节点
- 启用 `iprisk` 后可追加纯净度标签
- 支持通过 `filters` 和 `node-type` 控制保留范围

### 订阅源维护

- 支持本地订阅与远程订阅清单
- 支持失败计数、最小节点数校验与安全删除策略

## 附加文档

- 功能调整/历史变更：详见 [FEATURE_CHANGES.md](FEATURE_CHANGES.md)
- API 变更与接口细节：详见 [API_CHANGES.md](API_CHANGES.md)
- 重构前后功能对比：详见 [FUNCTION_COMPARISON.md](FUNCTION_COMPARISON.md)
- 常见问题：详见 [FAQ.md](FAQ.md)

### HTTP API

#### 常用订阅接口
```bash
# 动态订阅（默认返回较高可用等级结果）
curl http://localhost:8199/sub

# 按阶段和速度筛选
curl "http://localhost:8199/sub?test=1&speed=100"

# 旧静态输出仍可访问
curl http://localhost:8199/sub/all.yaml
```

#### 常用管理接口

所有管理 API 都需要在请求头中包含 API 密钥：

```bash
# 获取当前配置
curl -H "X-API-Key: YOUR_API_KEY" http://localhost:8199/api/config

# 添加订阅链接
curl -X POST -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"sub_url": "https://example.com/subscription"}' \
  http://localhost:8199/api/config/add

# 获取运行状态
curl -H "X-API-Key: YOUR_API_KEY" http://localhost:8199/api/status
```

更完整的接口说明、参数、响应示例与动态订阅说明，详见 [API_CHANGES.md](API_CHANGES.md)。

**API 密钥说明：**
- 首次运行时会自动生成随机密钥
- 可在配置文件中通过 `api-key` 字段自定义
- 也可通过环境变量 `API_KEY` 设置
- 密钥会在启动日志中显示

## 性能建议

### 并发数设置

根据您的系统配置调整：

- **低配置（1-2 核）**：`concurrent-stage.alive: 10-20`
- **中等配置（4 核）**：`concurrent-stage.alive: 20-50`
- **高配置（8+ 核）**：`concurrent-stage.alive: 50-100`

### 内存限制

建议配置：

- **最小配置**：512MB（不启用测速和媒体检测）
- **推荐配置**：2GB（启用所有功能）
- **大规模订阅**：4GB+（100+ 节点）

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

## 支持

- 问题反馈：[GitHub Issues](https://github.com/55gY/subs-check/issues)
- 功能建议：[GitHub Discussions](https://github.com/55gY/subs-check/discussions)

---

<p align="center">
  如果这个项目对你有帮助，请给个 ⭐️ Star 支持一下！
</p>
