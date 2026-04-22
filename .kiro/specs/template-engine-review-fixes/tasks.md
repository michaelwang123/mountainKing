# 实现计划：SQL 模板引擎代码审查修复

## 概述

修复 SQL 模板查询引擎代码审查发现的 7 个缺陷（1 Critical、3 High、3 Medium）。所有修复均为局部变更，不改变公共 API 签名。按优先级排序：Critical → High → Medium。

## Tasks

- [x] 1. C1 — 集成 TemplateWatcher 到 main.go（Critical）
  - [x] 1.1 在 cmd/server/main.go 中 TemplateEngine 创建成功后，添加 TemplateWatcher 创建和启动逻辑：构建 watchDirs（base_dir + shared_dir），调用 NewTemplateWatcher，调用 Start
  - [x] 1.2 在优雅关闭流程中，在 TemplateEngine.Close 之前添加 TemplateWatcher.Stop 调用

- [x] 2. C2 — 修复 sanitizer 反斜杠序列处理（High/安全）
  - [x] 2.1 在 sanitizer.go stateInSingleQuote 分支中，在 \' 检查之前添加 \\\\ 检查
  - [x] 2.2 编写单元测试验证修复：测试 \\\\\' 正确关闭字符串、检测注入

- [x] 3. C3 — 修复 Reload 缓存清除范围（High）
  - [x] 3.1 将 ClearByDatasource(ctx, "") 替换为 ClearByDatasource(ctx, "template:"+name)，删除死代码

- [x] 4. C4+C7 — 实现 RenderGoroutineLeaks 指标追踪（High）
  - [x] 4.1 将 render 包级函数改为 TemplateEngine 方法，更新 engine.go 调用点
  - [x] 4.2 在超时分支中递增 RenderGoroutineLeaks，启动 goroutine 等待泄漏完成后递减

- [x] 5. C5 — 修复 Reload SuccessCount 计算（Medium）
  - [x] 5.1 在 loadAll 返回后、错误隔离合并之前保存 freshSuccessCount，用于最终 result.SuccessCount

- [x] 6. C6 — 修复 ListTemplates 返回顺序（Medium）
  - [x] 6.1 在 registry.go GetAll 方法中按 Name 升序排序

- [x] 7. 回归验证
  - [x] 7.1 运行 go build 和 go vet 确保编译通过
  - [x] 7.2 运行模板引擎相关测试确保无回归
