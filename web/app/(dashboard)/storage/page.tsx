'use client';

import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { type StorageTargetResponse } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/table';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import { EmptyState } from '@/components/ui/empty-state';
import { formatDate, getStatusBadgeVariant, formatStorageTargetType } from '@/lib/format/formatters';
import { HardDrive, Cloud, Server, ChevronRight, Check } from 'lucide-react';

export default function StorageTargetsPage() {
  const { activeOrgId } = useAuth();

  const { data, isLoading, isError, error, refetch } = useQuery<StorageTargetResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).storageTargets.all() : ['disabled'],
    queryFn: () => apiClient.get<StorageTargetResponse[]>('/storage-targets'),
    enabled: !!activeOrgId,
  });

  const targets = data || [];

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-foreground">Storage Destinations</h1>
        <p className="text-sm text-muted-foreground mt-0.5">
          Local volumes and S3-compatible cloud targets configured for backup retention
        </p>
      </div>

      {/* Main Content */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base font-semibold">Configured Storage Targets</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="p-6 space-y-4">
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          ) : isError ? (
            <div className="p-6">
              <ErrorState
                title="Could not load storage targets"
                error={error}
                onRetry={() => refetch()}
              />
            </div>
          ) : targets.length === 0 ? (
            <div className="p-6">
              <EmptyState
                icon={HardDrive}
                title="No storage targets configured"
                description="Default storage destinations will appear here once provisioned for this organization."
              />
            </div>
          ) : (
            <>
              {/* Desktop Table */}
              <div className="hidden md:block">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Target Name</TableHead>
                      <TableHead>Type</TableHead>
                      <TableHead>Default</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Cloud / Target Details</TableHead>
                      <TableHead>Created</TableHead>
                      <TableHead className="w-[80px]"></TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {targets.map((target) => {
                      const { label, variant } = getStatusBadgeVariant(target.status);
                      const isCloud = target.type === 's3' || target.type === 's3_compatible';

                      return (
                        <TableRow key={target.id}>
                          <TableCell className="font-medium text-foreground">
                            <Link
                              href={`/storage/${target.id}`}
                              className="hover:underline flex items-center gap-2"
                            >
                              {isCloud ? (
                                <Cloud className="h-4 w-4 text-sky-500 shrink-0" />
                              ) : (
                                <Server className="h-4 w-4 text-muted-foreground shrink-0" />
                              )}
                              {target.name}
                            </Link>
                          </TableCell>
                          <TableCell className="text-xs font-medium">
                            {formatStorageTargetType(target.type)}
                          </TableCell>
                          <TableCell>
                            {target.is_default ? (
                              <Badge variant="secondary" className="gap-1 text-[11px]">
                                <Check className="h-3 w-3" />
                                Default
                              </Badge>
                            ) : (
                              '—'
                            )}
                          </TableCell>
                          <TableCell>
                            <Badge variant={variant} className="capitalize">
                              {label}
                            </Badge>
                          </TableCell>
                          <TableCell className="text-xs text-muted-foreground font-mono">
                            {target.s3_config
                              ? `bucket: ${target.s3_config.bucket}`
                              : isCloud
                              ? 'S3-compatible target'
                              : 'Platform Managed Volume'}
                          </TableCell>
                          <TableCell className="text-xs text-muted-foreground">
                            {formatDate(target.created_at)}
                          </TableCell>
                          <TableCell className="text-right">
                            <Link
                              href={`/storage/${target.id}`}
                              className="text-muted-foreground hover:text-foreground inline-flex p-1"
                              aria-label={`View target ${target.name}`}
                            >
                              <ChevronRight className="h-4 w-4" />
                            </Link>
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>

              {/* Mobile Stacked Cards */}
              <div className="md:hidden divide-y">
                {targets.map((target) => {
                  const { label, variant } = getStatusBadgeVariant(target.status);
                  const isS3 = target.type === 's3';

                  return (
                    <Link
                      key={target.id}
                      href={`/storage/${target.id}`}
                      className="block p-4 hover:bg-muted/30 transition-colors"
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="flex items-center gap-2 font-medium">
                          {isS3 ? (
                            <Cloud className="h-4 w-4 text-sky-500 shrink-0" />
                          ) : (
                            <Server className="h-4 w-4 text-muted-foreground shrink-0" />
                          )}
                          <span className="truncate text-foreground">{target.name}</span>
                        </div>
                        <Badge variant={variant} className="capitalize shrink-0">
                          {label}
                        </Badge>
                      </div>

                      <div className="mt-2 text-xs text-muted-foreground space-y-1">
                        <div className="flex items-center justify-between">
                          <span className="uppercase font-mono">{target.type}</span>
                          {target.is_default && (
                            <span className="text-primary font-medium">Default Target</span>
                          )}
                        </div>
                        {isS3 && target.s3_config && (
                          <div className="font-mono truncate">
                            bucket: {target.s3_config.bucket}
                          </div>
                        )}
                      </div>
                    </Link>
                  );
                })}
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
