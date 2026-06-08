import { describe, it, expect } from 'vitest';

/**
 * Pure function: convert internal .md links to doc.html?file= format.
 * Mirrors the implementation in doc.html for testability.
 */
function convertMdLink(href: string | null | undefined): string | null {
  if (!href || !href.endsWith('.md') || href.startsWith('http')) {
    return null;
  }
  // Extract filename: portion after the last '/' (or entire href if no '/')
  const lastSlashIndex = href.lastIndexOf('/');
  const filename = lastSlashIndex >= 0 ? href.substring(lastSlashIndex + 1) : href;
  // Strip .md only from the end (anchored match)
  const name = filename.replace(/\.md$/, '');
  return `doc.html?file=${name}`;
}

describe('convertMdLink', () => {
  it('converts ./foo.md to doc.html?file=foo', () => {
    expect(convertMdLink('./foo.md')).toBe('doc.html?file=foo');
  });

  it('converts official_document/bar.md to doc.html?file=bar', () => {
    expect(convertMdLink('official_document/bar.md')).toBe('doc.html?file=bar');
  });

  it('converts bar.md to doc.html?file=bar', () => {
    expect(convertMdLink('bar.md')).toBe('doc.html?file=bar');
  });

  it('converts ../baz.md to doc.html?file=baz', () => {
    expect(convertMdLink('../baz.md')).toBe('doc.html?file=baz');
  });

  it('does not convert URLs starting with http', () => {
    expect(convertMdLink('http://example.com/foo.md')).toBeNull();
    expect(convertMdLink('https://example.com/bar.md')).toBeNull();
  });

  it('does not convert hrefs that do not end with .md', () => {
    expect(convertMdLink('foo.html')).toBeNull();
    expect(convertMdLink('bar.txt')).toBeNull();
    expect(convertMdLink('something')).toBeNull();
  });

  it('returns null for null or undefined input', () => {
    expect(convertMdLink(null)).toBeNull();
    expect(convertMdLink(undefined)).toBeNull();
    expect(convertMdLink('')).toBeNull();
  });

  it('handles deeply nested paths', () => {
    expect(convertMdLink('a/b/c/deep.md')).toBe('doc.html?file=deep');
  });

  it('handles filename containing .md in the middle', () => {
    // e.g., a file named "my.md.file.md" - should only strip the trailing .md
    expect(convertMdLink('my.md.file.md')).toBe('doc.html?file=my.md.file');
  });

  it('handles paths with .md in directory names', () => {
    // e.g., "some.md.dir/readme.md" - should extract "readme" not strip .md from dir
    expect(convertMdLink('some.md.dir/readme.md')).toBe('doc.html?file=readme');
  });
});
