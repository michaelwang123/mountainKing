# MountainKing 项目 Backlog

> 最后更新：2026-06-09 | 当前版本：v0.1.0

## 🟡 中优先级

- [ ] **Branch protection rules** — 需在 GitHub Settings > Branches 手动设置：require PR review + require status checks (CI test, docs-test)

## 🟢 低优先级

- [ ] **Playwright E2E 测试** — 跨浏览器视觉回归测试。需要时再添加。
- [ ] **Lighthouse CI** — 自动化性能/无障碍/SEO 评分，集成到 CI 流程。需要时再添加。
- [ ] **GitHub Discussions 启用** — 在 GitHub Settings > General > Features 中开启。需要时手动启用。
- [ ] **Docker 镜像发布验证** — `release.yml` 已有 Docker 构建逻辑，下次打 tag 时验证 GHCR 镜像发布是否正常工作。
- [ ] **自定义域名** — 暂不需要，当前使用 `michaelwang123.github.io/mountainKing/` 即可。
