import { describe, it, expect, beforeAll } from 'vitest';
import { readFileSync } from 'fs';
import { resolve } from 'path';

let formatPageTitle: (h1Text: string) => string;

beforeAll(() => {
  // Extract the formatPageTitle function from doc.html script
  const html = readFileSync(resolve(__dirname, '../../doc.html'), 'utf-8');
  // Extract the script block content
  const scriptMatch = html.match(/<script>([\s\S]*?)<\/script>/);
  if (!scriptMatch) throw new Error('No script block found in doc.html');

  // Extract just the formatPageTitle function definition
  const fnMatch = scriptMatch[1].match(
    /function formatPageTitle\(h1Text\)\s*\{[\s\S]*?\n        \}/
  );
  if (!fnMatch) throw new Error('formatPageTitle function not found in doc.html');

  // Evaluate the function in isolation
  formatPageTitle = new Function('return (' + fnMatch[0] + ')')() as any;
});

describe('formatPageTitle', () => {
  it('should format a normal short title with suffix', () => {
    expect(formatPageTitle('架构')).toBe('架构 - MountainKing');
  });

  it('should return "MountainKing" for empty string', () => {
    expect(formatPageTitle('')).toBe('MountainKing');
  });

  it('should return "MountainKing" for whitespace-only string', () => {
    expect(formatPageTitle('   ')).toBe('MountainKing');
  });

  it('should not truncate when total length is exactly 60', () => {
    // suffix " - MountainKing" is 15 chars, so h1Text can be 45 chars
    const h1Text = 'A'.repeat(45); // 45 + 15 = 60
    const result = formatPageTitle(h1Text);
    expect(result).toBe(h1Text + ' - MountainKing');
    expect(result.length).toBe(60);
  });

  it('should truncate and add "…" when total exceeds 60 characters', () => {
    const h1Text = 'A'.repeat(50); // 50 + 15 = 65 > 60
    const result = formatPageTitle(h1Text);
    expect(result.length).toBe(60);
    expect(result.endsWith(' - MountainKing')).toBe(true);
    expect(result).toContain('…');
  });

  it('should handle a very long title', () => {
    const h1Text = '这是一个非常非常非常非常非常非常非常非常非常非常非常长的中文文档标题用于测试截断功能是否正常工作';
    const result = formatPageTitle(h1Text);
    expect(result.length).toBeLessThanOrEqual(60);
    expect(result.endsWith(' - MountainKing')).toBe(true);
    expect(result).toContain('…');
  });

  it('should return null-safe for null/undefined input', () => {
    expect(formatPageTitle(null as any)).toBe('MountainKing');
    expect(formatPageTitle(undefined as any)).toBe('MountainKing');
  });
});
