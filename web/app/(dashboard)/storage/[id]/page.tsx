'use client';

import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { type StorageTargetResponse } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import { formatDate, getStatusBadgeVariant, formatStorageTargetType } from '@/lib/format/formatters';
import { ArrowLeft, HardDrive, Cloud, Server, ShieldCheck, Check } from 'lucide-react';

export default function StorageTargetDetailPage() {
  const params = useParams();
  const id = params?.id as string;
  const { activeOrgId } = useAuth();

  const { data, isLoading, isError, error, refetch } = useQuery<StorageTargetResponse>({
    queryKey: activeOrgId && id ? queryKeys.org(activeOrgId).storageTargets.detail(id) : ['disabled'],
    queryFn: () => apiClient.get<StorageTargetResponse>(`/storage-targets/${id}`),
    enabled: !!activeOrgId && !!id,
  });

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
              <h1 className="text-2xl font-bold tracking-tight text-foreground">{data.name}</h1>
              <p className="text-xs font-mono text-muted-foreground">ID: {data.id}</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            {data.is_default && (
              <Badge variant="secondary" className="gap-1 text-xs">
                <Check className="h-3 w-3" />
                Default Destination
              </Badge>
            )}
            <Badge variant={variant} className="capitalize text-sm px-3 py-1">
              {label}
            </Badge>
          </div>
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
    </div>
  );
}
