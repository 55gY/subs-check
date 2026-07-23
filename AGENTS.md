# AGENTS.md

本文件为在本仓库工作的 AI 编码助手（Claude、Copilot、Cursor、Codex 等）提供统一的行为约束与项目上下文。核心目标：**最小改动、优先修根因、保持现有结构与风格一致**。

> 人类贡献者也可将本文件作为工程规范参考。

---

## 项目概览

`subs-check` 是一个用 Go 编写的代理订阅检测工具。它拉取订阅链接、解析节点，通过多阶段流水线检测节点的**存活、测速、流媒体解锁**能力，最终输出可用节点列表，并提供 Web 控制面板与动态订阅接口。

核心流程（详见 `checker/pipeline.go` 的 `Check()`）：

```
拉取订阅(provider) → 解析/清洗/去重/乱序 → 阶段1 存活检测 → 阶段2 测速检测 → 阶段3 媒体检测 → 重命名/打标签 → 持久化(bbolt) → 通知
```

---

## 技术栈

| 领域 | 依赖 |
| --- | --- |
| 语言 | Go 1.24.3（`go.mod` 为准） |
| 代理内核 | `github.com/metacubex/mihomo`（协议解析、拨号、UA 生成） |
| HTTP 框架 | `github.com/gin-gonic/gin` |
| 嵌入式存储 | `github.com/metacubex/bbolt`（B-tree KV，**非 SQL**） |
| 配置热重载 | `github.com/fsnotify/fsnotify` |
| 定时调度 | `github.com/robfig/cron/v3` |
| 配置解析 | `gopkg.in/yaml.v3` |
| 日志 | `log/slog` + `github.com/lmittmann/tint` + `lumberjack`（轮转） |
| 限速 | `github.com/juju/ratelimit` |

---

## 目录结构

| 包 / 文件 | 职责 |
| --- | --- |
| `main.go` | 程序入口：初始化日志、创建 `App`、`Initialize()` + `Run()` |
| `core/app.go` | 应用生命周期：定时器 / cron 调度、触发检测、结果持久化 |
| `core/config.go` | 配置文件加载、去重、fsnotify 热重载 |
| `core/server.go` | HTTP 服务器、鉴权中间件、Token/Key 生成 |
| `core/handlers.go` | 所有 REST API handler、动态订阅、手动测节点 |
| `core/admin.html` | Web 控制面板前端（单文件，`embed`） |
| `checker/pipeline.go` | 三阶段检测流水线、并发编排、重命名打标签 |
| `checker/client.go` | 基于 mihomo 的代理 HTTP 客户端、字节统计、限速 |
| `checker/alive.go` `delay.go` | 存活检测（可选统一延迟预热模式） |
| `checker/speed.go` | 测速检测（网络层字节计数 + 应用层大小限制） |
| `checker/media.go` `ai.go` | 流媒体 / AI 服务解锁检测（Netflix/Disney/YouTube/OpenAI/Gemini/TikTok） |
| `checker/progress.go` | 并发安全的进度追踪器与加权进度算法 |
| `provider/fetch.go` | 订阅拉取（直连 / 备用网卡 / 订阅代理四阶段重试） |
| `provider/parse.go` | 单节点链接解析 |
| `provider/process.go` | 去重、重命名、智能乱序、字符清洗 |
| `provider/info.go` | 出口 IP / 国家 / 纯净度查询（多数据源回退） |
| `output/db.go` `db_model.go` | bbolt 数据层：节点批次、鉴权失败记录、迁移/compact |
| `output/output.go` `local.go` | 结果分类与本地 YAML 落盘 |
| `config/config.go` | 配置结构体、默认值、订阅失败记录 |
| `monitor/memory.go` | 内存软/硬限制监控与自动重启 |
| `util/` | 日志、通知、信号处理、URL 处理等工具 |

---

## 构建与验证命令

**默认验证命令：`go build ./...`** —— 在每个重要修改阶段后执行一次，确保不破坏构建。

