# 功能调整/变更说明
## [2026-07-28] 修复大规模检测超时致大量节点未测 + 阶段3统计口径矛盾

**问题**（用户实测日志）：约 12 万节点检测，存活检测阶段 246 秒后整体超时，**仅测试 15708 个（87% 未测试）**，最终可用仅 324；且阶段3日志出现“成功(324) > 总数(218)”的矛盾。

**根因与修复**：
1. `config.Timeout` 默认值缺失（为 0）→ 存活检测的 HTTP client 无超时，失活节点要等系统 TCP 超时才失败，长时间占满 1000 个并发槽。现将默认值设为 `5000`（毫秒），失活节点快速失败。
2. `calcCheckTimeout` 用固定“每批 2 秒”估算总超时，对大规模节点严重偏低（12 万节点仅 246 秒）。改为按存活检测的**实际**单节点超时估算每批耗时：统一延迟模式（`unified-delay`）用 `warmup-timeout + test-timeout`，否则用 `config.timeout`；并对每批耗时做 [2s, 60s] 区间保护（避免 `timeout` 异常大值如 1000000 导致估算爆表）；总超时上限由 1800s 提高到 3600s。
3. 阶段3（媒体）日志“总数”错用 `speedSuccessTotal`（测速成功数），而媒体检测实际对所有存活成功节点执行，导致“成功 > 总数”。改用 `aliveSuccessTotal`，口径一致。

**影响范围**：
1. `config/config.go` - `GlobalConfig` 新增 `Timeout` 默认值 5000。
2. `checker/pipeline.go` - `calcCheckTimeout` 估算逻辑；阶段3统计口径（日志 2 处）。

**注意事项**：
1. `config.Timeout` 默认 5000ms 会让存活检测单节点最多等 5 秒（此前无超时）；若有响应较慢但可用的节点，可在配置文件中调大 `timeout`。
2. 媒体检测对所有存活成功节点执行（测速失败不淘汰节点），属“存活即可用”语义；阶段3统计现以存活成功数为分母。
3. `go build` / `go vet` / `gofmt` 均通过。

## [2026-07-23] 统一去重逻辑：拉取去重与入库去重共用 provider.ProxyKey

**背景**: 项目存在两套去重实现且标准不一致——`provider.DeduplicateProxies`（拉取批量去重，map O(N)）与 `output.proxyExists`（入库增量去重，线性 O(N²)）。前者为通用键、后者按协议精确判定，对“是否同一节点”可能给出不同结论。

**调整内容**:
1. 新增导出函数 `provider.ProxyKey(proxy)`：作为“同一节点”的统一判定标准，按协议类型选择关键字段生成去重键，融合原 `proxyExists` 的精度（vmess 的 alterId、ssr 的 protocol/obfs、trojan 的 sni、wireguard 的 public-key、mieru/ssh 的 username 等）；凭证别名字段（password↔auth/psk/private-key）取第一个非空值。
2. `provider.DeduplicateProxies` 改用 `ProxyKey`。
3. `output.InsertRecordsDedup` 改用 `proxyutils.ProxyKey` 的 map 去重（与拉取阶段同一标准），复杂度从 O(N²) 降为 O(N)；删除不再需要的 `proxyExists`。
4. `output` 包新增对底层 `provider` 包的依赖（provider 不反向依赖，无循环）。

**影响范围**:
1. `provider/process.go` - 新增 `ProxyKey`/`firstNonEmpty`，`DeduplicateProxies` 调用。
2. `output/db.go` - `InsertRecordsDedup` 改用统一键、删除 `proxyExists`、新增 import。
3. `provider/process_test.go` - 补充别名凭证同一性、ProxyKey 稳定性等测试。

**注意事项**:
1. 统一后两处“同一节点”判定一致；别名字段处理比旧 `proxyExists` 的 `||` 更准确（同凭证不同字段名视为同一）。
2. 入库去重性能提升（O(N²)→O(N)）。
3. `go build` / `go vet` / `go test ./provider/` / `gofmt` 均通过。

