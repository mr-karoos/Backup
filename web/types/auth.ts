/**
 * Authentication and identity types matching internal/identity/httpapi contracts.
 */

export interface TokenBundle {
  access_token: string;
  token_type: string;
  expires_in: number;
}

export interface UserSummary {
  id: string;
  email: string;
  full_name: string;
  is_system_admin: boolean;
  created_at?: string;
  status?: string;
}

export type OrgRole = 'admin' | 'member' | 'viewer';

export interface MembershipSummary {
  organization_id: string;
  organization_name: string;
  organization_slug: string;
  is_default_internal: boolean;
  role: OrgRole;
  status: string;
  permissions: string[];
}

export interface LoginResponseData {
  user: UserSummary;
  tokens: TokenBundle;
  default_organization_id?: string | null;
}

export interface RefreshResponseData {
  tokens: TokenBundle;
}

export interface MeResponseData {
  user: UserSummary;
  memberships: MembershipSummary[];
}

export interface OrganizationSummary {
  id: string;
  name: string;
  slug: string;
  is_default_internal: boolean;
  status: string;
  user_role: OrgRole;
  created_at: string;
}

export interface OrganizationDetail {
  id: string;
  name: string;
  slug: string;
  is_default_internal: boolean;
  status: string;
  metadata?: Record<string, unknown> | null;
  created_at: string;
  updated_at: string;
}
