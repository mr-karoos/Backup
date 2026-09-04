import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthProvider, useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';

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

  it('validates organization membership on switchOrganization and rejects unauthorized or inactive tenants', async () => {
    global.fetch = vi.fn().mockImplementation((url: string) => {
      if (url === '/api/v1/auth/refresh') {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () =>
            Promise.resolve({
              data: {
                tokens: {
                  access_token: 'token-multi-org',
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
                  id: 'u-10',
                  email: 'multi@domain.com',
                  full_name: 'Multi Org User',
                  is_system_admin: false,
                },
                memberships: [
                  {
                    organization_id: 'org-alpha',
                    organization_name: 'Org Alpha',
                    organization_slug: 'alpha',
                    is_default_internal: true,
                    role: 'admin',
                    status: 'active',
                    permissions: ['resource:read'],
                  },
                  {
                    organization_id: 'org-beta',
                    organization_name: 'Org Beta',
                    organization_slug: 'beta',
                    is_default_internal: false,
                    role: 'viewer',
                    status: 'active',
                    permissions: ['resource:read'],
                  },
                  {
                    organization_id: 'org-gamma-suspended',
                    organization_name: 'Org Gamma',
                    organization_slug: 'gamma',
                    is_default_internal: false,
                    role: 'viewer',
                    status: 'disabled',
                    permissions: [],
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

    expect(result.current.activeOrgId).toBe('org-alpha');
    expect(result.current.userRole).toBe('admin');

    // 1. Switch to valid active org (org-beta)
    let switchResult: boolean = false;
    act(() => {
      switchResult = result.current.switchOrganization('org-beta');
    });

    expect(switchResult).toBe(true);
    expect(result.current.activeOrgId).toBe('org-beta');
    expect(result.current.userRole).toBe('viewer');

    // 2. Attempt switch to completely unknown org (org-unknown)
    act(() => {
      switchResult = result.current.switchOrganization('org-unknown');
    });

    expect(switchResult).toBe(false);
    // Active org must remain org-beta
    expect(result.current.activeOrgId).toBe('org-beta');
    expect(result.current.userRole).toBe('viewer');

    // 3. Attempt switch to suspended/disabled org (org-gamma-suspended)
    act(() => {
      switchResult = result.current.switchOrganization('org-gamma-suspended');
    });

    expect(switchResult).toBe(false);
    expect(result.current.activeOrgId).toBe('org-beta');
  });

  it('synchronously updates token ref via onTokenUpdate to eliminate replay races', async () => {
    global.fetch = vi.fn().mockImplementation((url: string) => {
      if (url === '/api/v1/auth/refresh') {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () =>
            Promise.resolve({
              data: {
                tokens: {
                  access_token: 'initial-token',
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
                user: { id: 'u-1', email: 'user@domain.com', full_name: 'User', is_system_admin: false },
                memberships: [
                  {
                    organization_id: 'org-1',
                    organization_name: 'Org 1',
                    organization_slug: 'org-1',
                    is_default_internal: true,
                    role: 'admin',
                    status: 'active',
                    permissions: [],
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

    // apiClient's tokenProvider was configured by AuthProvider and points to accessTokenRef.current
    expect((apiClient as unknown as { tokenProvider: () => string | null }).tokenProvider()).toBe('initial-token');

    // When a single-flight refresh resolves, onTokenUpdate is called
    act(() => {
      (apiClient as unknown as { onTokenUpdate: (t: string) => void }).onTokenUpdate('new-refreshed-token');
    });

    // accessTokenRef.current is synchronously updated to new token, preventing stale token replay races
    expect((apiClient as unknown as { tokenProvider: () => string | null }).tokenProvider()).toBe('new-refreshed-token');

    // On session expiration / auth failure, state is wiped cleanly
    act(() => {
      (apiClient as unknown as { onAuthFailure: () => void }).onAuthFailure();
    });

    await waitFor(() => {
      expect(result.current.status).toBe('unauthenticated');
    });
    expect((apiClient as unknown as { tokenProvider: () => string | null }).tokenProvider()).toBeNull();
    expect(result.current.user).toBeNull();
  });
});