## [2026-07-23] 修复节点去重过度（去重键缺失协议类型与部分凭证字段）

**问题**: 用户实测 41 万节点去重后仅剩 2.4 万，去重率异常偏高。

**根因**: `provider/process.go` 的 `DeduplicateProxies` 去重键为 `server:port:servername:password(或uuid)`，存在两处缺陷：
1. 不含协议类型 `type` —— 同 server:port 的不同协议节点会碰撞；
2. 凭证仅取 `password`/`uuid` —— `hysteria`(auth)、`snell`(psk)、`wireguard`(private-key)、`ssh`(private-key) 等协议的凭证字段不同，其凭证部分为空，导致同一 server:port 上这类不同节点被合并成一个（叠加 servername 常为空，碰撞更严重）。

**调整内容**:
1. 新增 `proxyDedupKey`，去重键纳入 `type` + 按协议回退提取的多种凭证字段(`password`/`uuid`/`auth`/`auth-str`/`psk`/`private-key`/`key`/`token`，带字段名前缀避免跨字段同值碰撞) + `cipher`/`network`。
2. 新增 `provider/process_test.go` 单元测试：验证不同协议/凭证节点不再被误合并、真重复仍去重、同服务器不同密码保留、空 server 跳过；并含旧键碰撞的对比断言（4 个不同节点在旧键下被去重成 2 个）。

**影响范围**:
1. `provider/process.go` - `DeduplicateProxies` 去重键逻辑（新增 `proxyDedupKey`）。
2. `provider/process_test.go` - 新增去重单元测试。

**注意事项**:
1. 修复方向为“减少误合并、保留更多合法节点”，修复后去重结果会显著多于此前（更接近真实去重）。
2. 仍以 map 实现 O(N) 去重，大规模节点无性能回归。
3. `go test ./provider/`、`go build`、`go vet` 均通过。

## [2026-07-23] Web 面板精简：移除与阶段卡重复的详细统计长文本

**调整内容**:
1. 移除 `#detailStats` 详细统计长文本行：其展示的「存活/测速通过/媒体检测/阶段进度/阶段成功/阶段成功率」等，现已在上方各阶段步骤卡中结构化呈现，属重复信息，长串文本反而降低可读性。
2. 保留任务进度行（作为底部进度条的文字数值 label：任务进度 / 成功 / 失败 / 百分比），并将原长文本中独有的「预计剩余(ETA)」并入该行（`#etaText`）。

**影响范围**:
1. `core/admin.html` - 删除 `detailStatsText` 元素及 `updateProgressBar` 中的长文本拼接逻辑；ETA 改为独立 `#etaText` 展示；`updateStatus` 的两处重置逻辑同步从 `detailStatsText` 切换为 `etaText`。

**注意事项**:
1. 纯前端展示精简，无后端接口变化；进度条与阶段卡承载全部统计，信息不丢失（ETA 已保留）。
2. `go build` 与前端 JS 语法检查均通过。

## [2026-07-23] 流水线检测统计增强 + Web 面板显示优化 + 数据库磁盘回收

**调整内容**:
1. 媒体检测按平台细分统计：`ProgressTracker` 新增平台级计数（OpenAI/Netflix/Disney/YouTube/Gemini/TikTok），媒体检测完成后按节点结果累加；`GetDetailedStats` 输出 `mediaPlatform`。
2. 测速平均速度：`ProgressTracker` 累计成功测速节点的速度（`AddSpeedSample`），`GetDetailedStats` 输出 `avgSpeedKBps`。
3. ETA 预计剩余时间：新增 `GetETA()`，基于存活检测进度与已用时间估算整体剩余秒数；`/api/status` 输出 `eta`。
4. 数据库磁盘回收：新增 `DB.CompactDB()`（复用 `compactDBFile`，所有失败路径均重开连接），`app.go` 每 24 轮检测后触发一次，回收已删除历史批次占用的磁盘空间。
5. 订阅拉取并发优化：`proxyChan` 缓冲由 1 增大到 1024，减少 1000 并发下生产者阻塞。
6. Web 面板显示优化：测活卡显示实时存活率；测速卡显示平均速度（自动 KB/s↔MB/s）；媒体卡由冗余的“成功/失败”改为各平台解锁数细分展示；详情区新增“预计剩余”（ETA）。

