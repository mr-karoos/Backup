import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ErrorState } from '@/components/ui/error-state';
import { buildCspHeader } from '@/lib/security/csp';
import nextConfig from '@/next.config.mjs';
import fs from 'fs';
import path from 'path';

describe('Security & Anti-XSS Verification', () => {
  it('renders malicious <script>alert(1)</script> in backend error as harmless plain text', () => {
    const maliciousPayload = '<script>alert("XSS Attack")</script>';

    render(<ErrorState title="System Failure" error={maliciousPayload} />);

    // Must be found as text content inside DOM
    const textElement = screen.getByText(maliciousPayload);
    expect(textElement).toBeInTheDocument();

    // Must NOT exist as an active script tag in DOM
    const scriptTag = document.querySelector('script');
    expect(scriptTag).toBeNull();
  });

  it('renders onerror payload safely without HTML execution', () => {
    const maliciousImg = '<img src="invalid" onerror="window.pwned=true" />';

    render(<ErrorState title="Error" error={maliciousImg} />);

    expect(screen.getByText(maliciousImg)).toBeInTheDocument();
    expect((window as unknown as { pwned?: boolean }).pwned).toBeUndefined();
  });

  it('verifies that no product code uses dangerouslySetInnerHTML', () => {
    const webSrcDir = path.resolve(__dirname, '../../');

    function checkDir(dir: string): string[] {
      const violations: string[] = [];
      const entries = fs.readdirSync(dir, { withFileTypes: true });

      for (const entry of entries) {
        if (entry.name === 'node_modules' || entry.name === '.next' || entry.name === 'tests') {
          continue;
        }

        const fullPath = path.join(dir, entry.name);
        if (entry.isDirectory()) {
          violations.push(...checkDir(fullPath));
        } else if (entry.name.endsWith('.tsx') || entry.name.endsWith('.ts')) {
          const content = fs.readFileSync(fullPath, 'utf8');
          if (content.includes('dangerouslySetInnerHTML')) {
            violations.push(fullPath);
          }
        }
      }

      return violations;
    }

    const foundViolations = checkDir(webSrcDir);
    expect(foundViolations).toEqual([]);
  });

  describe('Content-Security-Policy (CSP) Construction & Strict Enforcement', () => {
    it('generates hardened production CSP without unsafe-inline or unsafe-eval in script-src', () => {
      const nonce = 'test-random-nonce-12345';
      const csp = buildCspHeader({ isProduction: true, nonce });

      // Script security assertions
      expect(csp).toContain(`script-src 'self' 'nonce-${nonce}'`);
      expect(csp).not.toContain("'unsafe-inline' in script-src");
      expect(csp).not.toMatch(/script-src[^;]*'unsafe-inline'/);
      expect(csp).not.toMatch(/script-src[^;]*'unsafe-eval'/);

      // Essential strict policy directives
      expect(csp).toContain("frame-ancestors 'none'");
      expect(csp).toContain("object-src 'none'");
      expect(csp).toContain("base-uri 'self'");
      expect(csp).toContain("form-action 'self'");
      expect(csp).toContain("connect-src 'self'");
      expect(csp).toContain("default-src 'self'");
    });

    it('allows unsafe-eval strictly only in development mode for HMR, never unsafe-inline', () => {
      const cspDev = buildCspHeader({ isProduction: false, nonce: 'dev-nonce' });
      expect(cspDev).toMatch(/script-src[^;]*'unsafe-eval'/);
      expect(cspDev).not.toMatch(/script-src[^;]*'unsafe-inline'/);
    });
  });

  it('verifies critical security headers in next.config.mjs', async () => {
    const headersFn = nextConfig.headers;
    expect(headersFn).toBeDefined();

    const headersList = await headersFn!();
    const globalHeaderRule = headersList.find((h) => h.source === '/(.*)');
    expect(globalHeaderRule).toBeDefined();

    const headersMap = new Map<string, string>();
    for (const h of globalHeaderRule!.headers) {
      headersMap.set(h.key, h.value);
    }

    // Standard browser hardening headers applied globally in next.config.mjs
    expect(headersMap.get('X-Content-Type-Options')).toBe('nosniff');
    expect(headersMap.get('Referrer-Policy')).toBe('strict-origin-when-cross-origin');
    expect(headersMap.get('X-Frame-Options')).toBe('DENY');
    expect(headersMap.get('Permissions-Policy')).toBe('camera=(), microphone=(), geolocation=()');
  });

  describe('Next.js 16 Proxy & Security Architecture', () => {
    it('verifies web/proxy.ts exists and implements export function proxy', () => {
      const proxyPath = path.resolve(__dirname, '../../proxy.ts');
      expect(fs.existsSync(proxyPath)).toBe(true);

      const content = fs.readFileSync(proxyPath, 'utf8');
      expect(content).toContain('export function proxy(');
    });

    it('verifies web/middleware.ts is deleted in accordance with Next.js 16 conventions', () => {
      const middlewarePath = path.resolve(__dirname, '../../middleware.ts');
      expect(fs.existsSync(middlewarePath)).toBe(false);
    });
  });
});