```bash
go build ./...        # 默认验证：编译全部包
go vet ./...          # 静态检查，提交前必跑
gofmt -w <file>       # 格式化改动过的文件
make build            # 编译当前平台二进制
make linux-amd64      # 交叉编译 Linux/amd64
make build-all        # 编译到 build/ 目录
make update-deps      # 更新 mihomo 及其他依赖
```

> 以**实际构建结果和错误检查为准**，不要被编辑器陈旧诊断（下划线报错）误导。

---

## 总体原则

1. 优先做小而准确的修改，不做无关重构。
2. 优先修复根因，不做表面兜底补丁。
3. 保持现有包结构、命名风格和公开接口稳定，除非需求明确要求调整。
4. 修改前先理解调用链、并发边界和配置影响范围。
5. 涉及多文件的改动时，确保配置、文档、前端展示与后端行为同步。
6. 编辑前保留可回退版本（依赖 git，不要在整理过程中丢失原始内容）。
7. 若用户明确要求“精简 / 整理 / MVP”，默认采用最小展示方案，不额外扩展说明层级。

---

## Go 代码规范

1. 保持函数职责单一，避免把解析、业务、输出、日志混在同一个函数中。
2. 优先复用现有 package 内能力（如 `doRequestAndReadBody`、`createHTTPClient`），不重复造轮子。
3. 错误处理显式：返回错误时用 `fmt.Errorf("...: %w", err)` 提供上下文，但不冗长。
4. 不随意引入全局状态；必须共享状态时，明确并发安全策略（见下）。
5. 非必要不新增抽象层；能在现有结构中完成的修改，不额外拆接口。
6. 新增导出符号时，命名与现有风格保持一致，避免缩写混乱。

---

## 并发与流水线规范

本项目是多阶段并发流水线，修改 `checker/`、`provider/`、`core/`、`output/` 时优先检查：

1. **共享状态的同步方式**
   - 包级共享变量优先使用 `sync/atomic`（如 `checker.Progress`、`Available`；跨 goroutine 的指针用 `atomic.Pointer`）。
   - `map` / slice 的并发写必须加锁（如 `provider.counterLock`、`app.configMutex`）。
   - 不要对普通指针字段做无保护的并发读写。
2. **channel 与 goroutine**
   - 关注 channel 关闭顺序、缓冲区大小、生产者/消费者退出条件。
   - 向下游 channel 发送前，考虑取消、超时和消费者退出场景，避免阻塞发送导致的挂起。
   - 循环中启动的 goroutine 要保证 `wg.Add/Done` 配对，避免泄漏。
3. **context 取消 / 超时传播**
   - 网络请求、测速、媒体检测、IP 查询一律通过 `context` 传递取消信号。
   - 检测超时按节点数与并发数动态计算（见 `calcCheckTimeout`），不要硬编码。
4. **资源释放**
   - HTTP `resp.Body` 必须在使用后关闭；**严禁在 for 循环内用 `defer` 关闭**（defer 累积到函数返回才执行），应显式 `Close()` 或封装单次请求函数。
   - 代理客户端（`ProxyClient`）用完 `Close()` 释放底层连接。
   - 外部响应体读取使用 `io.LimitReader` 限制大小，防止 OOM。
5. **bbolt 事务**
   - 写走 `conn.Update`，读走 `conn.View`（bbolt 自动提交/回滚）。
   - **不要在 cursor 遍历中直接 `Delete`/改变 key 集合**（会跳过元素）；先收集 key 再统一删除（参考 `MigrateAuthRecords` 的延迟删除模式）。
6. 调整阶段并发、缓冲区或 worker 数量时，同步检查配置推导逻辑（`GetAliveConcurrent` 等）和前端/状态接口的展示语义。

---

## 日志规范

