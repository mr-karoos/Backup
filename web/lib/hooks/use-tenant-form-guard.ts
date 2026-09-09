'use client';

import { useEffect, useState, useRef } from 'react';
import { useRouter } from 'next/navigation';
import { useAuth } from '@/lib/auth/auth-context';
import { useToast } from '@/lib/toast/toast-context';

export interface TenantFormGuardOptions {
  onTenantChange?: () => void;
  onTenantChanged?: () => void;
  fallbackPath?: string;
  isDirty?: boolean;
}

/**
 * Guards open forms against cross-tenant state pollution.
 * If the active organization changes while a mutation form is mounted,
 * the form state is invalidated, sensitive memory is cleared, and the user is redirected safely.
 */
export function useTenantFormGuard(options: TenantFormGuardOptions = {}) {
  const { activeOrgId } = useAuth();
  const router = useRouter();
  const { toast } = useToast();
  const [initialOrgId] = useState<string | null>(activeOrgId);
  const optionsRef = useRef(options);
  useEffect(() => {
    optionsRef.current = options;
  });

  useEffect(() => {
    // If activeOrgId changed away from initialOrgId at mount
    if (initialOrgId && activeOrgId && activeOrgId !== initialOrgId) {
      toast({
        title: 'Organization context switched',
        description: 'Open form was reset to prevent cross-tenant operations.',
        variant: 'destructive',
      });

      if (optionsRef.current.onTenantChange) {
        optionsRef.current.onTenantChange();
      }
      if (optionsRef.current.onTenantChanged) {
        optionsRef.current.onTenantChanged();
      }

      if (optionsRef.current.fallbackPath) {
        router.push(optionsRef.current.fallbackPath);
      }
    }
  }, [activeOrgId, router, toast, initialOrgId]);

  return {
    isCurrentOrg: initialOrgId === activeOrgId,
    initialOrgId,
  };
}
