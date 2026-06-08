import { describe, it, expect } from 'vitest';
import fc from 'fast-check';

/**
 * Feature: github-pages-docs, Property 2: Chapter navigation boundary behavior
 *
 * Validates: Requirements 3.5
 *
 * For any chapter number N where 1 ≤ N ≤ 12, the Course Page's prev/next navigation
 * SHALL disable the "previous" button if and only if N = 1, and SHALL disable the
 * "next" button if and only if N = 12.
 */

const chapters = [
  { id: "01-project-overview", number: "01", title: "项目概览与架构设计" },
  { id: "02-quick-start", number: "02", title: "环境搭建与快速上手" },
  { id: "03-graphql-schema", number: "03", title: "GraphQL Schema 深入解析" },
  { id: "04-datasource-adapters", number: "04", title: "数据源适配器详解" },
  { id: "05-sql-template-engine", number: "05", title: "SQL 模板引擎实战" },
  { id: "06-security", number: "06", title: "安全体系详解" },
  { id: "07-caching", number: "07", title: "缓存策略与实践" },
  { id: "08-observability", number: "08", title: "可观测性体系" },
  { id: "09-resilience", number: "09", title: "弹性设计模式" },
  { id: "10-deployment", number: "10", title: "部署与运维" },
  { id: "11-performance", number: "11", title: "性能调优指南" },
  { id: "12-advanced-topics", number: "12", title: "高级主题与最佳实践" }
];

/**
 * Pure function: compute prev/next navigation state.
 * Mirrors the getNavState implementation in course.html.
 */
function getNavState(currentIndex: number, totalChapters: number) {
  return {
    prevDisabled: currentIndex <= 0,
    nextDisabled: currentIndex >= totalChapters - 1,
    prevId: currentIndex > 0 ? chapters[currentIndex - 1].id : null,
    nextId: currentIndex < totalChapters - 1 ? chapters[currentIndex + 1].id : null
  };
}

describe('Feature: github-pages-docs, Property 2: Chapter navigation boundary behavior', () => {
  const totalChapters = chapters.length; // 12

  it('prevDisabled is true if and only if index === 0', () => {
    fc.assert(
      fc.property(
        fc.integer({ min: 0, max: 11 }),
        (index) => {
          const state = getNavState(index, totalChapters);
          if (index === 0) {
            expect(state.prevDisabled).toBe(true);
          } else {
            expect(state.prevDisabled).toBe(false);
          }
        }
      ),
      { numRuns: 100 }
    );
  });

  it('nextDisabled is true if and only if index === 11', () => {
    fc.assert(
      fc.property(
        fc.integer({ min: 0, max: 11 }),
        (index) => {
          const state = getNavState(index, totalChapters);
          if (index === 11) {
            expect(state.nextDisabled).toBe(true);
          } else {
            expect(state.nextDisabled).toBe(false);
          }
        }
      ),
      { numRuns: 100 }
    );
  });

  it('prevId is null iff index === 0, otherwise points to previous chapter', () => {
    fc.assert(
      fc.property(
        fc.integer({ min: 0, max: 11 }),
        (index) => {
          const state = getNavState(index, totalChapters);
          if (index === 0) {
            expect(state.prevId).toBeNull();
          } else {
            expect(state.prevId).toBe(chapters[index - 1].id);
          }
        }
      ),
      { numRuns: 100 }
    );
  });

  it('nextId is null iff index === 11, otherwise points to next chapter', () => {
    fc.assert(
      fc.property(
        fc.integer({ min: 0, max: 11 }),
        (index) => {
          const state = getNavState(index, totalChapters);
          if (index === 11) {
            expect(state.nextId).toBeNull();
          } else {
            expect(state.nextId).toBe(chapters[index + 1].id);
          }
        }
      ),
      { numRuns: 100 }
    );
  });
});
