# Changelog

本文件记录项目的所有重要变更，格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 规范。

## [Unreleased]

### Added
- 基于 gqlgen + chi 的 GraphQL API 服务框架
- StarRocks 数据源适配器（MySQL 协议，参数化查询，白名单校验）
- Prometheus 数据源适配器（HTTP API，即时查询和范围查询）
- 跨数据源并行查询与结果合并，部分失败隔离
- DataLoader 批量合并（per-request 实例，防止 N+1 查询）
- JWT 认证（HS256/RS256/ES256）和 API Key 认证（bcrypt 哈希）
- 按数据源和操作类型的细粒度授权
- 令牌桶限流（本地 + Redis 分布式 + 自动降级）
- 查询结果缓存（内存 LRU / Redis，穿透/雪崩/击穿三重防护）
- 熔断器（CLOSED/OPEN/HALF_OPEN 状态机）
- 指数退避重试（瞬时错误重试，业务错误立即返回）
- 后台指数退避重连
- Prometheus 指标端点（请求/数据源/缓存/错误指标，自定义标签）
- OpenTelemetry 链路追踪（Root/Resolver/DataSource/Redis Span 层级）
- W3C Trace Context 传播
- 结构化 JSON 日志（zap，trace_id 关联）
- 审计日志（独立输出）
- 敏感信息脱敏（正则规则）
- 认证失败暴力破解防护
- CSRF 防护、CORS、gzip 压缩、请求体大小限制
- 查询复杂度/深度限制、结果集截断
- 批量查询支持（按实际查询数限流）
- Relay 游标分页和传统 offset/limit 分页
- YAML 配置 + 环境变量覆盖（12-Factor）
- 配置热更新（日志级别、限流参数、缓存 TTL）
- 优雅关闭（有序资源释放）
- 健康检查（/health）和就绪探针（/ready）
- 多阶段 Dockerfile（distroless，非 root）
- Kubernetes 部署清单（Deployment, Service, ConfigMap, HPA）
- Docker Compose 集成测试环境
- GitHub Actions CI/CD 流水线
- 属性测试（rapid，96 个属性）
- 性能基准测试
