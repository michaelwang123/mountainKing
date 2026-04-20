# Contributing to GraphQL Multi-DataSource API

感谢你对本项目的关注！以下是参与贡献的指南�?

## 行为准则

参与本项目即表示你同意遵守我们的行为准则，营造友好、包容的社区环境�?

## 如何贡献

### 报告 Bug

1. �?Issues 中搜索是否已有相同问�?
2. 如果没有，创建新 Issue，包含：
   - 问题描述
   - 复现步骤
   - 期望行为 vs 实际行为
   - 环境信息（Go 版本、OS、配置）
   - 相关日志或错误信�?

### 提交功能建议

1. �?Issues 中创�?Feature Request
2. 描述使用场景和期望的功能
3. 等待维护者讨论和确认

### 提交代码

1. Fork 仓库
2. 创建特性分支：`git checkout -b feature/my-feature`
3. 编写代码和测�?
4. 确保通过所有检查：
   ```bash
   # Lint
   golangci-lint run ./...

   # 单元测试
   go test -race ./...

   # 覆盖率（目标 �?70%�?
   go test -race -coverprofile=coverage.out ./...
   ```
5. 提交 commit（遵�?Conventional Commits 规范�?
6. 推送到你的 Fork
7. 创建 Pull Request

### Commit 规范

```
<type>(<scope>): <description>

[optional body]
```

类型�?
- `feat` �?新功�?
- `fix` �?Bug 修复
- `docs` �?文档更新
- `refactor` �?代码重构
- `test` �?测试相关
- `chore` �?构建/工具�?

示例�?
```
feat(adapter): add ClickHouse datasource adapter
fix(cache): handle gob deserialization failure gracefully
docs(readme): update configuration reference
```

## 开发环�?

### 前置要求

- Go 1.25+
- golangci-lint
- Docker & Docker Compose（集成测试）

### 本地开�?

```bash
git clone https://github.com/michaelwang123/mountainKing.git
cd graphql-api
go mod download
go run cmd/server/main.go
```

### 代码生成

修改 `.graphql` Schema 文件后需要重新生成代码：

```bash
go run github.com/99designs/gqlgen generate
```

### 运行测试

```bash
# 全部测试
go test ./...

# 带竞态检�?
go test -race ./...

# 基准测试
go test -bench=. -benchmem ./internal/server/
```

## 代码规范

- 所有导出符号必须有 GoDoc 注释
- 使用 `fmt.Errorf` + `%w` 进行 error wrapping
- 核心组件通过接口解�?
- 属性测试使�?`pgregory.net/rapid`，每个属�?�?100 次迭�?

## 添加新数据源适配�?

详见 [数据源适配器扩展指南](official_document/datasource-adapters.md#扩展新数据源)�?

## Pull Request 检查清�?

- [ ] 代码通过 `golangci-lint run ./...`
- [ ] 所有测试通过 `go test -race ./...`
- [ ] 新功能包含单元测试和/或属性测�?
- [ ] 导出符号包含 GoDoc 注释
- [ ] 如涉及配置变更，更新 `config.yaml` 示例和配置文�?
- [ ] 如涉�?Schema 变更，已执行 `go generate`

## 许可�?

提交代码即表示你同意将代码以本项目的许可证发布�?