**影响范围**:
1. `checker/progress.go` - 平台细分/速度累计字段、`AddSpeedSample`/`AddMediaResult`/`GetETA`、`GetDetailedStats` 扩展。
2. `checker/pipeline.go` - 测速与媒体检测后记录统计。
3. `core/handlers.go` - `/api/status` 输出 `eta`（`mediaPlatform`/`avgSpeedKBps` 随 `stageStats` 输出）。
4. `output/db.go` + `core/app.go` - `CompactDB` 与周期性触发。
5. `provider/fetch.go` - `proxyChan` 缓冲。
6. `core/admin.html` - 阶段卡与详情区展示增强、`formatSpeed`。

**注意事项**:
1. `CompactDB` 会短暂关闭/重开连接，期间并发的 API 读可能短暂返回错误（低频、快速）；触发间隔 `compactEveryNChecks=24` 可按需调整。
2. 媒体“成功/失败”原本 success=总数、failed=0 意义不大，已用平台细分替代，展示更直观。
3. ETA 为基于存活阶段进度的粗略估算，仅供参考。
4. `go build` / `go vet` / `gofmt` 及前端 JS 语法均通过。

## [2026-07-23] 修复数据库无限增长 + 订阅阶段节点数实时显示

**调整内容**:
1. 【关键·性能】修复 `output/db.go` `ReplaceCurrentBatch` 的数据库无限增长问题：此前每次检测仅把当前批（Batch=0）标记降级为上一批（Batch=-1）并插入新批次，**从不删除任何记录**，导致历史批次（全部为 -1）随每次检测无限累积。第 K 次检测后库中约有 K×N 条记录，使 `ReplaceCurrentBatch`/`loadProxyList`/`queryBatch` 的全量遍历越来越慢，检测周期与订阅拉取整体变慢并有超时风险。
   - 修复后：每次持久化只保留最近两批——删除更早的 previous、将 current 降级为 previous、写入新的 current；首次运行即自动清理历史膨胀。
   - 附带修正语义问题：此前 `QueryRecords` 在 current 为空时回退查询 `BatchPrevious(-1)` 会返回所有历史批次的混合数据，修复后仅返回真正的上一批。
2. 【功能·可观测性】订阅获取阶段实时显示已解析节点数：新增 `provider.SubsFetchNodeCount` 原子计数器，收集协程每追加一个节点即累加；`/api/status` 暴露 `subsFetchNodeCount`；Web 面板“订阅获取”步骤卡新增“节点”实时计数。此前节点总数需等全部订阅拉取完成、进入检测阶段才显示。

**影响范围**:
1. `output/db.go` - `ReplaceCurrentBatch` 批次清理逻辑。
2. `provider/fetch.go` - 新增 `SubsFetchNodeCount` 计数器及初始化/累加。
3. `core/handlers.go` - `/api/status` 新增 `subsFetchNodeCount` 字段。
4. `core/admin.html` - 订阅获取步骤卡显示实时节点数。

**注意事项**:
1. `ReplaceCurrentBatch` 修复不改变 `QueryRecords` 的 current→previous 回退语义，行为兼容；首次运行会删除全部历史批次记录（一次性较大删除事务，在检测完成后的持久化阶段执行，不影响订阅拉取阶段）。
2. bbolt 删除记录后文件大小不会自动收缩（空闲页由后续写入复用，文件规模趋于稳定、不再无限增长）；如需回收已膨胀的磁盘空间，可在停止服务后删除 `output/subs.db` 让其重建，或依赖现有 compact（当前仅在鉴权记录迁移时触发一次）。
3. `go build` / `go vet` / `gofmt` 均通过。

## [2026-07-23] 代码梳理：删除死代码、加固日志读取边界

