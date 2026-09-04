'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { type ResourceResponse } from '@/types/domain';
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
import { formatDate, formatResourceType, getStatusBadgeVariant } from '@/lib/format/formatters';
import { Server, Search, ChevronRight } from 'lucide-react';

export default function ResourcesPage() {
  const { activeOrgId } = useAuth();
  const [search, setSearch] = useState('');

  const { data, isLoading, isError, error, refetch } = useQuery<ResourceResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).resources.all() : ['disabled'],
    queryFn: () => apiClient.get<ResourceResponse[]>('/resources'),
    enabled: !!activeOrgId,
  });

  const resources = data || [];
  const filtered = resources.filter((res) =>
    res.name.toLowerCase().includes(search.toLowerCase()) ||
    res.type.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">Protected Resources</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Servers, databases, and assets registered for backup operations
          </p>
        </div>
        <div className="relative w-full sm:w-64">
          <Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search resources..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-9 h-9"
          />
        </div>
      </div>

      {/* Main Content */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base font-semibold">Resources List</CardTitle>
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
                title="Could not load resources"
                error={error}
                onRetry={() => refetch()}
              />
            </div>
          ) : resources.length === 0 ? (
            <div className="p-6">
              <EmptyState
                icon={Server}
                title="No protected resources yet"
                description="Resources will appear here once registered by your organization administrator."
              />
            </div>
          ) : filtered.length === 0 ? (
            <div className="p-6 text-center text-sm text-muted-foreground">
              No resources match your search criteria.
            </div>
          ) : (
            <>
              {/* Desktop Table */}
              <div className="hidden md:block">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Resource Name</TableHead>
                      <TableHead>Type</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Connection Test</TableHead>
                      <TableHead>Created At</TableHead>
                      <TableHead className="w-[80px]"></TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {filtered.map((res) => {
                      const { label, variant } = getStatusBadgeVariant(res.status);
                      return (
                        <TableRow key={res.id}>
                          <TableCell className="font-medium text-foreground">
                            <Link
                              href={`/resources/${res.id}`}
                              className="hover:underline flex items-center gap-2"
                            >
                              <Server className="h-4 w-4 text-muted-foreground" />
                              {res.name}
                            </Link>
                          </TableCell>
                          <TableCell className="font-medium text-xs">
                            {formatResourceType(res.type)}
                          </TableCell>
                          <TableCell>
                            <Badge variant={variant} className="capitalize">
                              {label}
                            </Badge>
                          </TableCell>
                          <TableCell className="text-xs text-muted-foreground">
                            {res.last_connection_test_at
                              ? formatDate(res.last_connection_test_at)
                              : 'Never'}
                          </TableCell>
                          <TableCell className="text-xs text-muted-foreground">
                            {formatDate(res.created_at)}
                          </TableCell>
                          <TableCell className="text-right">
                            <Link
                              href={`/resources/${res.id}`}
                              className="text-muted-foreground hover:text-foreground inline-flex p-1"
                              aria-label={`View details for ${res.name}`}
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
                {filtered.map((res) => {
                  const { label, variant } = getStatusBadgeVariant(res.status);
                  return (
                    <Link
                      key={res.id}
                      href={`/resources/${res.id}`}
                      className="block p-4 hover:bg-muted/30 transition-colors"
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="flex items-center gap-2 font-medium">
                          <Server className="h-4 w-4 text-muted-foreground shrink-0" />
                          <span className="truncate text-foreground">{res.name}</span>
                        </div>
                        <Badge variant={variant} className="capitalize shrink-0">
                          {label}
                        </Badge>
                      </div>
                      <div className="mt-2 grid grid-cols-2 gap-1 text-xs text-muted-foreground">
                        <div>
                          <span className="font-medium text-foreground">{formatResourceType(res.type)}</span>
                        </div>
                        <div className="text-right">
                          <span>Created {formatDate(res.created_at)}</span>
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
