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
  <a href="#配置说明">配置</a> •
  <a href="#使用方法">使用</a> •
  <a href="#内存管理">内存管理</a> •
  <a href="#常见问题">FAQ</a>
</p>

---

## 简介

Subs-Check 是一个轻量级、高性能的订阅节点检测工具，专为代理订阅管理而设计。支持批量检测节点可用性、测速、媒体解锁检测等功能，并提供 HTTP API 接口方便集成。

**本仓库已重构**: 采用扁平化1级目录结构，26个Go文件，7个包，编译通过，功能100%保留。详见 [RESTRUCTURE.md](RESTRUCTURE.md) 和 [FUNCTION_COMPARISON.md](FUNCTION_COMPARISON.md)。

## 主要特性

### 🚀 核心功能
- **批量节点检测** - 支持并发检测大量节点，可自定义并发数
- **速度测试** - 内置测速功能，自动过滤慢速节点
- **媒体解锁检测** - 支持 Netflix、Disney+、YouTube、OpenAI、Gemini、TikTok 等平台
- **IP 风险评估与纯净度评分** - 使用 IPPure API 检测节点 IP 的风险等级和纯净度评分（0-100）
- **智能重命名** - 根据 IP 归属地和纯净度评分自动重命名节点（如：US_优秀、HK_中等、JP_极佳）
- **去重优化** - 自动去除重复节点

### 🔄 自动化管理
- **定时检测** - 支持 cron 表达式和固定间隔两种定时模式
- **远程订阅源** - 支持从远程清单集中维护订阅链接（`sub-urls-remote`）
- **节点数量检测** - 检测订阅是否被清空或节点过少（`sub-urls-min-node-count`）
- **智能失败处理** - 本地订阅失败达阈值后自动删除，远程订阅失败仅记录不删除
- **订阅源追踪** - 区分本地和远程订阅，确保删除操作安全可控
- **热重载配置** - 配置文件修改后自动生效，无需重启
- **订阅结果保存** - 保存到本地文件

### 🌐 Web 服务
- **HTTP API** - 内置 Web 服务，提供订阅链接和管理接口
- **Web 管理面板** - 可视化配置管理、实时状态监控、日志查看
- **订阅管理 API** - 支持通过 API 动态添加/删除订阅链接

### 💾 内存优化
- **软内存限制** - 限制最大内存使用而不杀死进程
- **智能 GC** - 定期垃圾回收，防止内存积压
- **内存监控** - 实时监控内存使用情况
- **Panic 恢复** - 全面的错误恢复机制，确保稳定运行

## 快速开始

### 方法一：一键安装（Ubuntu/Debian）

```bash
bash <(curl -Ls https://raw.githubusercontent.com/55gY/subs-check/HEAD/subs-check.sh)
```

安装脚本会自动：
- 从 GitHub Releases 下载最新版本
- 自动检测仓库默认分支
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

# 下载示例配置（使用 HEAD 自动匹配默认分支）
wget -O config/config.yaml https://raw.githubusercontent.com/55gY/subs-check/HEAD/config/config.example.yaml

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

# 服务器测速可用带宽峰值（MB/s）
# 当未设置 concurrent-stage.speed 时，可基于该值自动推导测速并发
network: 20

# 定时检测间隔（分钟）
check-interval: 120

# 或使用 cron 表达式（优先级高于 check-interval）
# cron-expression: "0 */2 * * *"  # 每2小时执行

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

**测速并发建议：**

- 当使用 `concurrent-stage.speed` 时，测速 worker 数量会以该配置为准
- 当未设置 `concurrent-stage.speed` 且配置了 `network` 时，程序会按带宽峰值自动推导测速并发
- 阶段通道容量也会按分阶段并发创建，不再依赖旧版 `concurrent`
- 如果 `total-speed-limit: 0`，程序会自动对测速并发做保护性收紧，避免在低带宽或共享链路下出现下载堆积、连接暴涨和阶段2崩溃
- 对于约 `20MB/s` 的链路，建议将 `concurrent-stage.speed` 控制在 `8-32` 区间内，再根据测速文件质量和节点速度逐步调整

**测速并发优先级：**

1. `concurrent-stage.speed`
2. `network`
3. 旧版 `concurrent` 推导
4. 内置默认值