**调整内容**:
1. 删除 `core/handlers.go` 中的死函数 `isProxyDuplicate`（约 164 行）：全项目无任何调用，是被 `output/db.go` 的 `proxyExists` 取代的遗留实现，且内部含与 `proxyExists` 相同的 hysteria/ssh 凭证别名 `&&` 判断问题；删除以消除重复实现与维护隐患。
2. `ReadLastNLines` 增加 `n<=0` 卫语句：避免 `make([]string, 负数)` 或 `count%0`（n==0）触发 panic。当前调用方 `getLogs` 传入固定值 100 不会触发，属函数契约健壮性加固。

**影响范围**:
1. `core/handlers.go` - 删除死代码、日志读取函数边界保护。

**注意事项**:
1. `isProxyDuplicate` 为包私有且无调用点，删除不影响编译与运行（`go build` / `go vet` 均通过）。实际去重逻辑由 `output/db.go` 的 `proxyExists` 承担（其别名判断已在本轮之前的修复中改为 `||`）。
2. 本轮审计另发现但未改动的既有情况：`updateConfig` 在加锁前写 `config.GlobalConfig.SubToken`（与鉴权/订阅读构成数据竞争，根因是 `GlobalConfig` 无统一锁，属架构级）；`getConfig` 在 API Key 校验后明文返回完整配置（含密钥，设计如此）；配置文件写入权限为 `0644`。以上留待评估，未在本轮改动。

## [2026-07-23] 前端安全加固：Web 面板 XSS 转义

**调整内容**:
1. 新增 `escapeHtml()` 辅助函数，`core/admin.html` 渲染动态内容前统一做 HTML 转义。
2. `colorizeLog()` 在着色前先对整行日志做 HTML 转义，修复日志渲染的存储型 XSS：日志包含节点名、订阅 URL、错误详情等外部可控数据，此前经 `innerHTML` 渲染时未转义，可被注入并在管理员查看面板时执行。
3. 黑名单表格渲染对 `key`、`failCount`、`lastScope`、封禁时间等字段做转义（纵深防御），避免 `innerHTML` 拼接被注入。

**影响范围**:
1. `core/admin.html` - 日志区与黑名单表格的动态内容渲染。

**注意事项**:
1. 转义在颜色着色之前进行；日志级别（INF/ERR/WRN/DBG）与时间戳均为 ASCII，不受转义影响，颜色标记正常。
2. 黑名单 `key` 为后端经 `net.ParseIP` 校验的 IP，本身已不含 HTML 危险字符；此处转义为纵深防御，防止未来来源变化。
3. 纯前端改动，不涉及后端接口与配置，`go build` / `go vet` 均通过。

## [2026-07-23] 安全优化：资源泄漏、并发竞争、密钥强度与去重修复

**调整内容**:
1. 修复 `provider/fetch.go` 订阅拉取四个阶段在重试循环内使用 `defer resp.Body.Close()` 的问题，改为读取后立即关闭，避免连接在整个 `GetDateFromSubs` 调用期间累积不释放。
2. `provider/fetch.go` 读取订阅响应体改用 `io.LimitReader` 限制最大 64MiB，防止恶意/故障订阅源返回超大内容导致 OOM。
3. `checker/pipeline.go` 全局进度追踪器由裸指针 `CurrentTracker` 改为 `atomic.Pointer[ProgressTracker]`（私有 `currentTracker`），并新增 `GetCurrentTracker()` 读取方法，消除检测 goroutine 写入与 `/api/status` 读取之间的数据竞争及潜在 nil panic。
4. `core/server.go` `GenerateSimpleKey` 由 6 位十进制数字（约百万量级）改为 `crypto/rand` 生成的 128 位随机值，提升自动生成 API Key 的强度。
5. `output/db.go` `proxyExists` 去重逻辑：`hysteria/hysteria2/hy2`、`anytls/snell`、`ssh` 分支的凭证别名比较由 `&&` 改为 `||`，与其余协议分支保持一致，修复“同服务器不同密码”节点被误判为重复而丢弃的问题。
6. `output/db.go` `CleanupAuthFailures` 改用“先收集 key 再统一删除”的延迟删除模式，避免在 bbolt cursor 遍历过程中 `Delete` 导致跳过相邻记录（与 `MigrateAuthRecords` 保持一致）。
7. `core/handlers.go` `/api/config/add` 对 `sub_url` 增加换行/回车字符校验，防止写入 `config.yaml` 时注入额外 YAML 行。
8. 将 `.copilot-instructions.md` 更名为 `AGENTS.md`，并补充项目技术栈、目录结构、构建命令等上下文。

