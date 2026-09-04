'use client';

import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { type BackupArtifactResponse } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import { formatDate, formatBytes, getStatusBadgeVariant } from '@/lib/format/formatters';
import { ArrowLeft, Archive, ShieldCheck, FileArchive } from 'lucide-react';

export default function BackupArtifactDetailPage() {
  const params = useParams();
  const id = params?.id as string;
  const { activeOrgId } = useAuth();

  const { data, isLoading, isError, error, refetch } = useQuery<BackupArtifactResponse>({
    queryKey: activeOrgId && id ? queryKeys.org(activeOrgId).artifacts.detail(id) : ['disabled'],
    queryFn: () => apiClient.get<BackupArtifactResponse>(`/backup-artifacts/${id}`),
    enabled: !!activeOrgId && !!id,
  });

  if (isLoading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-48 w-full" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }

  if (isError || !data) {
    return (
      <div className="space-y-6">
        <Link href="/artifacts">
          <Button variant="ghost" size="sm" className="gap-2">
            <ArrowLeft className="h-4 w-4" />
            Back to Artifacts
          </Button>
        </Link>
        <ErrorState
          title="Could not load artifact details"
          error={error}
          onRetry={() => refetch()}
        />
      </div>
    );
  }

  const { label, variant } = getStatusBadgeVariant(data.verification_status);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-2">
        <Link href="/artifacts" className="w-fit">
          <Button variant="ghost" size="sm" className="gap-2 -ml-2 text-muted-foreground hover:text-foreground">
            <ArrowLeft className="h-4 w-4" />
            Back to Artifacts
          </Button>
        </Link>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <Archive className="h-5 w-5" />
            </div>
            <div>
              <h1 className="text-xl md:text-2xl font-bold tracking-tight text-foreground font-mono truncate max-w-xl">
                {data.artifact_name}
              </h1>
              <p className="text-xs font-mono text-muted-foreground">ID: {data.id}</p>
            </div>
          </div>
          <Badge variant={variant} className="capitalize text-sm px-3 py-1">
            Verification: {label}
          </Badge>
        </div>
      </div>

      {/* Detail Cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* Archive Metadata Card */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              <FileArchive className="h-4 w-4 text-muted-foreground" />
              Archive Attributes
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Logical Filename</span>
              <span className="font-mono font-medium text-foreground truncate">
                {data.artifact_name}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Archive Size</span>
              <span className="font-mono font-medium text-foreground">
                {formatBytes(data.size_bytes)} ({data.size_bytes.toLocaleString()} bytes)
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Compression</span>
              <span className="font-mono text-foreground uppercase">
                {data.compression_type || 'GZIP'}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <span className="text-muted-foreground">Created</span>
              <span className="font-medium text-foreground">{formatDate(data.created_at)}</span>
            </div>
          </CardContent>
        </Card>

        {/* Verification & Integrity Card */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              <ShieldCheck className="h-4 w-4 text-muted-foreground" />
              Integrity & Verification
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Verification State</span>
              <span className="font-medium capitalize text-foreground">{data.verification_status}</span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Verified At</span>
              <span className="font-medium text-foreground">
                {data.verified_at ? formatDate(data.verified_at) : 'Not verified yet'}
              </span>
            </div>
            <div className="space-y-1">
              <span className="text-xs text-muted-foreground block">SHA-256 Checksum</span>
              <span className="font-mono text-xs bg-muted p-2 rounded block break-all text-foreground">
                {data.checksum_sha256}
              </span>
            </div>
          </CardContent>
        </Card>

        {/* Provenance References Card */}
        <Card className="md:col-span-2">
          <CardHeader>
            <CardTitle className="text-base font-semibold">Provenance Identifiers</CardTitle>
          </CardHeader>
          <CardContent className="grid grid-cols-1 md:grid-cols-2 gap-4 text-sm">
            <div>
              <span className="text-xs text-muted-foreground block">Parent Run Reference</span>
              <Link
                href={`/runs/${data.run_id}`}
                className="font-mono text-xs text-primary hover:underline"
              >
                {data.run_id}
              </Link>
            </div>
            <div>
              <span className="text-xs text-muted-foreground block">Protected Resource ID</span>
              <Link
                href={`/resources/${data.resource_id}`}
                className="font-mono text-xs text-primary hover:underline"
              >
                {data.resource_id}
              </Link>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