## [2026-03-09] 变更说明
**调整内容**:
1. 修复阶段2测速通道关闭竞态，避免 `speedChan` 在发送过程中被提前关闭。
2. 为不限总带宽场景增加测速并发保护，并支持通过 `network` 自动推导测速并发。
3. 修复测速与媒体检测处理数重复累计问题。
4. 修复阶段显示过早切换到媒体检测的问题。
5. 修复 Web 面板仍按旧语义展示阶段和统计的问题。
6. 修正测速保护逻辑中写死带宽值的问题。
7. 修正状态接口 `failed` 在检测进行中将媒体处理中节点误计为失败的问题。
8. 增强阶段进度信息，状态接口与 Web 面板现在会显示当前阶段的实际总量与完成进度。
9. 修复总进度条展示语义，前端不再把加权虚拟进度误显示为真实处理数量。
10. 修复详细统计中的“总进度”展示语义，改为总进度百分比而不是虚拟数量。
11. 简化内部进度模型，`Progress` 现在直接表示总进度百分比（保留两位小数），不再存储虚拟处理数量。
12. 统一阶段完成日志术语，明确区分“阶段总量 / 阶段完成 / 阶段通过”。
13. 修复仅启用媒体检测或仅启用测速时的阶段边界问题，避免后续阶段通道发送错误、死锁或过早关闭 `resultChan`。
14. 增强阶段2/3在强制关闭、超时和 `context cancel` 下的响应性，避免 worker 在下游通道或结果通道发送时长时间阻塞。
15. 为测速与媒体检测网络请求接入 `context`，使强制关闭和超时能更快中断 HTTP 请求。
16. 为 IP 查询与重命名链路接入 `context`，使位置查询、纯净度评分和重命名阶段也能响应取消信号。
17. 统一阶段1取消模型，为 `CheckAlive` / `CheckAliveWithWarmup` 接入外部 `context`，并让 alive worker 在发送到后续阶段前也能及时响应取消。
18. 调整基础重命名规则：`rename-node` 仅保留国家代码，IP 纯净度仅在启用 `iprisk` 时以标签形式追加，避免基础名自带纯净度造成重复显示。
19. 修正媒体阶段统计语义：只有命中至少一个媒体/标签结果时才计入成功；状态接口中的 `stageStats` 同时新增成功率与超时率字段。

**影响范围**:
1. `checker/pipeline.go` 的阶段2-3流水线调度与测速保护。
2. `checker/worker.go` 的测速/媒体检测统计。
3. `checker/progress.go` 的阶段进度信息输出。
4. `core/handlers.go` 的状态接口。
5. `core/admin.html` 的前端状态展示。
6. `config/config.go` 的测速并发计算。
7. `README.md` / `config/config.example.yaml` / `.copilot-instructions.md` 中的配置与规范说明。
8. 命令行进度条 `showProgress` 的展示语义。
9. 阶段完成日志的字段命名与可读性。
10. 阶段2/3在不同启用组合下的通道关闭顺序与结果收集时机。
11. `checker/worker.go` 中阶段2/3在取消场景下的结果发送与下游发送逻辑。
12. `checker/speed.go`、`checker/media.go`、`checker/ai.go` 的请求函数签名与调用链。
13. `provider/info.go` 与 `updateProxyName(...)` 的 IP 查询/重命名调用链。
14. `checker/alive.go` 与 `checker/worker.go` 中阶段1请求和后续发送逻辑。
15. `provider/process.go` 与 `README.md` 中基础命名规则和 `iprisk` 标签语义说明。
16. `checker/progress.go` 与 `core/handlers.go` 输出的阶段统计语义。

