'use client';

import * as React from 'react';
import { useParams, useRouter } from 'next/navigation';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { ArrowLeft, Calendar, ShieldAlert } from 'lucide-react';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { useAuth } from '@/lib/auth/auth-context';
import { usePermissions } from '@/lib/auth/permissions';
import { useTenantFormGuard } from '@/lib/hooks/use-tenant-form-guard';
import { useUnsavedChanges } from '@/lib/hooks/use-unsaved-changes';
import { useUpdateBackupPlan } from '@/lib/api/mutations';
import { FormField } from '@/components/ui/form-field';
import { Select } from '@/components/ui/select';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { ErrorState } from '@/components/ui/error-state';
import type {
  BackupPlanResponse,
  StorageTargetResponse,
  UpdateBackupPlanRequest,
  PlanStatus,
} from '@/types/domain';

function BackupPlanEditForm({
  id,
  plan,
  storageTargets,
}: {
  id: string;
  plan: BackupPlanResponse;
  storageTargets: StorageTargetResponse[];
}) {
  const router = useRouter();
  const updatePlan = useUpdateBackupPlan();

  // Form State
  const [name, setName] = React.useState(plan.name);
  const [cronExpression, setCronExpression] = React.useState(plan.schedule.cron_expression || '');
  const [timezone, setTimezone] = React.useState(plan.schedule.timezone);
  const [isEnabled, setIsEnabled] = React.useState(plan.schedule.is_enabled);
  const [status, setStatus] = React.useState<PlanStatus>(plan.status);
  const [storageTargetId, setStorageTargetId] = React.useState(plan.storage_target_id);
  const [keepLastN, setKeepLastN] = React.useState<number>(plan.retention_policy?.keep_last_n ?? 7);
  const [keepDays, setKeepDays] = React.useState<number>(plan.retention_policy?.keep_days ?? 30);
  const [paths, setPaths] = React.useState(plan.file_selection?.paths.join('\n') || '');
  const [excludes, setExcludes] = React.useState(
    plan.file_selection?.exclude_patterns?.join(', ') || ''
  );
  const [dbMode, setDbMode] = React.useState<'all' | 'selected'>(
    plan.database_selection?.mode || 'all'
  );
  const [dbsInput, setDbsInput] = React.useState(
    plan.database_selection?.databases?.join(', ') || ''
  );
  const [validationError, setValidationError] = React.useState<string | null>(null);

  const isDirty = name !== plan.name;
  useUnsavedChanges(isDirty);

  useTenantFormGuard({
    onTenantChanged: () => {
      router.push('/plans');
    },
  });

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setValidationError(null);

    if (!name.trim()) {
      setValidationError('Plan name is required.');
      return;
    }
    const cronParts = cronExpression.trim().split(/\s+/);
    if (cronParts.length !== 5) {
      setValidationError('Please provide a valid 5-field cron expression.');
      return;
    }

    const payload: UpdateBackupPlanRequest = {
      name: name.trim(),
      engine_type: plan.engine_type,
      storage_target_id: storageTargetId || undefined,
      schedule: {
        cron_expression: cronExpression.trim(),
        timezone: timezone || 'UTC',
        is_enabled: isEnabled,
      },
      retention_policy: {
        keep_last_n: Number(keepLastN) || undefined,
        keep_days: Number(keepDays) || undefined,
      },
      status,
    };

    if (plan.backup_type === 'mysql_database') {
      const dbs = dbMode === 'selected'
        ? dbsInput.split(/[,\n]/).map((s) => s.trim()).filter(Boolean)
        : [];
      payload.database_selection = {
        mode: dbMode,
        databases: dbs,
      };
    } else {
      const pathList = paths.split(/[,\n]/).map((s) => s.trim()).filter(Boolean);
      const excludeList = excludes.split(/[,\n]/).map((s) => s.trim()).filter(Boolean);
      payload.file_selection = {
        paths: pathList,
        exclude_patterns: excludeList,
      };
    }

    try {
      await updatePlan.mutateAsync({ id, data: payload });
      router.push(`/plans/${id}`);
    } catch {
      // Handled by onError toast
    }
  };

  return (
    <div className="max-w-2xl space-y-6">
      <div className="flex items-center gap-3">
        <Link
          href={`/plans/${id}`}
          className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          aria-label="Back to plan"
        >
          <ArrowLeft className="h-5 w-5" />
        </Link>
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">Edit Backup Plan</h1>
          <p className="text-sm text-muted-foreground">
            Update schedule, retention, and targets for {plan.name}
          </p>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base font-semibold flex items-center gap-2">
            <Calendar className="h-4 w-4 text-primary" />
            Plan Configuration
          </CardTitle>
        </CardHeader>
        <CardContent>
          {validationError && (
            <div className="mb-4 rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive">
              {validationError}
            </div>
          )}

          <form onSubmit={handleSubmit} className="space-y-4">
            <FormField label="Plan Name" htmlFor="plan-name" required>
              <input
                id="plan-name"
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </FormField>

            <FormField label="Status" htmlFor="plan-status" required>
              <Select
                id="plan-status"
                value={status}
                onChange={(e) => setStatus(e.target.value as PlanStatus)}
                options={[
                  { value: 'active', label: 'Active (Scheduled)' },
                  { value: 'paused', label: 'Paused (Suspended)' },
                ]}
              />
            </FormField>

            <div className="grid grid-cols-2 gap-3">
              <FormField label="Cron Expression" htmlFor="plan-cron" required>
                <input
                  id="plan-cron"
                  type="text"
                  value={cronExpression}
                  onChange={(e) => setCronExpression(e.target.value)}
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>

              <FormField label="Timezone" htmlFor="plan-tz" required>
                <Select
                  id="plan-tz"
                  value={timezone}
                  onChange={(e) => setTimezone(e.target.value)}
                  options={[
                    { value: 'UTC', label: 'UTC' },
                    { value: 'America/New_York', label: 'America/New_York' },
                    { value: 'Europe/London', label: 'Europe/London' },
                    { value: 'Asia/Tehran', label: 'Asia/Tehran' },
                  ]}
                />
              </FormField>
            </div>

            <div className="pt-1 pb-1">
              <label className="flex items-center gap-2 text-sm text-foreground cursor-pointer">
                <input
                  type="checkbox"
                  checked={isEnabled}
                  onChange={(e) => setIsEnabled(e.target.checked)}
                  className="rounded border-input text-primary focus:ring-ring"
                />
                <span>Enable automatic scheduled executions</span>
              </label>
            </div>

            {plan.backup_type === 'mysql_database' && (
              <div className="space-y-3 pt-2 border-t">
                <FormField label="Database Selection Mode" htmlFor="db-mode">
                  <Select
                    id="db-mode"
                    value={dbMode}
                    onChange={(e) => setDbMode(e.target.value as 'all' | 'selected')}
                    options={[
                      { value: 'all', label: 'All Databases (Full Instance)' },
                      { value: 'selected', label: 'Specific Databases Only' },
                    ]}
                  />
                </FormField>

                {dbMode === 'selected' && (
                  <FormField
                    label="Databases (comma-separated)"
                    htmlFor="db-names"
                    description="Specify one or more database names"
                  >
                    <input
                      id="db-names"
                      type="text"
                      value={dbsInput}
                      onChange={(e) => setDbsInput(e.target.value)}
                      placeholder="db1, db2, app_production"
                      className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                    />
                  </FormField>
                )}
              </div>
            )}

            {(plan.backup_type === 'website_files' || plan.backup_type === 'both') && (
              <div className="space-y-3 pt-2 border-t">
                <FormField
                  label="Target Directory Paths"
                  htmlFor="plan-paths"
                  description="One directory path per line"
                >
                  <textarea
                    id="plan-paths"
                    rows={3}
                    value={paths}
                    onChange={(e) => setPaths(e.target.value)}
                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </FormField>

                <FormField
                  label="Exclude Patterns (comma-separated)"
                  htmlFor="plan-excludes"
                  description="Globs or patterns to exclude, e.g. *.log, cache/*"
                >
                  <input
                    id="plan-excludes"
                    type="text"
                    value={excludes}
                    onChange={(e) => setExcludes(e.target.value)}
                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </FormField>
              </div>
            )}

            <FormField label="Storage Destination" htmlFor="plan-storage" required>
              <Select
                id="plan-storage"
                value={storageTargetId}
                onChange={(e) => setStorageTargetId(e.target.value)}
                options={
                  storageTargets?.map((t) => ({
                    value: t.id,
                    label: `${t.name} (${t.type})`,
                  })) || []
                }
              />
            </FormField>

            <div className="grid grid-cols-2 gap-3 pt-2 border-t">
              <FormField label="Keep Last N Runs" htmlFor="keep-n">
                <input
                  id="keep-n"
                  type="number"
                  min={0}
                  value={keepLastN}
                  onChange={(e) => setKeepLastN(Number(e.target.value))}
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>

              <FormField label="Keep Days" htmlFor="keep-days">
                <input
                  id="keep-days"
                  type="number"
                  min={0}
                  value={keepDays}
                  onChange={(e) => setKeepDays(Number(e.target.value))}
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>
            </div>

            <div className="flex justify-end gap-3 pt-4 border-t">
              <Link
                href={`/plans/${id}`}
                className="rounded-md border border-input bg-background px-4 py-2 text-sm font-medium hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                Cancel
              </Link>
              <button
                type="submit"
                disabled={updatePlan.isPending}
                className="inline-flex items-center justify-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 focus:outline-none disabled:opacity-50 transition-colors"
              >
                {updatePlan.isPending ? 'Saving...' : 'Save Changes'}
              </button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

export default function EditBackupPlanPage() {
  const params = useParams();
  const id = params?.id as string;
  const { activeOrgId } = useAuth();
  const { canEditPlan } = usePermissions();

  // Fetch plan details
  const {
    data: plan,
    isLoading: loadingPlan,
    isError,
    error,
    refetch,
  } = useQuery<BackupPlanResponse>({
    queryKey: activeOrgId && id ? queryKeys.org(activeOrgId).plans.detail(id) : ['disabled'],
    queryFn: () => apiClient.get<BackupPlanResponse>(`/backup-plans/${id}`),
    enabled: !!activeOrgId && !!id,
  });

  // Fetch storage targets
  const { data: storageTargets } = useQuery<StorageTargetResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).storageTargets.all() : ['disabled'],
    queryFn: () => apiClient.get<StorageTargetResponse[]>('/storage-targets'),
    enabled: !!activeOrgId,
  });

  if (!canEditPlan) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold tracking-tight text-foreground">Edit Backup Plan</h1>
        <Card className="border-destructive/20 bg-destructive/5 p-6 text-center">
          <div className="flex flex-col items-center space-y-3">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10 text-destructive">
              <ShieldAlert className="h-6 w-6" />
            </div>
            <h3 className="text-base font-semibold text-foreground">Permission Denied</h3>
            <p className="text-sm text-muted-foreground max-w-md">
              You do not have permission to modify backup plans.
            </p>
            <Link
              href={`/plans/${id}`}
              className="inline-flex items-center gap-2 text-sm text-primary hover:underline mt-2"
            >
              <ArrowLeft className="h-4 w-4" /> Back to Plan
            </Link>
          </div>
        </Card>
      </div>
    );
  }

  if (loadingPlan) {
    return (
      <div className="space-y-6 max-w-2xl">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (isError || !plan) {
    return (
      <div className="space-y-6 max-w-2xl">
        <ErrorState
          title="Could not load backup plan for editing"
          error={error}
          onRetry={() => refetch()}
        />
      </div>
    );
  }

  return (
    <BackupPlanEditForm
      key={plan.id}
      id={id}
      plan={plan}
      storageTargets={storageTargets || []}
    />
  );
}
