/**
 * Single-flight token refresh coordinator.
 * Guarantees that when multiple parallel requests receive 401, exactly ONE
 * POST /api/v1/auth/refresh is dispatched, while all concurrent requests await
 * the same in-flight promise and replay once upon resolution.
 */

export type RefreshFn = () => Promise<string>;

export class TokenRefreshManager {
  private inFlightPromise: Promise<string> | null = null;

  /**
   * Acquire a fresh access token via a single-flight refresh operation.
   * If a refresh is already in flight, all subsequent callers receive the same Promise.
   */
  public async executeRefresh(refreshFn: RefreshFn): Promise<string> {
    if (this.inFlightPromise) {
      return this.inFlightPromise;
    }

    this.inFlightPromise = (async () => {
      try {
        const token = await refreshFn();
        return token;
      } finally {
        this.inFlightPromise = null;
      }
    })();

    return this.inFlightPromise;
  }

  /**
   * Checks whether a refresh request is currently in flight.
   */
  public isRefreshing(): boolean {
    return this.inFlightPromise !== null;
  }

  /**
   * Clears any current promise reference (e.g. on explicit logout or hard reset).
   */
  public reset(): void {
    this.inFlightPromise = null;
  }
}

export const tokenRefreshManager = new TokenRefreshManager();
