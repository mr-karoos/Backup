import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthProvider, useAuth } from '@/lib/auth/auth-context';

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const Wrapper = ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>{children}</AuthProvider>
    </QueryClientProvider>
  );
  Wrapper.displayName = 'TestQueryAuthWrapper';
  return Wrapper;
}

describe('AuthProvider & useAuth', () => {
  const originalFetch = global.fetch;

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    global.fetch = originalFetch;
  });

  it('handles bootstrap session refresh success', async () => {
    global.fetch = vi.fn().mockImplementation((url: string) => {
      if (url === '/api/v1/auth/refresh') {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () =>
            Promise.resolve({
              data: {
                tokens: {
                  access_token: 'bootstrapped-token',
                  token_type: 'Bearer',
                  expires_in: 900,
                },
              },
            }),
        });
      }

      if (url === '/api/v1/auth/me') {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () =>
            Promise.resolve({
              data: {
                user: {
                  id: 'u-1',
                  email: 'admin@domain.com',
                  full_name: 'Admin User',
                  is_system_admin: true,
                },
                memberships: [
                  {
                    organization_id: 'org-1',
                    organization_name: 'Acme Corp',
                    organization_slug: 'acme',
                    is_default_internal: true,
                    role: 'admin',
                    status: 'active',
                    permissions: ['resource:read', 'resource:write'],
                  },
                ],
              },
            }),
        });
      }

      return Promise.reject(new Error(`Unexpected ${url}`));
    });

    const { result } = renderHook(() => useAuth(), { wrapper: createWrapper() });

    await waitFor(() => {
      expect(result.current.status).toBe('authenticated');
    });

    expect(result.current.user?.email).toBe('admin@domain.com');
    expect(result.current.isSystemAdmin).toBe(true);
    expect(result.current.activeOrgId).toBe('org-1');
    expect(result.current.userRole).toBe('admin');
  });

  it('transitions to unauthenticated on bootstrap refresh failure without error flash', async () => {
    global.fetch = vi.fn().mockImplementation((url: string) => {
      if (url === '/api/v1/auth/refresh') {
        return Promise.resolve({
          ok: false,
          status: 401,
          json: () =>
            Promise.resolve({
              error: { code: 'UNAUTHORIZED', message: 'No active session' },
            }),
        });
      }
      return Promise.reject(new Error(`Unexpected ${url}`));
    });

    const { result } = renderHook(() => useAuth(), { wrapper: createWrapper() });

    await waitFor(() => {
      expect(result.current.status).toBe('unauthenticated');
    });

    expect(result.current.user).toBeNull();
    expect(result.current.activeOrgId).toBeNull();
  });

  it('clears state cleanly on logout', async () => {
    // Initial boot succeeds
    global.fetch = vi.fn().mockImplementation((url: string) => {
      if (url === '/api/v1/auth/refresh') {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () =>
            Promise.resolve({
              data: {
                tokens: {
                  access_token: 'token',
                  token_type: 'Bearer',
                  expires_in: 900,
                },
              },
            }),
        });
      }
      if (url === '/api/v1/auth/me') {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () =>
            Promise.resolve({
              data: {
                user: { id: 'u-1', email: 'test@domain.com', full_name: 'Test', is_system_admin: false },
                memberships: [],
              },
            }),
        });
      }
      if (url === '/api/v1/auth/logout') {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () => Promise.resolve({ data: {} }),
        });
      }
      return Promise.reject(new Error(`Unexpected ${url}`));
    });

    const { result } = renderHook(() => useAuth(), { wrapper: createWrapper() });

    await waitFor(() => {
      expect(result.current.status).toBe('authenticated');
    });

    await act(async () => {
      await result.current.logout();
    });

    expect(result.current.status).toBe('unauthenticated');
    expect(result.current.user).toBeNull();
  });
});
