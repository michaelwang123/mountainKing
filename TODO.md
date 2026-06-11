# MountainKing 项目 Backlog

> 最后更新：2026-06-11 | 当前版本：v0.1.0

## ✅ 已完成

- [x] **Docker 镜像发布** — GHCR 多架构自动发布 + 健康验证 + dev/nightly 工作流
- [x] **.dockerignore 优化** — 构建上下文 <5MB

## 🟡 中优先级

- [ ] **Branch protection rules** — 需在 GitHub Settings > Branches 手动设置：require PR review + require status checks (CI test, docs-test)

## 🟢 低优先级

- [ ] **Playwright E2E 测试** — 跨浏览器视觉回归测试。需要时再添加。
- [ ] **Lighthouse CI** — 自动化性能/无障碍/SEO 评分，集成到 CI 流程。需要时再添加。
- [ ] **GitHub Discussions 启用** — 在 GitHub Settings > General > Features 中开启。需要时手动启用。
- [ ] **自定义域名** — 暂不需要，当前使用 `michaelwang123.github.io/mountainKing/` 即可。
