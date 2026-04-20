# GraphQL Multi-DataSource API 项目文档

欢迎阅读 GraphQL Multi-DataSource API 项目的官方文档。本文档集参考 Apache 开源项目文档风格编写，旨在帮助开发者、运维人员和贡献者全面了解本项目。

## 文档目录

| 文档 | 说明 | 适用读者 |
|------|------|----------|
| [架构概览](architecture.md) | 系统整体架构、组件关系、请求处理流程 | 架构师、开发者 |
| [快速开始](getting-started.md) | 环境准备、编译运行、首次查询 | 所有用户 |
| [配置参考](configuration.md) | 完整配置项说明、环境变量覆盖、热更新 | 运维人员、开发者 |
| [GraphQL API 参考](graphql-api.md) | Schema 定义、查询示例、分页与过滤 | 客户端开发者 |
| [安全指南](security.md) | 认证授权、限流、输入校验、脱敏 | 安全工程师、运维人员 |
| [数据源适配器](datasource-adapters.md) | StarRocks/Prometheus 适配器详解与扩展指南 | 开发者 |
| [可观测性](observability.md) | Prometheus 指标、OpenTelemetry 链路追踪、结构化日志 | 运维人员、SRE |
| [部署指南](deployment.md) | Docker、Kubernetes 部署、CI/CD 流水线 | DevOps、运维人员 |
| [性能调优](performance.md) | 缓存策略、连接池、熔断器、基准测试 | 开发者、运维人员 |
| [开发者指南](developer-guide.md) | 项目结构、代码规范、测试策略、贡献流程 | 贡献者、开发者 |

## 项目简介

GraphQL Multi-DataSource API 是一个基于 Go 语言和 gqlgen 框架构建的高性能 GraphQL API 服务。它作为只读查询网关，统一接入 StarRocks（OLAP 分析型数据库）和 Prometheus（时序数据库）等多种数据源，通过 GraphQL 协议向客户端提供灵活的数据查询能力。

## 许可证

详见项目根目录 [LICENSE](../LICENSE) 文件。
