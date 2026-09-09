import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { apiClient } from '@/lib/api/api-client';
import { ApiError } from '@/types/api';

describe('Central ApiClient', () => {
  const originalFetch = global.fetch;

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    global.fetch = originalFetch;
  });

  it('injects Bearer token and X-Organization-ID into tenant-scoped requests', async () => {
    const captured = { headers: null as Headers | null };

    global.fetch = vi.fn().mockImplementation((_url, init) => {
      captured.headers = new Headers(init.headers);
      return Promise.resolve({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ data: { message: 'success' } }),
      });
    });

    apiClient.configure({
      getToken: () => 'test-jwt-token',
      getOrgId: () => '11111111-1111-1111-1111-111111111111',
      onTokenUpdate: vi.fn(),
      onAuthFailure: vi.fn(),
    });

    const result = await apiClient.get<{ message: string }>('/resources');

    expect(result).toEqual({ message: 'success' });
    expect(captured.headers?.get('Authorization')).toBe('Bearer test-jwt-token');
    expect(captured.headers?.get('X-Organization-ID')).toBe('11111111-1111-1111-1111-111111111111');
  });

  it('does NOT inject X-Organization-ID on public or global endpoints', async () => {
    const captured = { headers: null as Headers | null };

    global.fetch = vi.fn().mockImplementation((_url, init) => {
      captured.headers = new Headers(init.headers);
      return Promise.resolve({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ data: [] }),
      });
    });

    apiClient.configure({
      getToken: () => 'test-jwt-token',
      getOrgId: () => '11111111-1111-1111-1111-111111111111',
      onTokenUpdate: vi.fn(),
      onAuthFailure: vi.fn(),
    });

    await apiClient.get('/organizations'); // Global list
    expect(captured.headers?.get('X-Organization-ID')).toBeNull();

    await apiClient.get('/health', { skipAuth: true }); // Health check
    expect(captured.headers?.get('X-Organization-ID')).toBeNull();
  });

  it('normalizes backend error envelope into ApiError', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
      json: () =>
        Promise.resolve({
          error: {
            code: 'RESOURCE_NOT_FOUND',
            message: 'Target resource does not exist',
            details: { id: 'test-id' },
          },
          request_id: 'req-12345',
        }),
    });

    await expect(apiClient.get('/resources/test-id')).rejects.toThrow(ApiError);

    try {
      await apiClient.get('/resources/test-id');
    } catch (err: unknown) {
      const apiErr = err as ApiError;
      expect(apiErr.status).toBe(404);
      expect(apiErr.code).toBe('RESOURCE_NOT_FOUND');
      expect(apiErr.message).toBe('Target resource does not exist');
      expect(apiErr.requestId).toBe('req-12345');
    }
  });

  it('propagates AbortSignal cancellation', async () => {
    const controller = new AbortController();
    controller.abort();

    global.fetch = vi.fn().mockImplementation((_url, init) => {
      if (init?.signal?.aborted) {
        const error = new DOMException('The user aborted a request.', 'AbortError');
        return Promise.reject(error);
      }
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) });
    });

    await expect(
      apiClient.get('/resources', { signal: controller.signal })
    ).rejects.toThrow('The user aborted a request.');
  });

  it('does NOT trigger token refresh on 403 Forbidden', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 403,
      json: () =>
        Promise.resolve({
          error: {
            code: 'FORBIDDEN',
            message: 'You lack permission to access this resource',
          },
        }),
    });

    const onTokenUpdate = vi.fn();
    const onAuthFailure = vi.fn();

    apiClient.configure({
      getToken: () => 'token',
      getOrgId: () => 'org-id',
      onTokenUpdate,
      onAuthFailure,
    });

    await expect(apiClient.get('/resources')).rejects.toThrow('You lack permission to access this resource');
    expect(onTokenUpdate).not.toHaveBeenCalled();
    expect(onAuthFailure).not.toHaveBeenCalled();
  });

  it('sends bodyless POST request without Content-Type header when data is undefined', async () => {
    let capturedInit: RequestInit | undefined;

    global.fetch = vi.fn().mockImplementation((_url, init) => {
      capturedInit = init;
      return Promise.resolve({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ data: { status: 'success' } }),
      });
    });

    apiClient.configure({
      getToken: () => 'test-jwt-token',
      getOrgId: () => 'tenant-111',
      onTokenUpdate: vi.fn(),
      onAuthFailure: vi.fn(),
    });

    await apiClient.post('/resources/res-1/test-connection', undefined);

    expect(capturedInit).toBeDefined();
    const headers = new Headers(capturedInit?.headers);
    expect(headers.get('Content-Type')).toBeNull();
    expect(capturedInit?.body).toBeUndefined();
    expect(headers.get('X-Organization-ID')).toBe('tenant-111');
  });

  it('preserves custom tenantOrgId across 401 token refresh replay', async () => {
    let callCount = 0;
    const capturedHeaders: Headers[] = [];
    let currentToken = 'expired-token';

    global.fetch = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      const headers = new Headers(init?.headers);

      if (url === '/api/v1/auth/refresh') {
        currentToken = 'fresh-token';
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () =>
            Promise.resolve({
              data: {
                tokens: {
                  access_token: 'fresh-token',
                  token_type: 'Bearer',
                  expires_in: 900,
                },
              },
            }),
        });
      }

      if (url === '/api/v1/resources') {
        callCount++;
        capturedHeaders.push(headers);

        if (headers.get('Authorization') === 'Bearer expired-token') {
          return Promise.resolve({
            ok: false,
            status: 401,
            json: () => Promise.resolve({ error: { code: 'UNAUTHORIZED', message: 'Token expired' } }),
          });
        }

        return Promise.resolve({
          ok: true,
          status: 200,
          json: () => Promise.resolve({ data: [] }),
        });
      }

      return Promise.reject(new Error(`Unexpected url: ${url}`));
    });

    apiClient.configure({
      getToken: () => currentToken,
      getOrgId: () => 'default-org-id',
      onTokenUpdate: (t) => {
        currentToken = t;
      },
      onAuthFailure: vi.fn(),
    });

    // Request specifying a custom tenantOrgId (e.g. from an explicit mutation context)
    const customOrgId = 'custom-org-999';
    await apiClient.get('/resources', { tenantOrgId: customOrgId });

    expect(callCount).toBe(2);
    // Both initial call and replayed call MUST have the custom tenantOrgId
    expect(capturedHeaders[0]?.get('X-Organization-ID')).toBe(customOrgId);
    expect(capturedHeaders[1]?.get('X-Organization-ID')).toBe(customOrgId);
    expect(capturedHeaders[1]?.get('Authorization')).toBe('Bearer fresh-token');
  });

  describe('Paginated API Support', () => {
    it('getPaginated preserves data array and page metadata', async () => {
      global.fetch = vi.fn().mockImplementation(() => {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () =>
            Promise.resolve({
              data: [
                { id: 'job-1', operation_type: 'restic_prune', status: 'completed' },
                { id: 'job-2', operation_type: 'restic_forget', status: 'pending' },
              ],
              page: {
                next_cursor: 'eyJpZCI6ImpvYi0yIn0=',
                has_more: true,
              },
            }),
        });
      });

      apiClient.configure({
        getToken: () => 'token-123',
        getOrgId: () => 'org-abc',
        onTokenUpdate: vi.fn(),
        onAuthFailure: vi.fn(),
      });

      const res = await apiClient.getPaginated<{ id: string; operation_type: string; status: string }>(
        '/maintenance-jobs'
      );

      expect(res.data).toHaveLength(2);
      expect(res.data[0]!.id).toBe('job-1');
      expect(res.page).toEqual({
        next_cursor: 'eyJpZCI6ImpvYi0yIn0=',
        has_more: true,
      });
    });

    it('legacy apiClient.get continues unwrapping data only', async () => {
      global.fetch = vi.fn().mockImplementation(() => {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () =>
            Promise.resolve({
              data: [{ id: 'job-1' }],
              page: {
                next_cursor: 'cursor-abc',
                has_more: true,
              },
            }),
        });
      });

      const data = await apiClient.get<{ id: string }[]>('/maintenance-jobs');
      // Should be array unwrapped from data, NOT the envelope
      expect(Array.isArray(data)).toBe(true);
      expect(data).toEqual([{ id: 'job-1' }]);
    });

    it('getPaginated preserves Bearer token and X-Organization-ID injection', async () => {
      const captured = { headers: null as Headers | null };

      global.fetch = vi.fn().mockImplementation((_url, init) => {
        captured.headers = new Headers(init.headers);
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () =>
            Promise.resolve({
              data: [],
              page: { next_cursor: null, has_more: false },
            }),
        });
      });

      apiClient.configure({
        getToken: () => 'valid-jwt',
        getOrgId: () => 'tenant-uuid-999',
        onTokenUpdate: vi.fn(),
        onAuthFailure: vi.fn(),
      });

      await apiClient.getPaginated('/maintenance-jobs');

      expect(captured.headers?.get('Authorization')).toBe('Bearer valid-jwt');
      expect(captured.headers?.get('X-Organization-ID')).toBe('tenant-uuid-999');
    });

    it('propagates AbortSignal to native fetch in getPaginated', async () => {
      let capturedSignal: AbortSignal | null | undefined;

      global.fetch = vi.fn().mockImplementation((_url, init) => {
        capturedSignal = init?.signal;
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () =>
            Promise.resolve({
              data: [],
              page: { next_cursor: null, has_more: false },
            }),
        });
      });

      const controller = new AbortController();
      await apiClient.getPaginated('/maintenance-jobs', { signal: controller.signal });

      expect(capturedSignal).toBe(controller.signal);
    });

    it('preserves tenantOrgId snapshot and paginated envelope across 401 token refresh replay', async () => {
      let callCount = 0;
      const capturedHeaders: Headers[] = [];
      let currentToken = 'expired-token';
      let currentOrgProvider = 'initial-org-111';

      global.fetch = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
        const headers = new Headers(init?.headers);

        if (url === '/api/v1/auth/refresh') {
          currentToken = 'fresh-replayed-token';
          return Promise.resolve({
            ok: true,
            status: 200,
            json: () =>
              Promise.resolve({
                data: {
                  tokens: {
                    access_token: 'fresh-replayed-token',
                    token_type: 'Bearer',
                    expires_in: 900,
                  },
                },
              }),
          });
        }

        if (url.startsWith('/api/v1/maintenance-jobs')) {
          callCount++;
          capturedHeaders.push(headers);

          if (headers.get('Authorization') === 'Bearer expired-token') {
            // Simulate changing org provider in the middle of request/refresh
            currentOrgProvider = 'changed-org-222';

            return Promise.resolve({
              ok: false,
              status: 401,
              json: () => Promise.resolve({ error: { code: 'UNAUTHORIZED', message: 'Token expired' } }),
            });
          }

          return Promise.resolve({
            ok: true,
            status: 200,
            json: () =>
              Promise.resolve({
                data: [{ id: 'job-replayed-1', operation_type: 'restic_prune', status: 'completed' }],
                page: { next_cursor: 'cursor-after-replay', has_more: true },
              }),
          });
        }

        return Promise.reject(new Error(`Unexpected url: ${url}`));
      });

      apiClient.configure({
        getToken: () => currentToken,
        getOrgId: () => currentOrgProvider,
        onTokenUpdate: (t) => {
          currentToken = t;
        },
        onAuthFailure: vi.fn(),
      });

      const result = await apiClient.getPaginated<{ id: string; operation_type: string; status: string }>(
        '/maintenance-jobs',
        { tenantOrgId: 'pinned-snapshot-org-999' }
      );

      expect(callCount).toBe(2);
      // Both initial and replayed call must retain the pinned tenantOrgId snapshot, NOT the changed provider
      expect(capturedHeaders[0]?.get('X-Organization-ID')).toBe('pinned-snapshot-org-999');
      expect(capturedHeaders[1]?.get('X-Organization-ID')).toBe('pinned-snapshot-org-999');
      expect(capturedHeaders[1]?.get('Authorization')).toBe('Bearer fresh-replayed-token');

      // Paginated result envelope intact
      expect(result.data).toHaveLength(1);
      expect(result.data[0]!.id).toBe('job-replayed-1');
      expect(result.page.next_cursor).toBe('cursor-after-replay');
      expect(result.page.has_more).toBe(true);
    });
  });
});
