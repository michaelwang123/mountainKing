/**
 * Feature: github-pages-docs, Property 5: Markdown rendering produces valid structure
 *
 * Validates: Requirements 2.3
 *
 * For any valid Markdown string containing headings, code blocks, and links,
 * the marked.js rendering pipeline SHALL produce HTML output where:
 * - every `# heading` becomes an `<h1>` element
 * - every fenced code block becomes a `<pre><code>` element
 * - every `[text](url)` becomes an `<a href="url">text</a>` element
 */
import { describe, it, expect } from 'vitest';
import fc from 'fast-check';
import { marked } from 'marked';

describe('Property 5: Markdown rendering produces valid structure', () => {
  /**
   * Validates: Requirements 2.3
   *
   * Headings: `# text` should produce `<h1>text</h1>`
   */
  it('should render # headings as <h1> elements', () => {
    // Generate heading text that is alphanumeric (no special markdown chars)
    const headingTextArb = fc.stringOf(
      fc.char().filter((c) => /[a-zA-Z0-9 ]/.test(c) && c !== ''),
      { minLength: 1, maxLength: 50 }
    ).filter((s) => s.trim().length > 0);

    fc.assert(
      fc.property(headingTextArb, (headingText) => {
        const markdown = `# ${headingText}`;
        const html = marked.parse(markdown) as string;

        // The output should contain an <h1> tag with the heading text
        expect(html).toMatch(/<h1[^>]*>/);
        expect(html).toContain(headingText.trim());
      }),
      { numRuns: 100 }
    );
  });

  /**
   * Validates: Requirements 2.3
   *
   * Headings levels 1-6: `## text` → `<h2>`, `### text` → `<h3>`, etc.
   */
  it('should render heading levels 1-6 as corresponding <hN> elements', () => {
    const headingLevelArb = fc.integer({ min: 1, max: 6 });
    const headingTextArb = fc.stringOf(
      fc.char().filter((c) => /[a-zA-Z0-9 ]/.test(c)),
      { minLength: 1, maxLength: 30 }
    ).filter((s) => s.trim().length > 0);

    fc.assert(
      fc.property(headingLevelArb, headingTextArb, (level, text) => {
        const hashes = '#'.repeat(level);
        const markdown = `${hashes} ${text}`;
        const html = marked.parse(markdown) as string;

        const regex = new RegExp(`<h${level}[^>]*>`);
        expect(html).toMatch(regex);
      }),
      { numRuns: 100 }
    );
  });

  /**
   * Validates: Requirements 2.3
   *
   * Fenced code blocks: ```lang\ncode\n``` → <pre><code>
   */
  it('should render fenced code blocks as <pre><code> elements', () => {
    const codeContentArb = fc.stringOf(
      fc.char().filter((c) => c !== '`' && c !== '\r'),
      { minLength: 1, maxLength: 80 }
    ).filter((s) => s.trim().length > 0 && !s.includes('```'));

    const languageArb = fc.constantFrom('', 'js', 'go', 'python', 'typescript', 'sql', 'yaml');

    fc.assert(
      fc.property(codeContentArb, languageArb, (code, lang) => {
        const markdown = `\`\`\`${lang}\n${code}\n\`\`\``;
        const html = marked.parse(markdown) as string;

        // Should contain <pre> and <code> elements
        expect(html).toMatch(/<pre>/);
        expect(html).toMatch(/<code/);
      }),
      { numRuns: 100 }
    );
  });

  /**
   * Validates: Requirements 2.3
   *
   * Links: `[text](url)` → `<a href="url">text</a>`
   */
  it('should render [text](url) as <a href="url">text</a> elements', () => {
    // Generate simple link text (no brackets or parens)
    const linkTextArb = fc.stringOf(
      fc.char().filter((c) => /[a-zA-Z0-9 \-_]/.test(c)),
      { minLength: 1, maxLength: 30 }
    ).filter((s) => s.trim().length > 0);

    // Generate simple URLs
    const urlArb = fc.tuple(
      fc.constantFrom('https://', 'http://'),
      fc.stringOf(
        fc.char().filter((c) => /[a-z0-9\-]/.test(c)),
        { minLength: 1, maxLength: 20 }
      ),
      fc.constantFrom('.com', '.org', '.io', '.dev')
    ).map(([protocol, domain, tld]) => `${protocol}${domain}${tld}`);

    fc.assert(
      fc.property(linkTextArb, urlArb, (text, url) => {
        const markdown = `[${text}](${url})`;
        const html = marked.parse(markdown) as string;

        // Should produce an anchor element with the correct href and text
        expect(html).toMatch(/<a\s/);
        expect(html).toContain(`href="${url}"`);
        expect(html).toContain(`>${text}</a>`);
      }),
      { numRuns: 100 }
    );
  });

  /**
   * Validates: Requirements 2.3
   *
   * Combined: Markdown with headings, code blocks, and links all produce valid structure
   */
  it('should render combined Markdown with headings, code, and links correctly', () => {
    const wordArb = fc.stringOf(
      fc.char().filter((c) => /[a-zA-Z0-9]/.test(c)),
      { minLength: 1, maxLength: 15 }
    ).filter((s) => s.trim().length > 0);

    fc.assert(
      fc.property(wordArb, wordArb, wordArb, (heading, code, linkText) => {
        const url = 'https://example.com';
        const markdown = [
          `# ${heading}`,
          '',
          `[${linkText}](${url})`,
          '',
          '```js',
          code,
          '```',
        ].join('\n');

        const html = marked.parse(markdown) as string;

        // Heading → <h1>
        expect(html).toMatch(/<h1[^>]*>/);
        // Link → <a href="...">
        expect(html).toMatch(/<a\s/);
        expect(html).toContain(`href="${url}"`);
        // Code block → <pre><code>
        expect(html).toMatch(/<pre>/);
        expect(html).toMatch(/<code/);
      }),
      { numRuns: 100 }
    );
  });
});
