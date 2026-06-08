import { describe, it, expect } from 'vitest';

/**
 * Mock chapters array matching the structure defined in course.html.
 * Used to test prevId/nextId resolution.
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

describe('getNavState', () => {
  const totalChapters = chapters.length; // 12

  describe('boundary: first chapter (index=0)', () => {
    it('should disable previous button at index 0', () => {
      const state = getNavState(0, totalChapters);
      expect(state.prevDisabled).toBe(true);
    });

    it('should enable next button at index 0', () => {
      const state = getNavState(0, totalChapters);
      expect(state.nextDisabled).toBe(false);
    });

    it('should return null for prevId at index 0', () => {
      const state = getNavState(0, totalChapters);
      expect(state.prevId).toBeNull();
    });

    it('should return chapter 02 as nextId at index 0', () => {
      const state = getNavState(0, totalChapters);
      expect(state.nextId).toBe('02-quick-start');
    });
  });

  describe('boundary: last chapter (index=11)', () => {
    it('should enable previous button at index 11', () => {
      const state = getNavState(11, totalChapters);
      expect(state.prevDisabled).toBe(false);
    });

    it('should disable next button at index 11', () => {
      const state = getNavState(11, totalChapters);
      expect(state.nextDisabled).toBe(true);
    });

    it('should return chapter 11 as prevId at index 11', () => {
      const state = getNavState(11, totalChapters);
      expect(state.prevId).toBe('11-performance');
    });

    it('should return null for nextId at index 11', () => {
      const state = getNavState(11, totalChapters);
      expect(state.nextId).toBeNull();
    });
  });

  describe('middle chapter (index=5)', () => {
    it('should enable both prev and next buttons at index 5', () => {
      const state = getNavState(5, totalChapters);
      expect(state.prevDisabled).toBe(false);
      expect(state.nextDisabled).toBe(false);
    });

    it('should return correct prevId at index 5', () => {
      const state = getNavState(5, totalChapters);
      expect(state.prevId).toBe('05-sql-template-engine');
    });

    it('should return correct nextId at index 5', () => {
      const state = getNavState(5, totalChapters);
      expect(state.nextId).toBe('07-caching');
    });
  });

  describe('edge cases', () => {
    it('should handle single-chapter scenario (totalChapters=1)', () => {
      const state = getNavState(0, 1);
      expect(state.prevDisabled).toBe(true);
      expect(state.nextDisabled).toBe(true);
      expect(state.prevId).toBeNull();
      expect(state.nextId).toBeNull();
    });

    it('should handle negative index as having prev disabled', () => {
      const state = getNavState(-1, totalChapters);
      expect(state.prevDisabled).toBe(true);
    });

    it('should handle index beyond last as having next disabled', () => {
      const state = getNavState(12, totalChapters);
      expect(state.nextDisabled).toBe(true);
    });
  });
});