**注意事项**:
1. 若链路带宽有限但测速并发过高，仍会导致测速结果失真。
2. 建议优先配置 `network` 或显式设置 `concurrent-stage.speed`，并结合 `total-speed-limit` 一起使用。
3. 测速和媒体检测日志中的“处理数”现在仅表示实际完成该阶段处理的节点数。
4. 状态接口与 Web 面板会同步显示当前真实阶段信息。
5. 状态接口中的 `failed` 现在只统计已明确失败的存活/测速节点，不再把媒体处理中节点计入失败。
6. 文档变更记录中的各字段内容统一使用序号格式，便于阅读。
7. Web 面板中的“阶段进度”现在显示为 `已完成/当前阶段总量`，比单纯显示累计完成数更直观。
8. 若需要在节点名中显示 IP 纯净度，请确保 `platforms` 中启用了 `iprisk`。
8. Web 面板中的“任务进度”现在优先显示当前阶段进度，总进度百分比仍保留为整体加权进度，用于反映整轮任务完成度。
9. 详细统计中的“总进度”现在直接显示百分比，避免把加权虚拟进度误解为真实已处理节点数。
10. `checker.Progress` 的内部语义已切换为“百分比 × 100”；如果后续有其他代码直接读取该值，应按百分比语义使用。
11. 日志中的 `阶段总量` 表示进入该阶段的节点总数，`阶段完成` 表示已完成该阶段处理的节点数，`阶段通过` 仅在存在通过/淘汰语义的阶段出现。
12. 现在会根据 `speedON/mediaON` 的组合分别关闭 `speedChan`、`mediaChan` 和 `resultChan`；若只启用媒体检测，将直接把存活节点送入媒体阶段。
13. 在强制关闭或超时后，阶段2/3 worker 现在会在发送到 `mediaChan` 或 `resultChan` 前优先检查取消信号，以减少阻塞残留。
14. 现在测速与媒体检测的大部分 HTTP 请求已绑定 `context`；取消时通常会比仅依赖 `http.Client.Timeout` 更快退出。
15. 现在 IP 位置查询与重命名相关请求也已绑定 `context`；取消时不再必须等待整轮位置查询链路全部超时后才退出。
16. 现在阶段1存活检测的预热与正式请求也已绑定外部 `context`；在强制关闭、超时或阶段切换时会更快停止，不再只依赖内部 timeout 自行结束。
17. 状态接口中的 `stageStats.alive/speed/media` 现在除 `done/success/failed/timeouts/averageMs` 外，还会返回 `successRate` 与 `timeoutRate`；媒体阶段的 `success/failed` 也会基于真实命中结果统计。

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

**节点命名规则：**

启用 `rename-node` 后，程序会根据节点 IP 的归属地重命名节点；若同时启用 `iprisk`，纯净度会作为标签追加到节点名中：

**命名格式：** `[前缀]国家代码[|纯净度等级][|其它标签]`

**纯净度等级评分标准：**（基于 IPPure fraudScore，0-100分）
- **0-10**：极佳 - IP 质量极高，几乎无风险
- **11-30**：优秀 - IP 质量很好，风险极低
- **31-50**：良好 - IP 质量较好，风险较低
- **51-70**：中等 - IP 质量一般，存在一定风险
- **71-90**：差 - IP 质量较差，风险较高
- **>90**：极差 - IP 质量很差，风险很高

**命名示例：**
```
US               # 仅启用 rename-node
HK|中等          # 同时启用 iprisk，fraudScore 51-70
JP|极佳          # 同时启用 iprisk，fraudScore 0-10
SG|良好|备注     # 同时启用 iprisk，且带自定义备注
```

**IP 位置查询 API 优先级：**
1. **IPPure**（主 API）- 提供位置信息和 fraudScore
2. ip.122911.xyz - 备用
3. IPLark - 备用
4. Cloudflare - 备用
5. EdgeOne - 备用

**注意事项：**
- IP 查询可能因节点质量差导致失败，会稍微影响整体检测速度
- 如果主 API 失败，会自动降级到备用 API（但备用 API 不提供 fraudScore，默认为0）
- `iprisk` 检测平台会直接使用重命名时获取的 fraudScore，无需额外 API 调用
- 基础重命名结果只保留国家代码，纯净度仅在启用 `iprisk` 时以标签形式追加

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

说明：
- `platforms` 按配置顺序执行，常用或更想优先看到结果的平台可以放前面。
- 请尽量避免重复配置同一平台，虽然程序会做去重保护，但保持配置简洁更利于排查问题。
- 若依赖 `filters` 基于国家/纯净度做过滤，建议保留 `iprisk`，并尽量靠前放置，便于更早拿到 `Country` / `IPRisk` 信息。
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

保存方式：