**影响范围**:
1. `provider/fetch.go` - 四阶段 body 关闭与读取大小限制。
2. `checker/pipeline.go`、`core/handlers.go` - 进度追踪器改为原子指针及其读取点。
3. `core/server.go` - API Key 生成强度。
4. `output/db.go` - 节点去重逻辑与鉴权记录清理。
5. `core/handlers.go` - 订阅链接输入校验。
6. `AGENTS.md`（原 `.copilot-instructions.md`）。

**注意事项**:
1. 以上修复均不改变对外 API、配置项与输出格式，属行为兼容的健壮性/安全加固。
2. 去重逻辑改为 `||` 后，同 `server:port` 但凭证不同的节点将被保留（此前会被误删），可能使可用节点数略有增加，属预期修正。
3. 订阅响应体上限 64MiB 远高于正常订阅体积，不影响合法订阅。
4. `CurrentTracker` 已改为包内私有 `currentTracker`（`atomic.Pointer`），包外请通过 `checker.GetCurrentTracker()` 读取。

## [2026-05-25] 安全加固：订阅鉴权与 API 防护

**调整内容**:
1. 移除 `/all.yaml` 静态文件路由，订阅接口统一为 `/sub?token=...`，不再暴露固定文件路径。
2. 订阅接口 `/sub` 新增 token 校验：访问时必须携带 `?token=xxx` 查询参数，token 无效时返回 404。
3. 新增 DB 持久化鉴权失败追踪：使用 bbolt `auth` bucket 存储失败记录，同一 IP 连续失败 3 次后封禁 30 天。
4. API 鉴权中间件（`/api/*`）与订阅鉴权（`/sub`）均接入 DB 持久化封禁，封禁数据在服务重启后不丢失。
5. 所有鉴权失败统一返回 HTTP 404（非 401/JSON），避免泄露接口存在性和错误详情。
6. 新增 `/api/ui-info` 受保护端点：需 API Key 验证后返回真实订阅地址和配置文件路径。
7. Web 面板（`/admin`）默认显示占位文本"验证 API 密钥后显示"，前端在 API Key 验证成功后调用 `/api/ui-info` 获取真实信息。
8. 移除 `core/app.go` 中的 `allYamlMutex` 字段和 `output.SaveConfig(results)` 调用。
9. Web/API 节点写入改为通过 `db.InsertRecordsDedup()` 直接入库，不再写入静态文件。
10. `sub-token` 配置项：空值时自动生成随机 token；配置文件和文档中的订阅示例统一改为 `/sub?token=...` 格式。

**影响范围**:
1. `core/server.go` - 移除静态路由，新增 `authMiddleware`、`subscriptionURL`、`authFailureKey`、`GenerateSecureToken`、`GenerateSimpleKey`，封禁常量 `authMaxFailures=3`、`authBanDuration=30天`。
2. `core/handlers.go` - `/sub` 端点新增 token 校验和 DB 鉴权；新增 `getUIInfo`、`validSubToken`；`addMultipleNodesDirectly`/`addSingleNodeFromProxy` 改用 DB 写入。
3. `core/app.go` - 移除 `allYamlMutex` 和 `SaveConfig` 调用。
4. `output/db.go` - 新增 `IsAuthBlocked`、`RecordAuthFailure`、`ClearAuthFailure`、`CleanupAuthFailures` 方法。
5. `output/db_model.go` - 新增 `authBucketName` 常量和 `AuthFailureRecord` 结构体。
6. `core/admin.html` - 前端默认显示占位文本，API Key 验证后调用 `/api/ui-info` 获取真实地址。
7. `config/config.example.yaml` - 订阅示例改为 `/sub?token=...`，新增 `sub-token` 配置说明。
8. `README.md` - 订阅接口示例和说明更新。
9. `subs-check.sh` - 状态输出中的订阅地址示例更新。

