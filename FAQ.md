# 常见问题
## 1. 程序在测速阶段异常终止？
可能原因：
- 内存不足
- 并发过高
- 个别节点触发异常
建议：
- 启用软内存限制：`SUB_CHECK_MEM_SOFT_LIMIT=2GB`
- 降低 `concurrent-stage`
- 开启内存监控：`SUB_CHECK_MEM_MONITOR=1`
## 2. 节点检测速度太慢？
建议：
- 提高合适的并发数
- 减少或关闭媒体检测
- 更换更稳定的测速地址
- 使用 `success-limit` 控制输出规模
## 3. 某些订阅无法获取？
建议：
- 检查订阅链接是否有效
- 配置 `sub-urls-retry-failed`
- 查看 `sub_failure_record.txt`
- 查看运行日志定位错误
## 4. 如何通过 API 添加订阅链接？
使用 `POST /api/config/add`，详见 [API_CHANGES.md](API_CHANGES.md)。
## 5. 智能失败处理如何避免误删订阅？
核心机制是失败计数、成功重置以及本地/远程订阅分级处理，详见 [API_CHANGES.md](API_CHANGES.md)。
## 6. API 密钥在哪里查看？
可在启动日志中查看自动生成的密钥，或在配置文件中通过 `api-key` 自定义，也支持环境变量 `API_KEY`。
## 7. Web 服务无法访问？
建议检查：
- 端口是否被占用
- 防火墙是否放行
- `listen-port` 是否正确
## 8. 配置文件修改后不生效？
项目支持热重载；若未生效，请检查：
- YAML 语法是否正确
- 日志中是否有报错
- 是否修改了需要重启才能生效的配置项
## 9. 如何限制测速流量？
使用 `total-speed-limit`，具体写法见 [config/config.example.yaml](config/config.example.yaml)。
## 10. 节点去重不起作用？
去重基于连接信息而不是节点名称；名称不同但连接信息相同的节点仍会被识别为重复节点。
