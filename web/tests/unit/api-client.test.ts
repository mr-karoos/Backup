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
});
