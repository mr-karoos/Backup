import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { apiClient } from '@/lib/api/api-client';
import { tokenRefreshManager } from '@/lib/auth/token-refresh';

describe('Single-Flight Token Refresh Concurrency', () => {
  const originalFetch = global.fetch;

  beforeEach(() => {
    vi.clearAllMocks();
    tokenRefreshManager.reset();
  });

  afterEach(() => {
    global.fetch = originalFetch;
  });

  it('guarantees that 3 concurrent 401 requests trigger exactly ONE refresh call and replay successfully', async () => {
    let refreshCallCount = 0;
    let initialResourceCallCount = 0;
    let replayResourceCallCount = 0;

    let currentToken = 'initial-expired-token';

    global.fetch = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      const headers = new Headers(init?.headers);
      const authHeader = headers.get('Authorization');

      // Refresh Endpoint
      if (url === '/api/v1/auth/refresh') {
        refreshCallCount++;
        // Simulate minor async network delay
        return new Promise((resolve) => {
          setTimeout(() => {
            currentToken = 'new-refreshed-token';
            resolve({
              ok: true,
              status: 200,
              json: () =>
                Promise.resolve({
                  data: {
                    tokens: {
                      access_token: 'new-refreshed-token',
                      token_type: 'Bearer',
                      expires_in: 900,
                    },
                  },
                }),
            });
          }, 20);
        });
      }

      // Operational Endpoint (e.g. /resources)
      if (url === '/api/v1/resources') {
        if (authHeader === 'Bearer initial-expired-token') {
          initialResourceCallCount++;
          return Promise.resolve({
            ok: false,
            status: 401,
            json: () =>
              Promise.resolve({
                error: { code: 'UNAUTHORIZED', message: 'Token expired' },
              }),
          });
        }

        if (authHeader === 'Bearer new-refreshed-token') {
          replayResourceCallCount++;
          return Promise.resolve({
            ok: true,
            status: 200,
            json: () =>
              Promise.resolve({
                data: [{ id: 'res-1', name: 'Server A' }],
              }),
          });
        }
      }

      return Promise.reject(new Error(`Unexpected url: ${url}`));
    });

    const onTokenUpdate = vi.fn((token: string) => {
      currentToken = token;
    });

    apiClient.configure({
      getToken: () => currentToken,
      getOrgId: () => 'test-org-id',
      onTokenUpdate,
      onAuthFailure: vi.fn(),
    });

    // Fire Request A, B, and C concurrently
    const [resA, resB, resC] = await Promise.all([
      apiClient.get('/resources'),
      apiClient.get('/resources'),
      apiClient.get('/resources'),
    ]);

    // All 3 requests must succeed
    expect(resA).toEqual([{ id: 'res-1', name: 'Server A' }]);
    expect(resB).toEqual([{ id: 'res-1', name: 'Server A' }]);
    expect(resC).toEqual([{ id: 'res-1', name: 'Server A' }]);

    // CRITICAL: Exactly ONE POST /api/v1/auth/refresh call must have been issued
    expect(refreshCallCount).toBe(1);

    // Initial 3 requests failed with 401
    expect(initialResourceCallCount).toBe(3);

    // Replayed 3 requests succeeded with new token
    expect(replayResourceCallCount).toBe(3);

    // Token update callback invoked
    expect(onTokenUpdate).toHaveBeenCalledWith('new-refreshed-token');
  });

  it('rejects all queued requests and invokes onAuthFailure when refresh fails', async () => {
    let refreshCallCount = 0;

    global.fetch = vi.fn().mockImplementation((url: string) => {
      if (url === '/api/v1/auth/refresh') {
        refreshCallCount++;
        return Promise.resolve({
          ok: false,
          status: 401,
          json: () =>
            Promise.resolve({
              error: { code: 'UNAUTHORIZED', message: 'Refresh token invalid or expired' },
            }),
        });
      }

      // Initial call returns 401
      return Promise.resolve({
        ok: false,
        status: 401,
        json: () =>
          Promise.resolve({
            error: { code: 'UNAUTHORIZED', message: 'Token expired' },
          }),
      });
    });

    const onAuthFailure = vi.fn();

    apiClient.configure({
      getToken: () => 'expired-token',
      getOrgId: () => 'test-org-id',
      onTokenUpdate: vi.fn(),
      onAuthFailure,
    });

    const promiseA = apiClient.get('/resources');
    const promiseB = apiClient.get('/resources');

    await expect(promiseA).rejects.toThrow();
    await expect(promiseB).rejects.toThrow();

    expect(refreshCallCount).toBe(1);
    expect(onAuthFailure).toHaveBeenCalled();
  });
});
