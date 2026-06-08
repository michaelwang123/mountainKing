import { describe, it, expect } from 'vitest';
import fc from 'fast-check';

/**
 * Pure function: convert internal .md links to doc.html?file= format.
 * Mirrors the implementation in doc.html for testability.
 *
 * Feature: github-pages-docs, Property 1: Internal link conversion
 * Feature: github-pages-docs, Property 7: Internal link path extraction
 *
 * **Validates: Requirements 2.7**
 */
function convertMdLink(href: string | null | undefined): string | null {
  if (!href || !href.endsWith('.md') || href.startsWith('http')) {
    return null;
  }
  const lastSlashIndex = href.lastIndexOf('/');
  const filename = lastSlashIndex >= 0 ? href.substring(lastSlashIndex + 1) : href;
  const name = filename.replace(/\.md$/, '');
  return `doc.html?file=${name}`;
}

/**
 * Arbitrary: generates a valid path segment (non-empty, no `/`, no `.md` suffix confusion).
 */
const pathSegmentArb = fc.stringOf(
  fc.char().filter((c) => c !== '/' && c !== '\0'),
  { minLength: 1, maxLength: 20 }
);

/**
 * Arbitrary: generates a valid filename (non-empty, doesn't start with `http`, no `/`).
 */
const filenameArb = fc
  .stringOf(
    fc.char().filter((c) => c !== '/' && c !== '\0'),
    { minLength: 1, maxLength: 30 }
  )
  .filter((s) => !s.startsWith('http') && s.length > 0);

/**
 * Arbitrary: generates a file path with directory separators and a `.md` suffix.
 * Format: {segment1}/{segment2}/.../{filename}.md
 */
const internalMdPathArb = fc
  .tuple(
    fc.array(pathSegmentArb, { minLength: 0, maxLength: 4 }),
    filenameArb
  )
  .map(([dirs, name]) => {
    const path = dirs.length > 0 ? dirs.join('/') + '/' + name + '.md' : name + '.md';
    return path;
  })
  .filter((p) => !p.startsWith('http'));

describe('Property: Internal link conversion', () => {
  it('Property 1: Internal .md links are converted to doc.html?file={filename-without-md} format', () => {
    fc.assert(
      fc.property(internalMdPathArb, (href) => {
        const result = convertMdLink(href);

        // Must produce a non-null result since href ends with .md and doesn't start with http
        expect(result).not.toBeNull();

        // Result must start with doc.html?file=
        expect(result!.startsWith('doc.html?file=')).toBe(true);

        // The filename portion must not contain .md at the end
        const convertedName = result!.replace('doc.html?file=', '');
        expect(convertedName.endsWith('.md')).toBe(false);
      }),
      { numRuns: 100 }
    );
  });

  it('Property 7: Internal link path extraction - only the filename after the last / is used', () => {
    fc.assert(
      fc.property(internalMdPathArb, (href) => {
        const result = convertMdLink(href);

        // Extract what the expected filename should be
        const lastSlashIndex = href.lastIndexOf('/');
        const expectedFilename =
          lastSlashIndex >= 0 ? href.substring(lastSlashIndex + 1) : href;
        const expectedName = expectedFilename.replace(/\.md$/, '');

        expect(result).toBe(`doc.html?file=${expectedName}`);
      }),
      { numRuns: 100 }
    );
  });

  it('Property 1: Paths without .md suffix are not converted', () => {
    const nonMdPathArb = fc
      .stringOf(fc.char().filter((c) => c !== '\0'), { minLength: 1, maxLength: 50 })
      .filter((s) => !s.endsWith('.md') && s.length > 0);

    fc.assert(
      fc.property(nonMdPathArb, (href) => {
        const result = convertMdLink(href);
        expect(result).toBeNull();
      }),
      { numRuns: 100 }
    );
  });

  it('Property 1: Paths starting with http are not converted even if they end with .md', () => {
    const httpPathArb = fc
      .tuple(
        fc.constantFrom('http://', 'https://'),
        fc.stringOf(fc.char().filter((c) => c !== '\0'), { minLength: 1, maxLength: 30 })
      )
      .map(([prefix, rest]) => prefix + rest + '.md');

    fc.assert(
      fc.property(httpPathArb, (href) => {
        const result = convertMdLink(href);
        expect(result).toBeNull();
      }),
      { numRuns: 100 }
    );
  });
});