```yaml
save-method: local
output-dir: "./output"
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

# 添加订阅链接
curl -X POST -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"sub_url": "https://example.com/subscription"}' \
  http://localhost:8199/api/config/add

# 添加单个节点（新功能）
curl -X POST -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"ss": "vmess://eyJ2IjoiMi..."}' \
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

通过 Web API 动态添加订阅链接或单个节点，无需手动编辑配置文件：

**API 端点：** `POST /api/config/add`

#### 方式一：添加订阅链接

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

#### 方式二：添加单个节点（新功能）

**请求示例：**
```bash
curl -X POST http://localhost:8199/api/config/add \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"ss": "vmess://eyJ2IjoiMiIsInBzIjoi576O..."}'
```

**支持的协议：**
- VMess: `vmess://...`
- VLESS: `vless://...`
- Shadowsocks: `ss://...`
- ShadowsocksR: `ssr://...`
- Trojan: `trojan://...`
- Hysteria: `hysteria://...`
- Hysteria2: `hysteria2://...`
- TUIC: `tuic://...`
- Juicity: `juicity://...`

**功能特点：**
- ✅ 自动解析节点链接
- 🔍 智能去重检测（根据协议类型匹配关键字段）
- 📝 直接添加到 `all.yaml` 文件
- 🚀 立即生效，无需重启

**去重规则：**
- 基础字段：`type` + `server` + `port`
- 协议特定字段：
  - VMess: `uuid` + `alterId`
  - VLESS: `uuid`
  - Shadowsocks: `cipher` + `password`
  - Trojan: `password` + `sni`
  - 其他协议类似

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

### 智能订阅失败处理（sub-urls-retry-failed）

自动监控和智能处理无法访问的订阅链接，区分本地订阅和远程订阅，避免因临时网络问题误删除：

**配置方法：**
```yaml
# 在 config.yaml 中启用
sub-urls-retry-failed: 3  # 本地订阅失败3次后才删除
sub-urls-min-node-count: 5  # 订阅至少要有5个节点

# 远程订阅清单（失败时仅记录不删除）
sub-urls-remote:
  - "https://example.com/sub-list.txt"

# 本地订阅（失败时可自动删除）
sub-urls:
  - "https://example.com/sub1"
```

**工作机制：**

1. **失败计数跟踪**：
   - 程序会自动生成 `sub_failure_record.txt` 文件
   - 记录每个订阅链接的失败次数和时间
   - 获取成功时会自动重置计数

2. **智能删除策略**：
   - **本地订阅**：只有连续失败次数达到 `sub-urls-retry-failed` 配置值才删除
   - **远程订阅**：达到阈值时输出警告日志，但不删除（由远程清单维护者管理）
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
sub-urls-retry-failed: 5  # 较高的重试次数，避免误删
sub-urls-min-node-count: 3  # 确保订阅有足够节点

# 使用远程订阅清单集中维护
sub-urls-remote:
  - "https://your-repo.com/sub-list.txt"
```

**日志示例：**
```
# 本地订阅失败并删除
WARN 获取订阅链接错误跳过: https://expired.com/sub
INFO 记录订阅链接失败 url=https://expired.com/sub 失败次数=3 删除阈值=3
WARN 已删除失败订阅

# 远程订阅失败但不删除
WARN 获取订阅链接错误跳过: https://remote-list.com/sub
WARN 远程订阅失败次数已达阈值 说明=该订阅来自远程清单，不会从本地配置中删除，请检查远程清单质量
```

**适用场景：**
- 🔄 **长期运行的服务** - 自动维护订阅列表，无需人工干预
- 🧹 **订阅源管理** - 自动清理过期或失效的订阅
- 📊 **订阅源质量监控** - 结合日志监控订阅的可用性

**注意事项：**
- 本地订阅删除操作是永久性的，会修改配置文件
- 远程订阅失败不会被删除，仅记录和警告
- 建议在启用前备份配置文件
- 使用较高的 `sub-urls-retry-failed` 值避免误删（推荐5次以上）
- 可以通过 API 或 Web 界面重新添加被删除的订阅

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
sub-urls-retry-failed: 5  # 本地订阅失败5次后才删除，减少误删
sub-urls-min-node-count: 3  # 确保订阅至少有3个节点
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