**注意事项**:
1. token 比较使用 `crypto/subtle.ConstantTimeCompare`，防止时序攻击。
2. 同一地址连续 3 次鉴权失败后封禁 30 天，封禁数据持久化到 DB，重启不丢失。
3. 所有鉴权失败均返回 HTTP 404，不返回 401 或 JSON 错误信息，避免信息泄露。
4. 订阅地址和配置路径仅在 API Key 验证成功后才通过 `/api/ui-info` 返回，Web 面板默认不展示。
5. `sub-token` 为空时程序启动或保存配置时会自动生成随机 token 并写入配置文件。
6. 旧配置文件如仍使用 `/all.yaml` 作为订阅源地址，需要手动更新为 `/sub?token=...` 格式。

## [2026-04-10] 变更说明
**调整内容**:
1. 修复 Web 面板在点击“开始检测”后的订阅获取阶段，状态文本被上一轮检测残留阶段错误覆盖为“媒体检测”的问题。
2. 调整 `/api/status` 的阶段字段优先级：当仍处于订阅获取阶段时，`currentStageCode` 与 `currentStageName` 固定返回 `subscription` / `订阅获取`，不再被 `CurrentTracker` 的旧阶段覆盖。
3. 在每轮新检测启动前重置全局 `CurrentTracker`，避免上一轮检测遗留的阶段统计影响新任务初始展示。
**影响范围**:
1. `core/handlers.go` 的 `/api/status` 阶段字段合成逻辑。
2. `checker/pipeline.go` 的检测初始化流程。
**注意事项**:
1. 本次修复仅调整阶段展示与状态接口语义，不改变订阅获取、存活检测、测速、媒体检测的实际执行顺序。
2. 订阅获取阶段仍可继续返回上一轮 tracker 的 `detailStats` 作为附加统计，但扁平阶段字段会优先保持“订阅获取”语义一致。

## [2026-03-30] 变更说明
**调整内容**:
1. 为 Web 面板进度区域新增“订阅获取 / 测活 / 测速 / 媒体”分阶段步骤条展示。
2. 复用现有 `/api/status` 返回的 `currentStageCode`、`currentStageName` 与 `detailStats` 字段驱动前端状态，不新增后端协议。
3. 当前执行阶段会高亮显示，已完成阶段显示为已完成，当前阶段会同步显示进行中文案或阶段进度。
4. 进一步细化步骤条副标题：已完成阶段显示“通过数 / 完成数”，当前阶段显示当前进度与通过数，便于直接观察各阶段产出。
5. 当任务进入 `stopping` 状态时，步骤条中的当前阶段会切换为停止态样式，并直接显示“停止中”。
6. 为检测流水线发送协程补充 `context cancel` 响应，避免阶段超时或停止后发送方阻塞在阶段通道写入上，降低 goroutine 泄漏风险。
7. 修复“仅媒体检测”场景下 `resultChan` 的关闭归属，避免媒体 worker 尚未退出时结果通道被前置收尾协程提前关闭。
8. 为 `ForceClose` 增加统一的阶段取消桥接：检测到强制停止标志后，会主动取消阶段1与阶段2/3的上下文，缩短停止信号传递路径。
**影响范围**:
1. `core/admin.html` 的进度区域展示与状态轮询更新逻辑。
2. Web 面板中的阶段信息展示语义，从单一文本补充为步骤化状态展示。
3. `checker/pipeline.go` 中阶段1与阶段2/3的任务发送边界。
4. `checker/pipeline.go` 中仅媒体检测路径的结果通道收尾逻辑。
5. `checker/pipeline.go` 中强制停止到阶段 `context` 的传播路径。
**注意事项**:
1. 本次仅增强前端展示，不修改检测流水线和阶段统计语义。
2. 若当前未在检测中或状态获取失败，步骤条会回退为“等待中”状态。
3. 停止态仅反映前端运行状态展示，不改变后端阶段推进与统计口径。
4. 本次生命周期修复仅针对发送协程的取消响应，不改变各阶段 worker 的成功/失败统计口径。
5. 本次收尾修复仅调整通道关闭时机，不改变结果内容与阶段统计口径。
6. 本次停止路径修复主要提升取消传播一致性；同时统一 `/api/force-close` 返回文案为“已强制停止”，避免与“退出程序”语义混淆。

