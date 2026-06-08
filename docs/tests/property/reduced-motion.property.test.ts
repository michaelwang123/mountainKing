import { describe, it, expect, beforeAll } from 'vitest';
import fc from 'fast-check';
import { readFileSync } from 'fs';
import { resolve } from 'path';

/**
 * Feature: github-pages-docs, Property 6: Reduced motion support
 *
 * Verifies that the CSS file contains a proper @media (prefers-reduced-motion: reduce)
 * block targeting *, *::before, *::after with animation-duration and transition-duration
 * values ≤ 0.01ms.
 *
 * **Validates: Requirements 1.9, 4.3**
 */

let cssContent: string;

beforeAll(() => {
  const cssPath = resolve(__dirname, '../../assets/style.css');
  cssContent = readFileSync(cssPath, 'utf-8');
});

/**
 * Parse the @media (prefers-reduced-motion: reduce) block from CSS content.
 * Returns the inner content of the media query block.
 */
function extractReducedMotionBlock(css: string): string | null {
  const mediaRegex = /@media\s*\(\s*prefers-reduced-motion\s*:\s*reduce\s*\)\s*\{([\s\S]*?)\n\}/;
  const match = css.match(mediaRegex);
  return match ? match[1] : null;
}

/**
 * Check if a selector set targets the required universal selectors.
 */
function hasUniversalSelectors(block: string): {
  hasStar: boolean;
  hasBeforePseudo: boolean;
  hasAfterPseudo: boolean;
} {
  // Look for a rule that targets *, *::before, *::after
  const selectorRegex = /([^{]+)\{([^}]+)\}/;
  const match = block.match(selectorRegex);
  if (!match) {
    return { hasStar: false, hasBeforePseudo: false, hasAfterPseudo: false };
  }
  const selector = match[1].trim();
  return {
    hasStar: /(?:^|,)\s*\*\s*(?:,|$)/.test(selector) || selector.includes('*,') || selector.startsWith('*') || /\*[^:]/.test(selector) || selector === '*' || /,\s*\*\s*,/.test(selector) || /,\s*\*\s*$/.test(selector),
    hasBeforePseudo: selector.includes('*::before'),
    hasAfterPseudo: selector.includes('*::after'),
  };
}

/**
 * Extract duration values from a CSS declaration block.
 * Returns parsed duration values in milliseconds.
 */
function parseDurationValue(value: string): number {
  value = value.trim().replace('!important', '').trim();
  if (value.endsWith('ms')) {
    return parseFloat(value);
  }
  if (value.endsWith('s')) {
    return parseFloat(value) * 1000;
  }
  return parseFloat(value);
}

/**
 * Extract all duration properties from the reduced motion block.
 */
function extractDurationProperties(block: string): {
  animationDuration: number | null;
  transitionDuration: number | null;
} {
  const animDurationMatch = block.match(/animation-duration\s*:\s*([^;]+);/);
  const transDurationMatch = block.match(/transition-duration\s*:\s*([^;]+);/);

  return {
    animationDuration: animDurationMatch ? parseDurationValue(animDurationMatch[1]) : null,
    transitionDuration: transDurationMatch ? parseDurationValue(transDurationMatch[1]) : null,
  };
}

describe('Property 6: Reduced motion support', () => {
  it('CSS file contains @media (prefers-reduced-motion: reduce) block', () => {
    fc.assert(
      fc.property(fc.constant(cssContent), (css) => {
        const block = extractReducedMotionBlock(css);
        expect(block).not.toBeNull();
      }),
      { numRuns: 1 }
    );
  });

  it('Reduced motion block targets *, *::before, *::after selectors', () => {
    fc.assert(
      fc.property(fc.constant(cssContent), (css) => {
        const block = extractReducedMotionBlock(css);
        expect(block).not.toBeNull();

        const selectors = hasUniversalSelectors(block!);
        expect(selectors.hasStar).toBe(true);
        expect(selectors.hasBeforePseudo).toBe(true);
        expect(selectors.hasAfterPseudo).toBe(true);
      }),
      { numRuns: 1 }
    );
  });

  it('Reduced motion block sets animation-duration ≤ 0.01ms', () => {
    fc.assert(
      fc.property(fc.constant(cssContent), (css) => {
        const block = extractReducedMotionBlock(css);
        expect(block).not.toBeNull();

        const durations = extractDurationProperties(block!);
        expect(durations.animationDuration).not.toBeNull();
        expect(durations.animationDuration!).toBeLessThanOrEqual(0.01);
      }),
      { numRuns: 1 }
    );
  });

  it('Reduced motion block sets transition-duration ≤ 0.01ms', () => {
    fc.assert(
      fc.property(fc.constant(cssContent), (css) => {
        const block = extractReducedMotionBlock(css);
        expect(block).not.toBeNull();

        const durations = extractDurationProperties(block!);
        expect(durations.transitionDuration).not.toBeNull();
        expect(durations.transitionDuration!).toBeLessThanOrEqual(0.01);
      }),
      { numRuns: 1 }
    );
  });

  it('Reduced motion durations use !important to override all animations', () => {
    fc.assert(
      fc.property(fc.constant(cssContent), (css) => {
        const block = extractReducedMotionBlock(css);
        expect(block).not.toBeNull();

        // Check that both duration declarations have !important
        expect(block!).toMatch(/animation-duration\s*:\s*[^;]*!important/);
        expect(block!).toMatch(/transition-duration\s*:\s*[^;]*!important/);
      }),
      { numRuns: 1 }
    );
  });

  it('Property 6: For any animation class defined in the CSS, reduced motion media query ensures durations are effectively zero', () => {
    // Collect all animation class names defined in the CSS
    const animationClassRegex = /\.animate-[\w-]+\s*\{[^}]*animation\s*:/g;
    const animationClasses: string[] = [];
    let match;
    while ((match = animationClassRegex.exec(cssContent)) !== null) {
      animationClasses.push(match[0]);
    }

    // Generate arbitrary selections from the animation classes to verify
    // the reduced motion block applies universally via * selector
    const animClassArb = animationClasses.length > 0
      ? fc.constantFrom(...animationClasses)
      : fc.constant('.animate-fade-in-up { animation:');

    fc.assert(
      fc.property(animClassArb, (_animClass) => {
        // The reduced motion block uses * selector which covers ALL elements
        // regardless of their specific animation class
        const block = extractReducedMotionBlock(cssContent);
        expect(block).not.toBeNull();

        // The * selector means any element with any animation class will be affected
        const selectors = hasUniversalSelectors(block!);
        expect(selectors.hasStar).toBe(true);

        // Verify the duration override applies
        const durations = extractDurationProperties(block!);
        expect(durations.animationDuration).not.toBeNull();
        expect(durations.animationDuration!).toBeLessThanOrEqual(0.01);
        expect(durations.transitionDuration).not.toBeNull();
        expect(durations.transitionDuration!).toBeLessThanOrEqual(0.01);
      }),
      { numRuns: 100 }
    );
  });
});
