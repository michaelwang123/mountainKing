/**
 * Feature: github-pages-docs, Property 4: Dynamic title format
 *
 * Property 4: For any successfully loaded Markdown document containing an h1 heading,
 * the Doc Viewer SHALL update document.title to the format "{h1Text} - MountainKing".
 * If the concatenated string exceeds 60 characters, the h1 text portion SHALL be
 * truncated and appended with "…" such that the total title length does not exceed 60 characters.
 *
 * **Validates: Requirements 9.5**
 */
import { describe, it, expect, beforeAll } from 'vitest';
import fc from 'fast-check';
import { readFileSync } from 'fs';
import { resolve } from 'path';

let formatPageTitle: (h1Text: string) => string;

beforeAll(() => {
  const html = readFileSync(resolve(__dirname, '../../doc.html'), 'utf-8');
  const scriptMatch = html.match(/<script>([\s\S]*?)<\/script>/);
  if (!scriptMatch) throw new Error('No script block found in doc.html');

  const fnMatch = scriptMatch[1].match(
    /function formatPageTitle\(h1Text\)\s*\{[\s\S]*?\n        \}/
  );
  if (!fnMatch) throw new Error('formatPageTitle function not found in doc.html');

  formatPageTitle = new Function('return (' + fnMatch[0] + ')')() as any;
});

describe('Property 4: Dynamic title format', () => {
  const SUFFIX = ' - MountainKing';
  const MAX_TITLE_LENGTH = 60;

  it('title output never exceeds 60 characters for any h1 string', () => {
    fc.assert(
      fc.property(fc.string(), (h1Text) => {
        const result = formatPageTitle(h1Text);
        expect(result.length).toBeLessThanOrEqual(MAX_TITLE_LENGTH);
      }),
      { numRuns: 100 }
    );
  });

  it('non-empty h1 produces title in format "{text} - MountainKing"', () => {
    fc.assert(
      fc.property(
        fc.string({ minLength: 1 }).filter((s) => s.trim().length > 0),
        (h1Text) => {
          const result = formatPageTitle(h1Text);
          // Result must end with the suffix
          expect(result.endsWith(SUFFIX)).toBe(true);
        }
      ),
      { numRuns: 100 }
    );
  });

  it('short titles are preserved without truncation', () => {
    fc.assert(
      fc.property(
        fc.string({ minLength: 1, maxLength: 44 }).filter((s) => s.trim().length > 0),
        (h1Text) => {
          // h1Text up to 44 chars + 15 char suffix = max 59 chars, within 60 limit
          // h1Text of exactly 45 chars + 15 = 60, still within limit
          if ((h1Text + SUFFIX).length <= MAX_TITLE_LENGTH) {
            const result = formatPageTitle(h1Text);
            expect(result).toBe(h1Text + SUFFIX);
          }
        }
      ),
      { numRuns: 100 }
    );
  });

  it('long titles are truncated with "…" and still end with suffix', () => {
    fc.assert(
      fc.property(
        fc.string({ minLength: 46 }).filter((s) => s.trim().length > 0),
        (h1Text) => {
          // h1Text of 46+ chars + 15 char suffix = 61+, will exceed 60
          const result = formatPageTitle(h1Text);
          expect(result.length).toBeLessThanOrEqual(MAX_TITLE_LENGTH);
          expect(result.endsWith(SUFFIX)).toBe(true);
          expect(result).toContain('…');
        }
      ),
      { numRuns: 100 }
    );
  });

  it('empty or whitespace-only h1 returns bare "MountainKing"', () => {
    fc.assert(
      fc.property(
        fc.stringOf(fc.constantFrom(' ', '\t', '\n', '\r')),
        (whitespace) => {
          const result = formatPageTitle(whitespace);
          expect(result).toBe('MountainKing');
        }
      ),
      { numRuns: 100 }
    );
  });
});
