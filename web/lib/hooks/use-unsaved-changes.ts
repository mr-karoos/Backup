'use client';

import { useEffect, useState, useCallback, useRef } from 'react';
import { useRouter } from 'next/navigation';

export interface UseUnsavedChangesOptions {
  message?: string;
}

/**
 * Controlled navigation and window beforeunload guard for dirty form states.
 * Protects against accidental data loss on browser refresh/close and internal navigation.
 */
export function useUnsavedChanges(isDirty: boolean, options: UseUnsavedChangesOptions = {}) {
  const router = useRouter();
  const [bypass, setBypass] = useState(false);
  const bypassRef = useRef(false);
  useEffect(() => {
    bypassRef.current = bypass;
  }, [bypass]);

  const defaultMessage =
    options.message || 'You have unsaved changes. Are you sure you want to discard them and leave?';

  // Browser reload / tab close guard
  useEffect(() => {
    const handleBeforeUnload = (e: BeforeUnloadEvent) => {
      if (isDirty && !bypassRef.current) {
        e.preventDefault();
        e.returnValue = '';
      }
    };

    window.addEventListener('beforeunload', handleBeforeUnload);
    return () => {
      window.removeEventListener('beforeunload', handleBeforeUnload);
    };
  }, [isDirty]);

  /**
   * Allows successful form submissions to immediately bypass navigation confirmation.
   */
  const bypassGuard = useCallback(() => {
    setBypass(true);
    bypassRef.current = true;
  }, []);

  /**
   * Safe controlled navigation to be called by Cancel / Back buttons or link clicks.
   * Prompts the user before discarding unsaved state.
   */
  const safeNavigate = useCallback(
    (targetUrl: string, onDiscard?: () => void) => {
      if (!isDirty || bypassRef.current) {
        if (onDiscard) onDiscard();
        router.push(targetUrl);
        return;
      }

      const confirmed = typeof window !== 'undefined' ? window.confirm(defaultMessage) : true;
      if (confirmed) {
        bypassGuard();
        if (onDiscard) onDiscard();
        router.push(targetUrl);
      }
    },
    [isDirty, defaultMessage, router, bypassGuard]
  );

  return {
    isDirty: isDirty && !bypass,
    bypassGuard,
    safeNavigate,
  };
}
