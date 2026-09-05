'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { usePermissions } from '@/lib/auth/permissions';
import { useUpdateOrganization } from '@/lib/api/mutations';
import { type OrganizationDetail } from '@/types/auth';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { FormField } from '@/components/ui/form-field';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import { formatDate } from '@/lib/format/formatters';
import { Building2, UserCheck, Pencil } from 'lucide-react';

export default function OrganizationSettingsPage() {
  const { activeOrgId, activeMembership, userRole } = useAuth();
  const { canUpdateOrganization } = usePermissions();
  const updateOrg = useUpdateOrganization();

  const [editDialogOpen, setEditDialogOpen] = React.useState(false);
  const [orgName, setOrgName] = React.useState('');

  const { data, isLoading, isError, error, refetch } = useQuery<OrganizationDetail>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).settings() : ['disabled'],
    queryFn: () => apiClient.get<OrganizationDetail>(`/organizations/${activeOrgId}`),
    enabled: !!activeOrgId,
  });

  const handleOpenEdit = () => {
    if (!data) return;
    setOrgName(data.name);
    setEditDialogOpen(true);
  };

  const handleUpdate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!data || !orgName.trim()) return;

    await updateOrg.mutateAsync({
      id: data.id,
      data: {
        name: orgName.trim(),
        metadata: data.metadata || {},
      },
    });
    setEditDialogOpen(false);
    refetch();
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">Organization Settings</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Tenant identity, active membership parameters, and operational attributes
          </p>
        </div>
        {canUpdateOrganization && data && (
          <Button
            variant="outline"
            size="sm"
            onClick={handleOpenEdit}
            className="gap-1.5"
          >
            <Pencil className="h-4 w-4" /> Edit Organization
          </Button>
        )}
      </div>

      {isLoading ? (
        <div className="space-y-6">
          <Skeleton className="h-48 w-full" />
          <Skeleton className="h-48 w-full" />
        </div>
      ) : isError || !data ? (
        <ErrorState
          title="Could not load organization details"
          error={error}
          onRetry={() => refetch()}
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          {/* Organization Profile Card */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base font-semibold flex items-center gap-2">
                <Building2 className="h-4 w-4 text-muted-foreground" />
                Tenant Information
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4 text-sm">
              <div className="grid grid-cols-2 gap-2 border-b pb-3">
                <span className="text-muted-foreground">Organization Name</span>
                <span className="font-semibold text-foreground">{data.name}</span>
              </div>
              <div className="grid grid-cols-2 gap-2 border-b pb-3">
                <span className="text-muted-foreground">Unique Slug</span>
                <span className="font-mono text-foreground">{data.slug}</span>
              </div>
              <div className="grid grid-cols-2 gap-2 border-b pb-3">
                <span className="text-muted-foreground">Tenant Status</span>
                <Badge variant="outline" className="capitalize w-fit">
                  {data.status}
                </Badge>
              </div>
              <div className="grid grid-cols-2 gap-2 border-b pb-3">
                <span className="text-muted-foreground">Default Internal Tenant</span>
                <span className="font-medium text-foreground">
                  {data.is_default_internal ? 'Yes' : 'No'}
                </span>
              </div>
              <div className="grid grid-cols-2 gap-2 border-b pb-3">
                <span className="text-muted-foreground">Provisioned Date</span>
                <span className="text-foreground">{formatDate(data.created_at)}</span>
              </div>
              <div className="grid grid-cols-2 gap-2">
                <span className="text-muted-foreground">Organization ID</span>
                <span className="font-mono text-xs text-foreground truncate">{data.id}</span>
              </div>
            </CardContent>
          </Card>

          {/* Caller Membership Role & Permissions */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base font-semibold flex items-center gap-2">
                <UserCheck className="h-4 w-4 text-muted-foreground" />
                Active Membership Scope
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4 text-sm">
              <div className="grid grid-cols-2 gap-2 border-b pb-3">
                <span className="text-muted-foreground">Assigned Role</span>
                <Badge variant="secondary" className="capitalize w-fit">
                  {userRole || 'Member'}
                </Badge>
              </div>
              <div className="grid grid-cols-2 gap-2 border-b pb-3">
                <span className="text-muted-foreground">Membership Status</span>
                <span className="font-medium capitalize text-foreground">
                  {activeMembership?.status || 'Active'}
                </span>
              </div>
              <div>
                <span className="text-xs text-muted-foreground block mb-2">Effective RBAC Permissions:</span>
                {activeMembership?.permissions && activeMembership.permissions.length > 0 ? (
                  <div className="flex flex-wrap gap-1.5 max-h-48 overflow-y-auto p-1">
                    {activeMembership.permissions.map((perm) => (
                      <span
                        key={perm}
                        className="rounded bg-muted px-2 py-0.5 text-xs font-mono text-muted-foreground"
                      >
                        {perm}
                      </span>
                    ))}
                  </div>
                ) : (
                  <span className="text-xs text-muted-foreground italic">
                    Inheriting role-based standard permissions.
                  </span>
                )}
              </div>
            </CardContent>
          </Card>
        </div>
      )}

      {/* Edit Organization Dialog */}
      <Dialog open={editDialogOpen} onOpenChange={setEditDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Building2 className="h-5 w-5 text-primary" />
              Edit Organization Details
            </DialogTitle>
            <DialogDescription>
              Update your organization name. Slugs and internal IDs are permanent.
            </DialogDescription>
          </DialogHeader>

          <form onSubmit={handleUpdate} className="space-y-4 py-2">
            <FormField label="Organization Name" htmlFor="org-name-input" required>
              <input
                id="org-name-input"
                type="text"
                value={orgName}
                onChange={(e) => setOrgName(e.target.value)}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </FormField>

            <div className="flex justify-end gap-3 pt-4 border-t">
              <Button
                type="button"
                variant="outline"
                onClick={() => setEditDialogOpen(false)}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={updateOrg.isPending || !orgName.trim()}
              >
                {updateOrg.isPending ? 'Saving...' : 'Save Changes'}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
