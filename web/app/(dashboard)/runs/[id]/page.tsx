'use client';

import * as React from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { usePermissions } from '@/lib/auth/permissions';
import { useVerifyBackupRun } from '@/lib/api/mutations';
import { type BackupRunResponse, type VerifyBackupRunResponse } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import {
  formatDate,
  formatDuration,
  formatBytes,
  getStatusBadgeVariant,
} from '@/lib/format/formatters';
import {
  ArrowLeft,
  History,
  AlertTriangle,
  Clock,
  HardDrive,
  ShieldCheck,
  CheckCircle2,
  XCircle,
} from 'lucide-react';

export default function BackupRunDetailPage() {
  const params = useParams();
  const id = params?.id as string;
  const { activeOrgId } = useAuth();
  const { canVerifyRun } = usePermissions();
  const verifyRun = useVerifyBackupRun();
  const [verificationResult, setVerificationResult] = React.useState<VerifyBackupRunResponse | null>(null);

  const { data, isLoading, isError, error, refetch } = useQuery<BackupRunResponse>({
    queryKey: activeOrgId && id ? queryKeys.org(activeOrgId).runs.detail(id) : ['disabled'],
    queryFn: () => apiClient.get<BackupRunResponse>(`/backup-runs/${id}`),
    enabled: !!activeOrgId && !!id,
    refetchInterval: (query) => {
      const run = query.state.data;
      return run?.status === 'running' || run?.status === 'pending' ? 3000 : false;
    },
  });

  const handleVerify = async () => {
    const res = await verifyRun.mutateAsync(id);
    setVerificationResult(res);
    refetch();
  };

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
        <Link href="/runs">
          <Button variant="ghost" size="sm" className="gap-2">
            <ArrowLeft className="h-4 w-4" />
            Back to Runs
          </Button>
        </Link>
        <ErrorState
          title="Could not load backup run details"
          error={error}
          onRetry={() => refetch()}
        />
      </div>
    );
  }

  const { label, variant } = getStatusBadgeVariant(data.status);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-2">
        <Link href="/runs" className="w-fit">
          <Button variant="ghost" size="sm" className="gap-2 -ml-2 text-muted-foreground hover:text-foreground">
            <ArrowLeft className="h-4 w-4" />
            Back to Runs
          </Button>
        </Link>
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <History className="h-5 w-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h1 className="text-2xl font-bold tracking-tight text-foreground font-mono">
                  Run {data.id}
                </h1>
                <Badge variant={variant} className="capitalize text-xs px-2.5 py-0.5">
                  {label}
                </Badge>
              </div>
              <p className="text-xs text-muted-foreground mt-0.5">Attempt #{data.attempt_number}</p>
            </div>
          </div>

          {/* Action Buttons */}
          {canVerifyRun && data.status === 'success' && (
            <Button
              size="sm"
              onClick={handleVerify}
              disabled={verifyRun.isPending}
              className="gap-1.5 bg-sky-600 hover:bg-sky-500 text-white"
            >
              <ShieldCheck className="h-4 w-4" />
              {verifyRun.isPending ? 'Verifying...' : 'Verify Backup'}
            </Button>
          )}
        </div>
      </div>

      {/* Verification Result Banner */}
      {verificationResult && (
        <div
          className={`rounded-lg border p-4 text-sm space-y-2 ${
            verificationResult.verification_status === 'verified'
              ? 'border-emerald-800/50 bg-emerald-950/20 text-emerald-300'
              : 'border-rose-800/50 bg-rose-950/20 text-rose-300'
          }`}
        >
          <div className="flex items-center gap-2">
            {verificationResult.verification_status === 'verified' ? (
              <CheckCircle2 className="h-5 w-5 text-emerald-400 shrink-0" />
            ) : (
              <XCircle className="h-5 w-5 text-rose-400 shrink-0" />
            )}
            <p className="font-semibold">
              Verification Result:{' '}
              <span className="uppercase">{verificationResult.verification_status}</span>
            </p>
          </div>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 pt-2 border-t border-zinc-800 text-xs">
            <div>
              <span className="text-muted-foreground block">Checksum Match</span>
              <span className="font-semibold">
                {verificationResult.details.checksum_matched ? 'Valid' : 'Failed'}
              </span>
            </div>
            <div>
              <span className="text-muted-foreground block">Archive Integrity</span>
              <span className="font-semibold">
                {verificationResult.details.archive_integrity}
              </span>
            </div>
            <div>
              <span className="text-muted-foreground block">Compression</span>
              <span className="font-semibold">
                {verificationResult.details.compression_valid ? 'Valid' : 'Invalid'}
              </span>
            </div>
            <div>
              <span className="text-muted-foreground block">Verified At</span>
              <span className="font-semibold">
                {formatDate(verificationResult.verified_at)}
              </span>
            </div>
          </div>
        </div>
      )}

      {/* Safe Error Alert if Failed */}
      {data.error_message && (
        <div className="rounded-lg border border-destructive/20 bg-destructive/5 p-4 space-y-2">
          <div className="flex items-center gap-2 text-destructive font-semibold text-sm">
            <AlertTriangle className="h-4 w-4 shrink-0" />
            <span>Safe Error Summary</span>
          </div>
          <p className="text-xs font-mono text-destructive break-words">
            {String(data.error_message)}
          </p>
        </div>
      )}

      {/* Detail Cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* Execution Metadata Card */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              <Clock className="h-4 w-4 text-muted-foreground" />
              Execution Details
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Run Status</span>
              <span className="font-medium capitalize text-foreground">{data.status}</span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Started At</span>
              <span className="font-medium text-foreground">{formatDate(data.started_at)}</span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Ended At</span>
              <span className="font-medium text-foreground">
                {data.ended_at ? formatDate(data.ended_at) : 'In progress'}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Duration</span>
              <span className="font-medium text-foreground">
                {formatDuration(data.duration_seconds)}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <span className="text-muted-foreground">Job Reference ID</span>
              <span className="font-mono text-xs text-foreground truncate">{data.job_id}</span>
            </div>
          </CardContent>
        </Card>

        {/* Artifacts Summary Card */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              <HardDrive className="h-4 w-4 text-muted-foreground" />
              Artifact Production
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Produced Artifacts</span>
              <span className="font-medium text-foreground">{data.artifacts_count}</span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Total Output Size</span>
              <span className="font-mono font-medium text-foreground">
                {formatBytes(data.total_artifact_size_bytes)}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Resource ID</span>
              <span className="font-mono text-xs text-foreground truncate">{data.resource_id}</span>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <span className="text-muted-foreground">Created At</span>
              <span className="font-medium text-foreground">{formatDate(data.created_at)}</span>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