1. 统一使用 `log/slog`，键值对形式（`slog.Info("msg", "key", val)`），不用 `fmt` 拼接结构化字段。
2. 保持日志简洁，突出结果与阶段；正常路径不加噪声日志。
3. 错误日志便于定位模块、阶段和失败原因。
4. 高频路径的调试信息使用 `slog.Debug`，避免默认级别刷屏。
5. 严禁将 API Key、Token、密码等敏感信息输出到日志（生成随机凭证并写回配置属于既有行为，但不要新增泄漏点）。

---

## 配置与兼容性规范

1. 修改配置项时，同步更新：配置解析逻辑 → `config/config.example.yaml` → README/相关文档 → Web 前端/接口展示与语义。
2. 优先保持旧配置兼容；若必须改变语义，需在文档中明确迁移影响。
3. 不硬编码环境相关值、带宽阈值、URL 或路径，除非仓库中已有明确约定。
4. `config/config.example.yaml` 是配置项的**唯一详细说明来源**；README 只保留分组提示与引用入口，避免重复。
5. 删除/废弃配置项时，同步清理代码、示例配置、README、API 文档及前端旧术语，不留过时字段名。
6. 已明确的默认值文档必须与代码一致（例如默认 Web 端口为 `8199`，不得在文档中写成其他端口）。
7. 新增配置项应在 `config.Config` 结构体中给出合理默认值（见 `GlobalConfig` 初始化），避免旧配置文件升级后行为突变。

---

## 前后端联动规范

1. 修改状态统计、阶段定义、字段语义时，必须同时检查：`core/handlers.go`、`core/admin.html`、README 对应说明。
2. 前端展示的“进度 / 总量 / 失败数 / 当前阶段”必须与后端实际语义一致（注意跨阶段统计不要重复累计）。
3. 新增/修改 API 时，保持鉴权中间件（`authMiddleware`）覆盖，注意 `X-API-Key` 与 `Authorization: Bearer` 两种入口。

---

## 变更管理规范

每次代码调整后，同步更新对应说明文件，确保文档与代码一致。

1. 文档更新要求：
   - 记录本次调整的目的、影响范围和关键变更。
   - 影响使用方式、配置或输出结果的，必须更新 `README.md`。
   - 阶段性修复可追加到现有专题说明文档。
2. 常见更新位置：
   - 功能、配置、行为变化 → `README.md`（仅入口级说明）
   - 详细 API 行为、动态订阅说明 → `API_CHANGES.md`
   - 功能调整 / 变更历史 → `FEATURE_CHANGES.md`
   - 常见问题 → `FAQ.md`
   - 开发约束更新 → 本文件
3. 记录格式建议：

   ```markdown
   ## [日期] 变更说明

   **调整内容**:
   1. 修复 xx
   2. 优化 xx

   **影响范围**:
   1. 模块 A
   2. 模块 B

   **注意事项**:
   1. 注意事项 A
   2. 注意事项 B
   ```

---

## 修改代码时的执行要求

1. 先搜索现有实现，再决定修改点，避免重复实现。
2. 只改与当前任务直接相关的内容，不顺手修 unrelated 问题。
3. 修改后做最小范围验证；有构建或测试命令时，先跑与改动最相关的检查。
4. 不做无意义的格式调整、导入重排或大规模重命名。
5. 每个重要阶段完成后执行一次 `go build ./...`，确保改动不破坏构建。
6. 编辑器报错与实际构建结果冲突时，以 `go build` / `go vet` 结果为准。

---

## Go 特定注意事项

1. **`goto` 与变量声明**：使用 `goto` 时，跳转目标之后用到的变量必须在 `goto` 之前声明，避免 `goto jumps over declaration` 编译错误（`pipeline.go` 中存在 `goto finished` 用法）。
2. **性能敏感路径**：避免不必要的字符串拼接、重复解析和重复分配；正则表达式尽量在循环外预编译。
3. **goroutine / channel 修改后**：优先排查死锁、提前关闭、阻塞发送和数据竞争。可用 `go build -race` / `go test -race` 辅助验证并发改动。
4. **循环内 `defer`**：警惕在长循环中 `defer` 释放资源，defer 只在函数返回时执行，易造成句柄/连接累积。
