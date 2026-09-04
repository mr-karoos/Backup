import { ApiError, type ApiResponseEnvelope, type ApiErrorEnvelope } from '@/types/api';
import { tokenRefreshManager } from '@/lib/auth/token-refresh';

export interface RequestOptions extends RequestInit {
  skipAuth?: boolean;
  skipOrgHeader?: boolean;
  _isRetry?: boolean;
}

export type TokenProvider = () => string | null;
export type OrgIdProvider = () => string | null;
export type OnTokenUpdate = (token: string) => void;
export type OnAuthFailure = () => void;

class ApiClient {
  private tokenProvider: TokenProvider = () => null;
  private orgIdProvider: OrgIdProvider = () => null;
  private onTokenUpdate: OnTokenUpdate = () => {};
  private onAuthFailure: OnAuthFailure = () => {};

  public configure(config: {
    getToken: TokenProvider;
    getOrgId: OrgIdProvider;
    onTokenUpdate: OnTokenUpdate;
    onAuthFailure: OnAuthFailure;
  }): void {
    this.tokenProvider = config.getToken;
    this.orgIdProvider = config.getOrgId;
    this.onTokenUpdate = config.onTokenUpdate;
    this.onAuthFailure = config.onAuthFailure;
  }

  /**
   * Identifies whether an endpoint is public/global vs tenant-scoped.
   */
  private isTenantScoped(endpoint: string): boolean {
    const cleanEndpoint = endpoint.startsWith('/') ? endpoint : `/${endpoint}`;

    // Public / Non-tenant endpoints
    if (
      cleanEndpoint.startsWith('/auth/login') ||
      cleanEndpoint.startsWith('/auth/refresh') ||
      cleanEndpoint.startsWith('/health') ||
      cleanEndpoint === '/organizations' // Global list of user orgs
    ) {
      return false;
    }

    return true;
  }

  public async request<T>(endpoint: string, options: RequestOptions = {}): Promise<T> {
    const { skipAuth = false, skipOrgHeader = false, _isRetry = false, ...fetchOptions } = options;

    const cleanEndpoint = endpoint.startsWith('/') ? endpoint : `/${endpoint}`;
    const url = `/api/v1${cleanEndpoint}`;

    const headers = new Headers(fetchOptions.headers || {});

    // Add Content-Type for mutation methods if body is present
    if (fetchOptions.body && !headers.has('Content-Type')) {
      headers.set('Content-Type', 'application/json');
    }

    // Inject in-memory Bearer token
    if (!skipAuth) {
      const token = this.tokenProvider();
      if (token) {
        headers.set('Authorization', `Bearer ${token}`);
      }
    }

    // Inject X-Organization-ID for tenant-scoped endpoints
    if (!skipOrgHeader && this.isTenantScoped(cleanEndpoint)) {
      const orgId = this.orgIdProvider();
      if (orgId) {
        headers.set('X-Organization-ID', orgId);
      }
    }

    let response: Response;
    try {
      response = await fetch(url, {
        ...fetchOptions,
        headers,
      });
    } catch (networkErr: unknown) {
      if (networkErr instanceof DOMException && networkErr.name === 'AbortError') {
        throw networkErr;
      }
      throw new ApiError(0, 'NETWORK_ERROR', 'Could not connect to Backup Platform server.');
    }

    // Handle 401 Unauthorized with single-flight refresh (unless already a retry or an auth endpoint)
    if (
      response.status === 401 &&
      !_isRetry &&
      cleanEndpoint !== '/auth/login' &&
      cleanEndpoint !== '/auth/refresh'
    ) {
      try {
        await tokenRefreshManager.executeRefresh(async () => {
          const refreshRes = await fetch('/api/v1/auth/refresh', {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
            },
          });

          if (!refreshRes.ok) {
            throw new ApiError(refreshRes.status, 'REFRESH_FAILED', 'Session expired');
          }

          const refreshData = (await refreshRes.json()) as ApiResponseEnvelope<{
            tokens: { access_token: string };
          }>;

          const newToken = refreshData.data.tokens.access_token;
          this.onTokenUpdate(newToken);
          return newToken;
        });

        // Replay the original request ONCE with new access token
        return this.request<T>(endpoint, {
          ...options,
          _isRetry: true,
        });
      } catch (refreshErr) {
        tokenRefreshManager.reset();
        this.onAuthFailure();
        throw refreshErr;
      }
    }

    // Handle non-2xx responses
    if (!response.ok) {
      let errorCode = 'UNKNOWN_ERROR';
      let errorMessage = `HTTP error ${response.status}`;
      let errorDetails: unknown = undefined;
      let requestId: string | undefined = undefined;

      try {
        const errorJson = (await response.json()) as ApiErrorEnvelope;
        if (errorJson.error) {
          errorCode = errorJson.error.code || errorCode;
          errorMessage = errorJson.error.message || errorMessage;
          errorDetails = errorJson.error.details;
        }
        requestId = errorJson.request_id;
      } catch {
        // Response was not JSON
        errorMessage = response.statusText || errorMessage;
      }

      throw new ApiError(response.status, errorCode, errorMessage, errorDetails, requestId);
    }

    // Handle 204 No Content
    if (response.status === 204) {
      return null as T;
    }

    // Parse JSON response
    const json = await response.json();

    // Check if wrapped in standard ApiResponseEnvelope
    if (json && typeof json === 'object' && 'data' in json) {
      return (json as ApiResponseEnvelope<T>).data;
    }

    // Direct JSON (e.g. /health endpoint)
    return json as T;
  }

  public get<T>(endpoint: string, options?: RequestOptions): Promise<T> {
    return this.request<T>(endpoint, { ...options, method: 'GET' });
  }

  public post<T>(endpoint: string, data?: unknown, options?: RequestOptions): Promise<T> {
    return this.request<T>(endpoint, {
      ...options,
      method: 'POST',
      body: data !== undefined ? JSON.stringify(data) : undefined,
    });
  }

  public put<T>(endpoint: string, data?: unknown, options?: RequestOptions): Promise<T> {
    return this.request<T>(endpoint, {
      ...options,
      method: 'PUT',
      body: data !== undefined ? JSON.stringify(data) : undefined,
    });
  }

  public delete<T>(endpoint: string, options?: RequestOptions): Promise<T> {
    return this.request<T>(endpoint, { ...options, method: 'DELETE' });
  }
}

export const apiClient = new ApiClient();
