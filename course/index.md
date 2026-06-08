# mountainKing 系统课程

> 从零到生产：全面掌握 Go GraphQL 多数据源 API 服务的设计、开发与运维。

## 课程概览

本课程体系围绕 mountainKing 项目展开，共 12 个模块，覆盖架构设计、核心功能、安全、可观测性、弹性、部署和性能调优等方面。每个模块既可独立学习，也可按顺序系统推进。

## 前置要求

- Go 1.25+ 基础
- GraphQL 基本概念
- SQL 基础（StarRocks 为 MySQL 兼容协议）
- Docker / Kubernetes 基本操作（部署模块）

## 课程目录

| 模块 | 标题 | 内容概要 |
|------|------|----------|
| 01 | [项目概览与架构设计](01-project-overview.md) | 项目定位、分层架构、请求处理流程、技术栈选型 |
| 02 | [环境搭建与快速上手](02-quick-start.md) | 本地开发环境、配置文件、第一个查询、开发模式 |
| 03 | [GraphQL Schema 深入解析](03-graphql-schema.md) | Schema 模块化设计、类型系统、分页模型、代码生成 |
| 04 | [数据源适配器详解](04-datasource-adapters.md) | DataSource 接口、StarRocks/Prometheus 适配器、白名单、扩展新数据源 |
| 05 | [SQL 模板引擎实战](05-sql-template-engine.md) | 模板语法、安全函数、参数校验、分页、缓存、热加载 |
| 06 | [安全体系详解](06-security.md) | JWT/API Key 认证、授权模型、CSRF/CORS、暴力破解防护、输入校验 |
| 07 | [缓存策略与实践](07-caching.md) | 缓存层架构、穿透/雪崩/击穿防护、singleflight、TTL 策略 |
| 08 | [可观测性体系](08-observability.md) | Prometheus 指标、OpenTelemetry 链路追踪、结构化日志、审计日志 |
| 09 | [弹性设计模式](09-resilience.md) | 熔断器、指数退避重试、限流、信号量并发控制、优雅关闭 |
| 10 | [部署与运维](10-deployment.md) | Docker 构建、Kubernetes 部署、CI/CD、Grafana 监控、告警规则 |
| 11 | [性能调优指南](11-performance.md) | 连接池、DataLoader、查询优化、缓存命中率、负载测试 |
| 12 | [高级主题与最佳实践](12-advanced-topics.md) | 中间件链、配置热更新、批量查询、错误处理体系、扩展开发 |

## 附录

| 资源　　　　　　　　　　　　　　　　　　　　　| 说明　　　　　　　　　　　　 |
| -----------------------------------------------| ------------------------------|
| [GraphQL 查询手册](graphql-query-cookbook.md) | StarRocks 单表查询实战示例集 |
| [官方文档](../official_document/README.md)　　| 完整的项目参考文档　　　　　 |
| [CHANGELOG](../CHANGELOG.md)　　　　　　　　　| 项目变更记录　　　　　　　　 |

## 学习路径建议

**快速入门（1-2 小时）：** 模块 01 → 02 → 查询手册

**核心开发（1 天）：** 模块 01 → 02 → 03 → 04 → 05

**生产就绪（2-3 天）：** 全部 12 个模块按顺序学习

**按需查阅：** 根据实际需求直接跳转到对应模块
