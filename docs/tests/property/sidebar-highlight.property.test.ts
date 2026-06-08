import { describe, it, expect, beforeEach } from 'vitest';
import fc from 'fast-check';

/**
 * Feature: github-pages-docs, Property 3: Sidebar current document highlight
 *
 * For any valid file parameter value that matches a sidebar link's `data-file` attribute,
 * the Doc Viewer SHALL apply the `.active` class to exactly that one sidebar link and
 * remove `.active` from all other sidebar links.
 *
 * **Validates: Requirements 8.4**
 */

// Valid sidebar file values matching the doc.html sidebar structure
const SIDEBAR_FILES = [
  'getting-started',
  'architecture',
  'configuration',
  'development-mode',
  'graphql-api',
  'sql-template-engine',
  'datasource-adapters',
  'security',
  'observability',
  'deployment',
  'performance',
  'developer-guide',
  'error-reference',
  'troubleshooting',
  'migration-guide',
  'faq',
];

/**
 * Pure function: highlight the active sidebar link.
 * Mirrors the implementation in doc.html.
 */
function highlightActiveLink(file: string): void {
  document.querySelectorAll('.sidebar-link').forEach((link) => {
    link.classList.remove('active');
    if ((link as HTMLElement).dataset.file === file) {
      link.classList.add('active');
    }
  });
}

/**
 * Sets up a DOM sidebar structure matching doc.html's sidebar.
 */
function setupSidebarDOM(): void {
  document.body.innerHTML = `
    <aside id="sidebar">
      <div class="mb-6">
        <h3>入门</h3>
        <a href="doc.html?file=getting-started" class="sidebar-link" data-file="getting-started">快速开始</a>
        <a href="doc.html?file=architecture" class="sidebar-link" data-file="architecture">架构概览</a>
        <a href="doc.html?file=configuration" class="sidebar-link" data-file="configuration">配置参考</a>
        <a href="doc.html?file=development-mode" class="sidebar-link" data-file="development-mode">开发模式指南</a>
      </div>
      <div class="mb-6">
        <h3>核心功能</h3>
        <a href="doc.html?file=graphql-api" class="sidebar-link" data-file="graphql-api">GraphQL API 参考</a>
        <a href="doc.html?file=sql-template-engine" class="sidebar-link" data-file="sql-template-engine">SQL 模板引擎</a>
        <a href="doc.html?file=datasource-adapters" class="sidebar-link" data-file="datasource-adapters">数据源适配器</a>
      </div>
      <div class="mb-6">
        <h3>运维</h3>
        <a href="doc.html?file=security" class="sidebar-link" data-file="security">安全指南</a>
        <a href="doc.html?file=observability" class="sidebar-link" data-file="observability">可观测性</a>
        <a href="doc.html?file=deployment" class="sidebar-link" data-file="deployment">部署指南</a>
        <a href="doc.html?file=performance" class="sidebar-link" data-file="performance">性能调优</a>
      </div>
      <div class="mb-6">
        <h3>参考</h3>
        <a href="doc.html?file=developer-guide" class="sidebar-link" data-file="developer-guide">开发者指南</a>
        <a href="doc.html?file=error-reference" class="sidebar-link" data-file="error-reference">错误码参考</a>
        <a href="doc.html?file=troubleshooting" class="sidebar-link" data-file="troubleshooting">故障排查</a>
        <a href="doc.html?file=migration-guide" class="sidebar-link" data-file="migration-guide">迁移指南</a>
        <a href="doc.html?file=faq" class="sidebar-link" data-file="faq">FAQ</a>
      </div>
    </aside>
  `;
}

describe('Feature: github-pages-docs, Property 3: Sidebar current document highlight', () => {
  beforeEach(() => {
    setupSidebarDOM();
  });

  it('exactly one sidebar link gets .active class for any valid file value', () => {
    fc.assert(
      fc.property(
        fc.constantFrom(...SIDEBAR_FILES),
        (file: string) => {
          // Reset any prior state
          document.querySelectorAll('.sidebar-link').forEach((link) => {
            link.classList.remove('active');
          });

          // Execute highlight logic
          highlightActiveLink(file);

          // Collect all sidebar links
          const allLinks = document.querySelectorAll('.sidebar-link');
          const activeLinks = document.querySelectorAll('.sidebar-link.active');

          // Property: exactly one link should have the .active class
          expect(activeLinks.length).toBe(1);

          // Property: the active link must be the one whose data-file matches the file parameter
          const activeLink = activeLinks[0] as HTMLElement;
          expect(activeLink.dataset.file).toBe(file);

          // Property: no other links should have .active class
          allLinks.forEach((link) => {
            const el = link as HTMLElement;
            if (el.dataset.file !== file) {
              expect(el.classList.contains('active')).toBe(false);
            }
          });
        }
      ),
      { numRuns: 100 }
    );
  });

  it('previously active link loses .active class when a new file is selected', () => {
    fc.assert(
      fc.property(
        fc.constantFrom(...SIDEBAR_FILES),
        fc.constantFrom(...SIDEBAR_FILES),
        (firstFile: string, secondFile: string) => {
          // Highlight the first file
          highlightActiveLink(firstFile);

          // Highlight the second file
          highlightActiveLink(secondFile);

          // Only the second file should be active now
          const activeLinks = document.querySelectorAll('.sidebar-link.active');
          expect(activeLinks.length).toBe(1);

          const activeLink = activeLinks[0] as HTMLElement;
          expect(activeLink.dataset.file).toBe(secondFile);
        }
      ),
      { numRuns: 100 }
    );
  });
});
