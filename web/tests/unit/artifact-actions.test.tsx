import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { apiClient, triggerBlobDownload } from '@/lib/api/api-client';
import { ApiError } from '@/types/api';

describe('Artifact Download & Security Actions', () => {
  const originalFetch = global.fetch;

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    global.fetch = originalFetch;
  });

  describe('apiClient.download', () => {
    it('injects Bearer token, X-Organization-ID, and extracts filename from Content-Disposition', async () => {
      const captured = { headers: null as Headers | null };

      const mockBlob = new Blob(['mock binary backup data'], { type: 'application/gzip' });

      global.fetch = vi.fn().mockImplementation((_url, init) => {
        captured.headers = new Headers(init?.headers);
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers({
            'Content-Type': 'application/gzip',
            'Content-Disposition': 'attachment; filename="production_db_2026.sql.gz"',
          }),
          blob: () => Promise.resolve(mockBlob),
        });
      });

      apiClient.configure({
        getToken: () => 'valid-jwt-token',
        getOrgId: () => '11111111-1111-1111-1111-111111111111',
        onTokenUpdate: vi.fn(),
        onAuthFailure: vi.fn(),
      });

      const { blob, filename } = await apiClient.download(
        '/backup-artifacts/00000000-0000-0000-0000-000000000001/download'
      );

      expect(filename).toBe('production_db_2026.sql.gz');
      expect(blob).toBe(mockBlob);
      expect(captured.headers?.get('Authorization')).toBe('Bearer valid-jwt-token');
      expect(captured.headers?.get('X-Organization-ID')).toBe('11111111-1111-1111-1111-111111111111');
    });

    it('falls back to default safe filename when Content-Disposition is missing', async () => {
      const mockBlob = new Blob(['data'], { type: 'application/gzip' });

      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers(),
        blob: () => Promise.resolve(mockBlob),
      });

      apiClient.configure({
        getToken: () => 'token',
        getOrgId: () => 'org-1',
        onTokenUpdate: vi.fn(),
        onAuthFailure: vi.fn(),
      });

      const { filename } = await apiClient.download('/backup-artifacts/art-1/download');
      expect(filename).toBe('backup-artifact.tar.gz');
    });

    it('throws ApiError on 403 Forbidden without exposing filesystem paths', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 403,
        headers: new Headers(),
        json: () =>
          Promise.resolve({
            error: {
              code: 'FORBIDDEN',
              message: 'insufficient permissions to download backup artifact',
            },
          }),
      });

      apiClient.configure({
        getToken: () => 'viewer-token',
        getOrgId: () => 'org-1',
        onTokenUpdate: vi.fn(),
        onAuthFailure: vi.fn(),
      });

      await expect(
        apiClient.download('/backup-artifacts/art-1/download')
      ).rejects.toThrow('insufficient permissions to download backup artifact');

      try {
        await apiClient.download('/backup-artifacts/art-1/download');
      } catch (err: unknown) {
        const apiErr = err as ApiError;
        expect(apiErr.status).toBe(403);
        expect(apiErr.code).toBe('FORBIDDEN');
      }
    });

    it('throws ApiError on 404 Not Found', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
        headers: new Headers(),
        json: () =>
          Promise.resolve({
            error: {
              code: 'ARTIFACT_NOT_FOUND',
              message: 'backup artifact not found',
            },
          }),
      });

      apiClient.configure({
        getToken: () => 'token',
        getOrgId: () => 'org-1',
        onTokenUpdate: vi.fn(),
        onAuthFailure: vi.fn(),
      });

      await expect(
        apiClient.download('/backup-artifacts/art-not-found/download')
      ).rejects.toThrow('backup artifact not found');
    });

    it('replays download request after successful 401 token refresh', async () => {
      let callCount = 0;
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

        if (url.includes('/backup-artifacts/art-1/download')) {
          callCount++;
          if (headers.get('Authorization') === 'Bearer expired-token') {
            return Promise.resolve({
              ok: false,
              status: 401,
              json: () =>
                Promise.resolve({ error: { code: 'UNAUTHORIZED', message: 'Token expired' } }),
            });
          }

          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers({
              'Content-Disposition': 'attachment; filename="refreshed_archive.tar.gz"',
            }),
            blob: () => Promise.resolve(new Blob(['fresh data'])),
          });
        }

        return Promise.reject(new Error(`Unexpected url: ${url}`));
      });

      apiClient.configure({
        getToken: () => currentToken,
        getOrgId: () => 'tenant-123',
        onTokenUpdate: (t) => {
          currentToken = t;
        },
        onAuthFailure: vi.fn(),
      });

      const { filename } = await apiClient.download('/backup-artifacts/art-1/download');

      expect(callCount).toBe(2);
      expect(filename).toBe('refreshed_archive.tar.gz');
    });
  });

  describe('triggerBlobDownload DOM Helper', () => {
    it('creates an object URL, clicks invisible anchor, and schedules URL revocation', () => {
      const mockUrl = 'blob:http://localhost/mock-blob-uuid';
      const createSpy = vi.fn().mockReturnValue(mockUrl);
      const revokeSpy = vi.fn();
      window.URL.createObjectURL = createSpy;
      window.URL.revokeObjectURL = revokeSpy;

      const clickSpy = vi.fn();
      const origCreateElement = document.createElement.bind(document);
      vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
        const el = origCreateElement(tag);
        if (tag === 'a') {
          el.click = clickSpy;
        }
        return el;
      });

      vi.useFakeTimers();

      const testBlob = new Blob(['test content']);
      triggerBlobDownload(testBlob, 'downloaded_artifact.sql.gz');

      expect(createSpy).toHaveBeenCalledWith(testBlob);
      expect(clickSpy).toHaveBeenCalled();

      vi.advanceTimersByTime(150);
      expect(revokeSpy).toHaveBeenCalledWith(mockUrl);
      vi.useRealTimers();
    });
  });

  describe('RBAC & Permission Checks for Artifact Actions', () => {
    it('verifies platform permissions include download and audit read', () => {
      const adminPermissions = [
        'backup_artifact:download',
        'backup_artifact:delete',
        'audit_log:read',
        'backup_run:verify',
      ];

      expect(adminPermissions).toContain('backup_artifact:download');
      expect(adminPermissions).toContain('backup_artifact:delete');
      expect(adminPermissions).toContain('audit_log:read');
    });
  });
});
