# MountainKing 产品体验优化路线图

> 目标：让用户顺滑地从「第一次接触」到「生产部署」，每个阶段都能快速获得价值反馈。

---

## 核心洞察

MountainKing 是一个「配置驱动」的中间件产品。用户旅程的关键卡点在 **首次配置** 和 **调试反馈**。用户面对 322 行 config.yaml 时，如果不能快速看到结果，流失概率很高。

---

## 一、「5 分钟可用」体验（P0 — 最高优先级）

**现状问题**：用户 clone 后面对 config.yaml + .env.example，不知道最小可用配置是什么。

### 1.1 `make dev` 一键启动

提供开发模式一键启动命令，自动使用 development mode（auth=none, introspection=true），内嵌 mock 数据源，让用户 clone 后 30 秒内看到 GraphQL Playground。

### 1.2 Development Mode 自动跳过外部依赖

当 `GRAPHQL_SERVER_MODE=development` 时，自动跳过需要外部依赖的配置（Redis、StarRocks），使用内存替代。数据源连接失败时不 panic，而是标记为 unavailable 并继续启动。

### 1.3 配置向导 CLI

增加 `mountainking init` 子命令，交互式生成最小 config.yaml：

```
$ mountainking init
> 选择认证方式: [none/jwt/apikey]
> 数据源类型: [starrocks/prometheus]
> 连接地址: ...
✅ 已生成 config.yaml
```

---

## 二、错误反馈的「人性化」（P0）

### 2.1 启动时配置校验 + 修复建议

```
❌ Config Error: datasources[0].connection.host is empty
💡 Fix: Set SR_DB_HOST environment variable or edit config.yaml
📖 See: https://michaelwang123.github.io/mountainKing/doc.html#configuration
```

### 2.2 `/health` 端点返回详细依赖状态

在 development mode 下返回详细连接诊断，告诉用户哪个组件 ready、哪个还没连上。

### 2.3 GraphQL 错误消息包含 error code + 文档链接

每个错误响应携带对应 code 和 URL，用户可以直接点击查看解决方案。

---

## 三、渐进式学习路径（P1）

### 3.1 README 中的用户分层入口

- 「我想试试」→ Quick Start（docker-compose 一键启动 + 示例查询）
- 「我想接入」→ Getting Started（配置自己的数据源 + 认证）
- 「我想深入」→ Course（12 章渐进式教程）
- 「我想贡献」→ Developer Guide

### 3.2 内置示例查询集

在 development mode 的 Playground 中预置 3-5 个示例查询（简单查询、分页、模板查询、跨源查询），用户打开即可执行。

### 3.3 `docker-compose.dev.yaml` 包含完整可演示环境

加一个 `init-data` service，启动时自动灌入几百条样本数据，让用户有真实数据可查询。

---

## 四、可观测性即产品力（P2）

### 4.1 内置 `/debug` 页面（仅 development mode）

展示当前连接池状态、熔断器状态、缓存命中率、限流计数器的实时数值。

### 4.2 配置热更新反馈

热更新成功时在日志中输出：`[config-reload] rate_limit.requests_per_window: 100 → 200`。

---

## 五、部署体验打磨（P2）

### 5.1 Helm Chart

生产用户习惯 `helm install mountainking ./deploy/helm --set datasources.starrocks.host=...`。

### 5.2 二进制自检

`mountainking version` 和 `mountainking doctor`（自检环境依赖）。

### 5.3 `.dockerignore`

将 Docker build context 从 91MB 降到 <5MB，CI 构建提速 10 倍。

---

## 优先级总览

| 优先级 | 动作 | 预期效果 |
|--------|------|----------|
| P0 | `make dev` / `docker-compose up` 一键体验 | 首次使用从 30min 降到 5min |
| P0 | 启动时配置校验 + 修复建议 | 减少 80% 配置问题 |
| P1 | Playground 预置示例查询 | 无需读文档即可体验核心功能 |
| P1 | README 分层入口 | 不同角色快速找到信息 |
| P2 | `mountainking init` CLI 向导 | 消除配置焦虑 |
| P2 | Helm Chart + `.dockerignore` | 生产部署体验提升 |

---

## 核心理念

**让用户在「动手之前」就看到价值。** Development mode + 内置 mock + 预置示例，是让用户先「哇一下」再决定是否深入投入的关键。
