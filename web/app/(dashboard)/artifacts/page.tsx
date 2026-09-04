'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { type BackupArtifactResponse } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
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
import {
  formatDate,
  formatBytes,
  getStatusBadgeVariant,
} from '@/lib/format/formatters';
import { Archive, Search, ChevronRight, FileArchive } from 'lucide-react';

export default function BackupArtifactsPage() {
  const { activeOrgId } = useAuth();
  const [search, setSearch] = useState('');

  // Critical: Send NO query parameters to /backup-artifacts
  const { data, isLoading, isError, error, refetch } = useQuery<BackupArtifactResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).artifacts.all() : ['disabled'],
    queryFn: () => apiClient.get<BackupArtifactResponse[]>('/backup-artifacts'),
    enabled: !!activeOrgId,
  });

  const artifacts = data || [];
  const filtered = artifacts.filter(
    (a) =>
      a.artifact_name.toLowerCase().includes(search.toLowerCase()) ||
      a.id.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">Backup Artifacts</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Immutable backup archives and verification status
          </p>
        </div>
        <div className="relative w-full sm:w-64">
          <Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search artifacts..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-9 h-9"
          />
        </div>
      </div>

      {/* Main Content */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base font-semibold">Stored Archives</CardTitle>
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
                title="Could not load backup artifacts"
                error={error}
                onRetry={() => refetch()}
              />
            </div>
          ) : artifacts.length === 0 ? (
            <div className="p-6">
              <EmptyState
                icon={Archive}
                title="No backup artifacts are available"
                description="Artifacts will appear here once backup jobs successfully produce archive files."
              />
            </div>
          ) : filtered.length === 0 ? (
            <div className="p-6 text-center text-sm text-muted-foreground">
              No artifacts match your search criteria.
            </div>
          ) : (
            <>
              {/* Desktop Table */}
              <div className="hidden md:block">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Filename</TableHead>
                      <TableHead>Size</TableHead>
                      <TableHead>Format / Compression</TableHead>
                      <TableHead>Verification</TableHead>
                      <TableHead>Verified At</TableHead>
                      <TableHead>Created</TableHead>
                      <TableHead className="w-[80px]"></TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {filtered.map((art) => {
                      const { label, variant } = getStatusBadgeVariant(art.verification_status);
                      return (
                        <TableRow key={art.id}>
                          <TableCell className="font-mono text-xs font-medium text-foreground">
                            <Link
                              href={`/artifacts/${art.id}`}
                              className="hover:underline flex items-center gap-2"
                            >
                              <FileArchive className="h-4 w-4 text-muted-foreground shrink-0" />
                              <span className="truncate max-w-[240px]">{art.artifact_name}</span>
                            </Link>
                          </TableCell>
                          <TableCell className="font-mono text-xs text-muted-foreground">
                            {formatBytes(art.size_bytes)}
                          </TableCell>
                          <TableCell className="capitalize text-xs text-muted-foreground font-mono">
                            {art.compression_type || 'standard'}
                          </TableCell>
                          <TableCell>
                            <Badge variant={variant} className="capitalize">
                              {label}
                            </Badge>
                          </TableCell>
                          <TableCell className="text-xs text-muted-foreground">
                            {art.verified_at ? formatDate(art.verified_at) : 'Unverified'}
                          </TableCell>
                          <TableCell className="text-xs text-muted-foreground">
                            {formatDate(art.created_at)}
                          </TableCell>
                          <TableCell className="text-right">
                            <Link
                              href={`/artifacts/${art.id}`}
                              className="text-muted-foreground hover:text-foreground inline-flex p-1"
                              aria-label={`View artifact ${art.artifact_name}`}
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
                {filtered.map((art) => {
                  const { label, variant } = getStatusBadgeVariant(art.verification_status);
                  return (
                    <Link
                      key={art.id}
                      href={`/artifacts/${art.id}`}
                      className="block p-4 hover:bg-muted/30 transition-colors"
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="flex items-center gap-2 font-mono text-xs font-medium truncate">
                          <FileArchive className="h-4 w-4 text-muted-foreground shrink-0" />
                          <span className="truncate text-foreground">{art.artifact_name}</span>
                        </div>
                        <Badge variant={variant} className="capitalize shrink-0">
                          {label}
                        </Badge>
                      </div>
                      <div className="mt-2 grid grid-cols-2 gap-1 text-xs text-muted-foreground font-mono">
                        <div>
                          <span>Size: {formatBytes(art.size_bytes)}</span>
                        </div>
                        <div className="text-right font-sans">
                          <span>{formatDate(art.created_at)}</span>
                        </div>
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