## [2026-03-29] 变更说明
**调整内容**:
1. 为 Web 运行态新增 `stopping` 语义，用于区分“空闲 / 检测中 / 停止中”。
2. 扩展状态接口扁平字段，新增 `statusText`、`currentStageCode`、`currentStageName`，便于前端直接展示当前阶段。
3. Web 面板状态栏新增当前阶段显示，检测过程中可直接看到订阅获取、存活检测、测速检测、媒体检测等阶段信息。
4. 将前端“强制关闭”操作文案调整为“强制停止”，避免与“立即退出程序”语义混淆。
5. 补齐信号处理逻辑，`SIGINT/SIGTERM` 第一次触发强制停止当前检测，第二次立即退出程序。
**影响范围**:
1. `core/app.go` 的运行态标志与停止状态维护。
2. `core/handlers.go` 的 `/api/status` 与 `/api/force-close` 输出语义。
3. `core/admin.html` 的状态栏展示和强制停止交互文案。
4. `util/common.go` 的进程信号处理路径。
**注意事项**:
1. `stopping=true` 表示当前任务进入停止流程，不等同于进程已经退出。
2. `currentStageName` 优先反映当前真实检测阶段；若处于订阅获取阶段，则会显示为“订阅获取”。
3. 强制停止会尽量保留已完成阶段结果，但不承诺未完成节点一定写入最终结果。

