/**
 * Centralized Content Security Policy (CSP) builder.
 * Enforces production policy:
 * - NO 'unsafe-inline' in script-src
 * - NO 'unsafe-eval' in script-src (except development HMR)
 * - Cryptographic nonce-based script execution
 * - Strict object-src, base-uri, frame-ancestors, connect-src
 */

export interface CspOptions {
  isProduction?: boolean;
  nonce?: string;
}

export function buildCspHeader(options: CspOptions = {}): string {
  const { isProduction = true, nonce } = options;

  const scriptSources = [
    "'self'",
    nonce ? `'nonce-${nonce}'` : undefined,
    nonce ? "'strict-dynamic'" : undefined,
    !isProduction ? "'unsafe-eval'" : undefined,
  ].filter(Boolean);

  const directives: Record<string, string> = {
    'default-src': "'self'",
    'script-src': scriptSources.join(' '),
    'style-src': "'self' 'unsafe-inline'",
    'img-src': "'self' blob: data:",
    'font-src': "'self'",
    'connect-src': "'self'",
    'object-src': "'none'",
    'base-uri': "'self'",
    'form-action': "'self'",
    'frame-ancestors': "'none'",
  };

  return Object.entries(directives)
    .map(([key, value]) => `${key} ${value}`)
    .join('; ');
}
