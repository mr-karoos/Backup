'use client';

import * as React from 'react';
import Link from 'next/link';
import { useParams, useRouter } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { usePermissions } from '@/lib/auth/permissions';
import { useUpdateStorageTarget, useDeleteStorageTarget } from '@/lib/api/mutations';
import { type StorageTargetResponse, type UpdateStorageTargetRequest } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { FormField } from '@/components/ui/form-field';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog';
import { formatDate, getStatusBadgeVariant, formatStorageTargetType } from '@/lib/format/formatters';
import { ArrowLeft, HardDrive, Cloud, Server, ShieldCheck, Check, Pencil, Trash2 } from 'lucide-react';

export default function StorageTargetDetailPage() {
  const params = useParams();
  const id = params?.id as string;
  const router = useRouter();
  const { activeOrgId } = useAuth();
  const { canManageStorage } = usePermissions();

  const [editDialogOpen, setEditDialogOpen] = React.useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = React.useState(false);

  // Edit form state
  const [editName, setEditName] = React.useState('');
  const [editBucket, setEditBucket] = React.useState('');
  const [editRegion, setEditRegion] = React.useState('');
  const [editEndpoint, setEditEndpoint] = React.useState('');
  const [editForcePathStyle, setEditForcePathStyle] = React.useState(false);

  const updateTarget = useUpdateStorageTarget();
  const deleteTarget = useDeleteStorageTarget();

  const { data, isLoading, isError, error, refetch } = useQuery<StorageTargetResponse>({
    queryKey: activeOrgId && id ? queryKeys.org(activeOrgId).storageTargets.detail(id) : ['disabled'],
    queryFn: () => apiClient.get<StorageTargetResponse>(`/storage-targets/${id}`),
    enabled: !!activeOrgId && !!id,
  });

  const handleOpenEdit = () => {
    if (!data) return;
    setEditName(data.name);
    if (data.s3_config) {
      setEditBucket(data.s3_config.bucket);
      setEditRegion(data.s3_config.region || '');
      setEditEndpoint(data.s3_config.endpoint || '');
      setEditForcePathStyle(data.s3_config.force_path_style ?? false);
    }
    setEditDialogOpen(true);
  };

  const handleUpdate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!data) return;

    const payload: UpdateStorageTargetRequest = {
      name: editName.trim() || undefined,
    };

    if (data.type === 's3' || data.type === 's3_compatible') {
      payload.s3_config = {
        bucket: editBucket.trim(),
        region: editRegion.trim(),
        endpoint: editEndpoint.trim(),
        force_path_style: editForcePathStyle,
      };
    }

    await updateTarget.mutateAsync({ id, data: payload });
    setEditDialogOpen(false);
    refetch();
  };

  const handleDelete = async () => {
    await deleteTarget.mutateAsync(id);
    setDeleteDialogOpen(false);
    router.push('/storage');
  };

  if (isLoading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }

  if (isError || !data) {
    return (
      <div className="space-y-6">
        <Link href="/storage">
          <Button variant="ghost" size="sm" className="gap-2">
            <ArrowLeft className="h-4 w-4" />
            Back to Storage Targets
          </Button>
        </Link>
        <ErrorState
          title="Could not load storage target details"
          error={error}
          onRetry={() => refetch()}
        />
      </div>
    );
  }

  const { label, variant } = getStatusBadgeVariant(data.status);
  const isCloud = data.type === 's3' || data.type === 's3_compatible';

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-2">
        <Link href="/storage" className="w-fit">
          <Button variant="ghost" size="sm" className="gap-2 -ml-2 text-muted-foreground hover:text-foreground">
            <ArrowLeft className="h-4 w-4" />
            Back to Storage Targets
          </Button>
        </Link>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
              {isCloud ? <Cloud className="h-5 w-5 text-sky-500" /> : <HardDrive className="h-5 w-5" />}
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h1 className="text-2xl font-bold tracking-tight text-foreground">{data.name}</h1>
                {data.is_default && (
                  <Badge variant="secondary" className="gap-1 text-xs">
                    <Check className="h-3 w-3" />
                    Default Destination
                  </Badge>
                )}
                <Badge variant={variant} className="capitalize text-sm px-2.5 py-0.5">
                  {label}
                </Badge>
              </div>
              <p className="text-xs font-mono text-muted-foreground mt-0.5">ID: {data.id}</p>
            </div>
          </div>

          {/* Action Buttons */}
          {canManageStorage && (
            <div className="flex items-center gap-2">
              {data.type !== 'local' && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleOpenEdit}
                  className="gap-1.5"
                >
                  <Pencil className="h-4 w-4" />
                  Edit Target
                </Button>
              )}

              {!data.is_default && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setDeleteDialogOpen(true)}
                  className="gap-1.5 text-rose-500 hover:text-rose-400 hover:bg-rose-950/20"
                >
                  <Trash2 className="h-4 w-4" />
                  Delete
                </Button>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Detail Cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* Core Attributes Card */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              <Server className="h-4 w-4 text-muted-foreground" />
              Target Attributes
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Target Type</span>
              <span className="font-medium text-foreground">{formatStorageTargetType(data.type)}</span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Operating Status</span>
              <span className="font-medium capitalize text-foreground">{data.status}</span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Default Allocation</span>
              <span className="font-medium text-foreground">
                {data.is_default ? 'Assigned as default for new plans' : 'Secondary destination'}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <span className="text-muted-foreground">Configured At</span>
              <span className="font-medium text-foreground">{formatDate(data.created_at)}</span>
            </div>
          </CardContent>
        </Card>

        {/* Cloud / Destination Specs */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              <ShieldCheck className="h-4 w-4 text-muted-foreground" />
              Destination Configuration
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            {data.s3_config ? (
              <>
                <div className="grid grid-cols-2 gap-2 border-b pb-3">
                  <span className="text-muted-foreground">S3 Bucket</span>
                  <span className="font-mono font-medium text-foreground">
                    {data.s3_config.bucket}
                  </span>
                </div>
                <div className="grid grid-cols-2 gap-2 border-b pb-3">
                  <span className="text-muted-foreground">Region</span>
                  <span className="font-mono text-foreground">{data.s3_config.region || 'default'}</span>
                </div>
                <div className="grid grid-cols-2 gap-2 border-b pb-3">
                  <span className="text-muted-foreground">Endpoint</span>
                  <span className="font-mono text-xs text-foreground truncate">
                    {data.s3_config.endpoint || 'AWS Default'}
                  </span>
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <span className="text-muted-foreground">Path Style</span>
                  <span className="font-mono text-foreground">
                    {data.s3_config.force_path_style ? 'Force Path Style' : 'Virtual Hosted'}
                  </span>
                </div>
              </>
            ) : isCloud ? (
              <div className="py-4 space-y-2 text-muted-foreground text-xs">
                <p className="font-medium text-foreground">S3-Compatible Object Storage Target</p>
                <p>
                  Artifacts are streamed directly to external S3-compatible cloud storage. Bucket and endpoint parameters are managed securely via platform configuration.
                </p>
              </div>
            ) : (
              <div className="py-4 space-y-2 text-muted-foreground text-xs">
                <p className="font-medium text-foreground">Platform-Managed Local Storage Target</p>
                <p>
                  Artifacts are isolated and stored within platform-managed secure volumes. Physical disk paths and server mounts remain strictly concealed in compliance with security guidelines.
                </p>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Edit Storage Target Modal */}
      <Dialog open={editDialogOpen} onOpenChange={setEditDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Pencil className="h-5 w-5 text-primary" />
              Edit Storage Target
            </DialogTitle>
            <DialogDescription>
              Update configuration parameters for {data.name}.
            </DialogDescription>
          </DialogHeader>

          <form onSubmit={handleUpdate} className="space-y-4 py-2">
            <FormField label="Target Name" htmlFor="edit-target-name" required>
              <input
                id="edit-target-name"
                type="text"
                value={editName}
                onChange={(e) => setEditName(e.target.value)}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </FormField>

            {isCloud && (
              <>
                <FormField label="Bucket Name" htmlFor="edit-bucket" required>
                  <input
                    id="edit-bucket"
                    type="text"
                    value={editBucket}
                    onChange={(e) => setEditBucket(e.target.value)}
                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </FormField>

                <FormField label="Region" htmlFor="edit-region" required>
                  <input
                    id="edit-region"
                    type="text"
                    value={editRegion}
                    onChange={(e) => setEditRegion(e.target.value)}
                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </FormField>

                {data.type === 's3_compatible' && (
                  <FormField label="Endpoint URL" htmlFor="edit-endpoint">
                    <input
                      id="edit-endpoint"
                      type="text"
                      value={editEndpoint}
                      onChange={(e) => setEditEndpoint(e.target.value)}
                      placeholder="https://..."
                      className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                    />
                  </FormField>
                )}

                <div className="pt-2">
                  <label className="flex items-center gap-2 text-sm text-foreground cursor-pointer">
                    <input
                      type="checkbox"
                      checked={editForcePathStyle}
                      onChange={(e) => setEditForcePathStyle(e.target.checked)}
                      className="rounded border-input text-primary focus:ring-ring"
                    />
                    <span>Force Path Style</span>
                  </label>
                </div>
              </>
            )}

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
                disabled={updateTarget.isPending}
              >
                {updateTarget.isPending ? 'Saving...' : 'Save Changes'}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>

      {/* Delete Confirmation Dialog */}
      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title="Delete Storage Target"
        description="Are you sure you want to delete this storage destination? Any active plans using this target must be updated before deletion."
        objectName={data.name}
        confirmText="Delete Target"
        destructive
        isLoading={deleteTarget.isPending}
        onConfirm={handleDelete}
      />
    </div>
  );
}