## [2026-03-23] 变更说明
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
20. 修复任务进度顶部“成功”统计，优先显示当前阶段成功数而不是最终可用数。
21. 移除旧的 `min-speed` 配置与相关筛选逻辑，统一改为基于动态配置和接口参数控制。
22. 移除旧的全局 `concurrent` 配置，统一使用 `concurrent-stage` 管理各阶段并发。
**影响范围**:
1. `checker/pipeline.go` 的阶段2-3流水线调度、测速保护与取消响应。
2. `checker/worker.go` 的测速/媒体检测统计、结果投递与取消处理。
3. `checker/progress.go` 的阶段进度信息输出。
4. `checker/alive.go`、`checker/speed.go`、`checker/media.go`、`checker/ai.go` 的请求上下文传递。
5. `provider/info.go`、`provider/process.go` 的 IP 查询、重命名规则与命名输出。
6. `core/handlers.go` 的状态接口与检测阶段统计输出。
7. `core/admin.html` 的 Web 面板阶段展示、总进度展示与任务成功统计。
8. `config/config.go` 的测速并发计算与阶段并发读取逻辑。
9. `config/config.example.yaml`、`README.md`、`API_CHANGES.md` 的配置和说明文档。
**注意事项**:
1. 若链路带宽有限但测速并发过高，仍可能导致测速结果失真。
2. 建议优先配置 `network` 或显式设置 `concurrent-stage.speed`，并结合 `total-speed-limit` 一起使用。
3. Web 面板中的“阶段进度”现在显示为 `已完成/当前阶段总量`，比单纯显示累计完成数更直观。
4. Web 面板中的“任务进度”会优先显示当前阶段进度，总进度百分比仍保留为整体加权进度。
5. 状态接口中的 `failed` 现在只统计已明确失败的存活/测速节点，不再把媒体处理中节点计入失败。
6. 现在状态接口中的 `stageStats.alive/speed/media` 会额外返回 `successRate` 与 `timeoutRate`。
7. 若需要在节点名中显示 IP 纯净度，请确保 `platforms` 中启用了 `iprisk`。
8. 旧版 `min-speed` 与旧版 `concurrent` 即使仍保留在历史配置文件中，也不再作为主流程生效配置。
## [2026-02-11] 变更说明
**调整内容**:
1. 修复媒体计数重复累计的问题，确保媒体检测数量不再大于测速成功数量。
2. 修复测速统计重复输出的问题，避免每个 worker 都打印一份统计日志。
3. 将测速阶段统计改为全局原子计数汇总，由流水线统一输出一次完整统计。
**影响范围**:
1. `checker/worker.go` 中的测速阶段局部统计逻辑被移除，改为全局原子计数。
2. `checker/worker.go` 中媒体阶段重复计数调用被删除。
3. `checker/pipeline.go` 在测速阶段完成后统一输出汇总日志。
**注意事项**:
1. 修复后媒体检测数量应满足：媒体检测数 ≤ 测速成功数 ≤ 存活数。
2. 测速统计日志现在只输出一次，便于观察整体分布。
3. 统计逻辑依赖 `atomic` 计数器，多 goroutine 场景下结果更稳定。
## [2025-12-26] 变更说明
**调整内容**:
1. 为 `/api/addConfig` 新增可选参数 `test`，支持在添加订阅链接或单个节点前先做检测。
2. 新增统一的检测添加流程，对节点执行存活检测与速度测试，仅将通过检测的节点加入输出配置。
3. 为检测添加流程补充 120 秒总超时控制、并发控制、互斥写入和结果统计。
4. 扩展接口响应格式，返回检测节点数、通过数、失败数、添加数、耗时与超时状态。
**影响范围**:
1. `core/handlers.go` 中 `addConfig` 的请求结构和处理流程。
2. 新增 `testAndAddNodes`、`addSingleNodeFromProxy`、`parseSubscriptionNodes` 等辅助逻辑。
3. `/api/addConfig` 的响应内容在 `test=true` 时会返回更详细的检测统计。
4. `API_CHANGES.md` 等文档补充了接口说明和使用示例。
**注意事项**:
1. `test` 默认为 `false`，不传时保持原有直接添加行为。
2. 大量节点检测可能耗时较长，超时后会返回已完成部分结果。
3. 并发检测会占用系统资源，建议结合 `concurrent-stage` 与网络环境调整配置。
4. 订阅链接只有在至少 1 个节点通过检测时才会被加入 `sub-urls`。
5. 节点去重仍会在实际写入时执行，`passed_nodes` 与 `added_nodes` 可能不同。
## [2025-12-25] 变更说明
**调整内容**:
1. 重构项目目录结构，将原来的二级嵌套目录改为更扁平的一层目录布局。
2. 合并和拆分核心模块，形成 `core`、`checker`、`provider`、`output`、`util`、`monitor`、`config` 等更清晰的职责边界。
3. 合并平台检测与代理处理相关文件，减少重复分散的文件组织方式。
4. 将日志初始化能力独立到工具模块，提升维护性。
5. 移除已失效的 `save-method` 配置，保存方式固定为本地文件输出。
6. 出于安全考虑移除 `callback-script` 功能，避免执行用户自定义脚本带来的风险。
7. 修复远程订阅失败达到阈值时仍尝试从本地配置删除的问题，区分本地订阅与远程订阅的失败处理策略。
**影响范围**:
1. 项目整体目录结构、包名和模块边界。
2. `core`、`checker`、`provider`、`output`、`util`、`monitor` 等目录下的文件归属和职责划分。
3. 订阅失败处理流程，尤其是 `sub-urls` 与 `sub-urls-remote` 的差异化处理。
4. 配置项兼容性，旧配置中的 `save-method`、`callback-script` 会被忽略或不再生效。
5. 文档和实现说明，包括重构说明、功能对比和 API 调整说明。
**注意事项**:
1. 若有二次开发或外部代码依赖旧包路径，需要同步更新导入路径与调用位置。
2. `GetProxies()` 等函数在重构后若签名发生变化，调用方需要同步适配。
3. 远程订阅失败达到阈值后只会记录和告警，不会从本地配置删除。
4. 如需在检测完成后执行外部动作，建议改用通知机制而不是脚本回调。
5. 旧配置文件即使仍包含已移除字段，程序也会尽量兼容忽略，但建议逐步清理。
