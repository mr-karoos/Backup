import { describe, it, expect, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useUnsavedChanges } from '@/lib/hooks/use-unsaved-changes';

// Mock useRouter
const mockPush = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({
    push: mockPush,
    replace: vi.fn(),
    prefetch: vi.fn(),
  }),
}));

describe('Unsaved Guard Ordering & Bypass Logic', () => {
  it('preserves dirty guard when mutation rejects (bypassGuard is not called)', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);

    const { result } = renderHook(() => useUnsavedChanges(true));

    expect(result.current.isDirty).toBe(true);

    // Simulate form submit handler where mutation rejects
    const simulatedSubmit = async (mutationFn: () => Promise<void>) => {
      try {
        await mutationFn();
        result.current.bypassGuard();
      } catch {
        // Handled by onError; form remains dirty
      }
    };

    // Run failing mutation
    await act(async () => {
      await simulatedSubmit(async () => {
        throw new Error('500 Internal Server Error');
      });
    });

    // Guard MUST still be active
    expect(result.current.isDirty).toBe(true);

    // Attempting navigation should trigger confirmation prompt
    act(() => {
      result.current.safeNavigate('/resources');
    });

    expect(confirmSpy).toHaveBeenCalledTimes(1);
    expect(mockPush).not.toHaveBeenCalled();
  });

  it('bypasses dirty guard when mutation succeeds (bypassGuard is called after await)', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
    mockPush.mockClear();

    const { result } = renderHook(() => useUnsavedChanges(true));

    expect(result.current.isDirty).toBe(true);

    // Simulate form submit handler where mutation succeeds
    const simulatedSubmit = async (mutationFn: () => Promise<void>) => {
      try {
        await mutationFn();
        result.current.bypassGuard();
      } catch {
        // Handled by onError
      }
    };

    // Run successful mutation
    await act(async () => {
      await simulatedSubmit(async () => {
        // Success
      });
    });

    // Guard MUST now be bypassed
    expect(result.current.isDirty).toBe(false);

    // Attempting navigation should NOT prompt confirmation
    act(() => {
      result.current.safeNavigate('/resources');
    });

    expect(confirmSpy).not.toHaveBeenCalled();
    expect(mockPush).toHaveBeenCalledWith('/resources');
  });
});
