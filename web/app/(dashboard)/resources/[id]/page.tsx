'use client';

import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { type ResourceResponse } from '@/types/domain';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import { formatDate, getStatusBadgeVariant } from '@/lib/format/formatters';
import { ArrowLeft, Server, Shield, Network } from 'lucide-react';

export default function ResourceDetailPage() {
  const params = useParams();
  const id = params?.id as string;
  const { activeOrgId } = useAuth();

  const { data, isLoading, isError, error, refetch } = useQuery<ResourceResponse>({
    queryKey: activeOrgId && id ? queryKeys.org(activeOrgId).resources.detail(id) : ['disabled'],
    queryFn: () => apiClient.get<ResourceResponse>(`/resources/${id}`),
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
        <Link href="/resources">
          <Button variant="ghost" size="sm" className="gap-2">
            <ArrowLeft className="h-4 w-4" />
            Back to Resources
          </Button>
        </Link>
        <ErrorState
          title="Could not load resource details"
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
        <Link href="/resources" className="w-fit">
          <Button variant="ghost" size="sm" className="gap-2 -ml-2 text-muted-foreground hover:text-foreground">
            <ArrowLeft className="h-4 w-4" />
            Back to Resources
          </Button>
        </Link>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <Server className="h-5 w-5" />
            </div>
            <div>
              <h1 className="text-2xl font-bold tracking-tight text-foreground">{data.name}</h1>
              <p className="text-xs font-mono text-muted-foreground">ID: {data.id}</p>
            </div>
          </div>
          <Badge variant={variant} className="capitalize text-sm px-3 py-1">
            {label}
          </Badge>
        </div>
      </div>

      {/* Grid Content */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* Basic Metadata Card */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              <Server className="h-4 w-4 text-muted-foreground" />
              Resource Information
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Resource Type</span>
              <span className="font-medium capitalize text-foreground">
                {data.type.replace(/_/g, ' ')}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Status</span>
              <span className="font-medium capitalize text-foreground">{data.status}</span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Last Connection Test</span>
              <span className="font-medium text-foreground">
                {data.last_connection_test_at ? formatDate(data.last_connection_test_at) : 'Never'}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <span className="text-muted-foreground">Connection Status</span>
              <span className="font-medium capitalize text-foreground">
                {data.last_connection_status || 'Unknown'}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <span className="text-muted-foreground">Registered At</span>
              <span className="font-medium text-foreground">{formatDate(data.created_at)}</span>
            </div>
          </CardContent>
        </Card>

        {/* Connector Details Card */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base font-semibold flex items-center gap-2">
              <Network className="h-4 w-4 text-muted-foreground" />
              Connector Configuration
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            {data.connector ? (
              <>
                <div className="grid grid-cols-2 gap-2 border-b pb-3">
                  <span className="text-muted-foreground">Host / Port</span>
                  <span className="font-mono font-medium text-foreground">
                    {data.connector.host || '—'}:{data.connector.port || '—'}
                  </span>
                </div>
                {data.connector.auth_type && (
                  <div className="grid grid-cols-2 gap-2 border-b pb-3">
                    <span className="text-muted-foreground">Authentication Type</span>
                    <span className="font-medium capitalize text-foreground">
                      {data.connector.auth_type.replace(/_/g, ' ')}
                    </span>
                  </div>
                )}
                {data.connector.username && (
                  <div className="grid grid-cols-2 gap-2 border-b pb-3">
                    <span className="text-muted-foreground">Username</span>
                    <span className="font-mono text-foreground">{data.connector.username}</span>
                  </div>
                )}
                {data.connector.credential_name && (
                  <div className="grid grid-cols-2 gap-2 border-b pb-3">
                    <span className="text-muted-foreground">Attached Credential</span>
                    <span className="font-medium text-foreground">
                      {data.connector.credential_name}
                    </span>
                  </div>
                )}
                {data.connector.host_key_fingerprint && (
                  <div className="space-y-1">
                    <span className="text-xs text-muted-foreground block">Host Key Fingerprint</span>
                    <span className="font-mono text-xs bg-muted p-2 rounded block break-all text-foreground">
                      {data.connector.host_key_fingerprint}
                    </span>
                  </div>
                )}
              </>
            ) : (
              <div className="py-6 text-center text-muted-foreground">
                <Shield className="h-8 w-8 mx-auto mb-2 opacity-40" />
                <p>Connector details are restricted based on your role permissions.</p>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
