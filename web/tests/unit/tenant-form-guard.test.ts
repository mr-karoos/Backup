import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useTenantFormGuard } from '@/lib/hooks/use-tenant-form-guard';

let mockActiveOrgId = 'org-1';
const mockToast = vi.fn();
const mockPush = vi.fn();

vi.mock('@/lib/auth/auth-context', () => ({
  useAuth: () => ({
    activeOrgId: mockActiveOrgId,
  }),
}));

vi.mock('@/lib/toast/toast-context', () => ({
  useToast: () => ({
    toast: mockToast,
  }),
}));

vi.mock('next/navigation', () => ({
  useRouter: () => ({
    push: mockPush,
  }),
}));

describe('useTenantFormGuard', () => {
  beforeEach(() => {
    mockActiveOrgId = 'org-1';
    vi.clearAllMocks();
  });

  it('initializes with current activeOrgId and does not trigger on mount', () => {
    const onTenantChanged = vi.fn();
    const { result } = renderHook(() =>
      useTenantFormGuard({ onTenantChanged, fallbackPath: '/dashboard' })
    );

    expect(result.current.isCurrentOrg).toBe(true);
    expect(result.current.initialOrgId).toBe('org-1');
    expect(onTenantChanged).not.toHaveBeenCalled();
    expect(mockToast).not.toHaveBeenCalled();
    expect(mockPush).not.toHaveBeenCalled();
  });

  it('triggers onTenantChanged and toast when activeOrgId changes', () => {
    const onTenantChanged = vi.fn();
    const { rerender } = renderHook(() =>
      useTenantFormGuard({ onTenantChanged, fallbackPath: '/dashboard' })
    );

    // Change org
    mockActiveOrgId = 'org-2';
    rerender();

    expect(mockToast).toHaveBeenCalledWith(
      expect.objectContaining({
        title: 'Organization context switched',
        variant: 'destructive',
      })
    );
    expect(onTenantChanged).toHaveBeenCalledTimes(1);
    expect(mockPush).toHaveBeenCalledWith('/dashboard');
  });
});
