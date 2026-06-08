import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import { resolve } from 'path';

/**
 * Smoke tests for HTML structure and meta tags across all documentation pages.
 * Validates: Requirements 9.1, 4.1
 */

const docsDir = resolve(__dirname, '../..');
const htmlFiles = [
  { name: 'index.html', path: resolve(docsDir, 'index.html') },
  { name: 'doc.html', path: resolve(docsDir, 'doc.html') },
  { name: 'course.html', path: resolve(docsDir, 'course.html') },
  { name: '404.html', path: resolve(docsDir, '404.html') },
];
const cssPath = resolve(docsDir, 'assets/style.css');

function readFile(filePath: string): string {
  return readFileSync(filePath, 'utf-8');
}

describe('HTML structure and meta tags', () => {
  for (const { name, path } of htmlFiles) {
    describe(name, () => {
      let html: string;

      beforeAll(() => {
        html = readFile(path);
      });

      it('has a <title> tag that is present and ≤60 characters', () => {
        const match = html.match(/<title>([^<]*)<\/title>/i);
        expect(match).not.toBeNull();
        const titleText = match![1].trim();
        expect(titleText.length).toBeGreaterThan(0);
        expect(titleText.length).toBeLessThanOrEqual(60);
      });

      it('has a <meta name="description"> with content between 50-160 characters', () => {
        const match = html.match(/<meta\s+name=["']description["']\s+content=["']([^"']*)["']/i);
        expect(match).not.toBeNull();
        const description = match![1].trim();
        expect(description.length).toBeGreaterThanOrEqual(50);
        expect(description.length).toBeLessThanOrEqual(160);
      });

      it('has a <link rel="icon"> tag', () => {
        const hasIcon = /<link\s[^>]*rel=["']icon["'][^>]*>/i.test(html);
        expect(hasIcon).toBe(true);
      });

      it('has a <meta name="viewport"> tag', () => {
        const hasViewport = /<meta\s[^>]*name=["']viewport["'][^>]*>/i.test(html);
        expect(hasViewport).toBe(true);
      });
    });
  }
});

describe('CSS animation @keyframes definitions', () => {
  let css: string;

  const expectedKeyframes = [
    'dash-flow',
    'pulse-glow',
    'move-dot-right',
    'fade-in-up',
    'spin-slow',
    'shimmer',
    'float',
  ];

  beforeAll(() => {
    css = readFile(cssPath);
  });

  for (const keyframeName of expectedKeyframes) {
    it(`contains @keyframes ${keyframeName}`, () => {
      const regex = new RegExp(`@keyframes\\s+${keyframeName}\\s*\\{`);
      expect(css).toMatch(regex);
    });
  }

  it('contains all 7 @keyframes definitions', () => {
    const allKeyframes = css.match(/@keyframes\s+[\w-]+\s*\{/g) || [];
    expect(allKeyframes.length).toBeGreaterThanOrEqual(7);
  });
});
