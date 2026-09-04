'use client';

import React, { createContext, useContext, useEffect, useState, useRef, useCallback, useMemo } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api/api-client';
import {
  type UserSummary,
  type MembershipSummary,
  type LoginResponseData,
  type MeResponseData,
  type RefreshResponseData,
  type OrgRole,
} from '@/types/auth';
import { queryKeys } from '@/lib/query/query-client';

export type AuthStatus = 'booting' | 'authenticated' | 'unauthenticated';

export interface AuthContextType {
  status: AuthStatus;
  user: UserSummary | null;
  memberships: MembershipSummary[];
  activeOrgId: string | null;
  activeMembership: MembershipSummary | null;
  isSystemAdmin: boolean;
  userRole: OrgRole | null;
  login: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  switchOrganization: (orgId: string) => void;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<AuthStatus>('booting');
  const [accessToken, setAccessToken] = useState<string | null>(null);
  const [user, setUser] = useState<UserSummary | null>(null);
  const [memberships, setMemberships] = useState<MembershipSummary[]>([]);
  const [activeOrgId, setActiveOrgId] = useState<string | null>(null);

  const accessTokenRef = useRef<string | null>(null);
  const activeOrgIdRef = useRef<string | null>(null);

  useEffect(() => {
    accessTokenRef.current = accessToken;
    activeOrgIdRef.current = activeOrgId;
  }, [accessToken, activeOrgId]);

  // Active membership computed from activeOrgId
  const activeMembership = useMemo(() => {
    if (!activeOrgId || memberships.length === 0) return null;
    return memberships.find((m) => m.organization_id === activeOrgId) || null;
  }, [activeOrgId, memberships]);

  const userRole = activeMembership?.role || null;
  const isSystemAdmin = user?.is_system_admin || false;

  // Clear session state cleanly
  const clearSessionState = useCallback(() => {
    setAccessToken(null);
    setUser(null);
    setMemberships([]);
    setActiveOrgId(null);
    setStatus('unauthenticated');
    queryClient.clear();
  }, [queryClient]);

  // Configure apiClient callbacks on mount
  useEffect(() => {
    apiClient.configure({
      getToken: () => accessTokenRef.current,
      getOrgId: () => activeOrgIdRef.current,
      onTokenUpdate: (newToken: string) => {
        setAccessToken(newToken);
      },
      onAuthFailure: () => {
        clearSessionState();
      },
    });
  }, [clearSessionState]);

  // Initial Session Boot
  useEffect(() => {
    let isCancelled = false;

    async function bootstrap() {
      try {
        // Attempt silent refresh via HttpOnly cookie
        const refreshData = await apiClient.post<RefreshResponseData>(
          '/auth/refresh',
          undefined,
          { skipAuth: true, skipOrgHeader: true }
        );

        if (isCancelled) return;

        const newAccessToken = refreshData.tokens.access_token;
        setAccessToken(newAccessToken);
        accessTokenRef.current = newAccessToken;

        // Fetch current user and memberships
        const meData = await apiClient.get<MeResponseData>('/auth/me', {
          headers: {
            Authorization: `Bearer ${newAccessToken}`,
          },
          skipOrgHeader: true,
        });

        if (isCancelled) return;

        setUser(meData.user);
        setMemberships(meData.memberships || []);

        // Pick default org or first available
        if (meData.memberships && meData.memberships.length > 0) {
          const defaultInternal = meData.memberships.find((m) => m.is_default_internal);
          const chosenOrgId = defaultInternal
            ? defaultInternal.organization_id
            : meData.memberships[0]!.organization_id;
          setActiveOrgId(chosenOrgId);
          activeOrgIdRef.current = chosenOrgId;
        }

        setStatus('authenticated');
      } catch {
        if (!isCancelled) {
          clearSessionState();
        }
      }
    }

    bootstrap();

    return () => {
      isCancelled = true;
    };
  }, [clearSessionState]);

  // Login handler
  const login = useCallback(
    async (email: string, password: string) => {
      const loginData = await apiClient.post<LoginResponseData>(
        '/auth/login',
        { email, password },
        { skipAuth: true, skipOrgHeader: true }
      );

      const token = loginData.tokens.access_token;
      setAccessToken(token);
      accessTokenRef.current = token;
      setUser(loginData.user);

      // Immediately load memberships via /auth/me
      const meData = await apiClient.get<MeResponseData>('/auth/me', {
        headers: { Authorization: `Bearer ${token}` },
        skipOrgHeader: true,
      });

      const memberList = meData.memberships || [];
      setMemberships(memberList);

      let targetOrgId: string | null = loginData.default_organization_id || null;
      if (!targetOrgId && memberList.length > 0) {
        const defaultInternal = memberList.find((m) => m.is_default_internal);
        targetOrgId = defaultInternal ? defaultInternal.organization_id : memberList[0]!.organization_id;
      }

      setActiveOrgId(targetOrgId);
      activeOrgIdRef.current = targetOrgId;
      setStatus('authenticated');
    },
    []
  );

  // Logout handler
  const logout = useCallback(async () => {
    try {
      await apiClient.post('/auth/logout', undefined, { skipOrgHeader: true });
    } catch {
      // Ignore network failure on logout; client state is wiped unconditionally
    } finally {
      clearSessionState();
    }
  }, [clearSessionState]);

  // Switch Organization
  const switchOrganization = useCallback(
    (newOrgId: string) => {
      if (newOrgId === activeOrgIdRef.current) return;

      // 1. Cancel in-flight tenant queries for old org
      if (activeOrgIdRef.current) {
        queryClient.cancelQueries({ queryKey: queryKeys.org(activeOrgIdRef.current).all });
        queryClient.removeQueries({ queryKey: queryKeys.org(activeOrgIdRef.current).all });
      }

      // 2. Set new active org
      setActiveOrgId(newOrgId);
      activeOrgIdRef.current = newOrgId;
    },
    [queryClient]
  );

  const value = useMemo(
    () => ({
      status,
      user,
      memberships,
      activeOrgId,
      activeMembership,
      isSystemAdmin,
      userRole,
      login,
      logout,
      switchOrganization,
    }),
    [
      status,
      user,
      memberships,
      activeOrgId,
      activeMembership,
      isSystemAdmin,
      userRole,
      login,
      logout,
      switchOrganization,
    ]
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextType {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
}
