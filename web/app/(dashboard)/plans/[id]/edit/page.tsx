'use client';

import { useParams, useRouter } from 'next/navigation';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { useForm, Controller } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
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
import {
  backupPlanEditSchema,
  type BackupPlanEditFormValues,
} from '@/lib/forms/schemas';
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

  const {
    register,
    handleSubmit,
    watch,
    control,
    formState: { errors, isDirty, isSubmitting },
  } = useForm<BackupPlanEditFormValues>({
    resolver: zodResolver(backupPlanEditSchema),
    defaultValues: {
      name: plan.name,
      is_enabled: plan.schedule.is_enabled,
      status: plan.status,
      cron_expression: plan.schedule.cron_expression || '',
      timezone: plan.schedule.timezone || 'UTC',
      storage_target_id: plan.storage_target_id || '',
      keep_last_n: plan.retention_policy?.keep_last_n ?? 7,
      keep_days: plan.retention_policy?.keep_days ?? 30,
      db_mode: (plan.database_selection?.mode as 'all' | 'selected') || 'all',
      selected_databases: plan.database_selection?.databases?.join(', ') || '',
      paths: plan.file_selection?.paths.join('\n') || '',
      exclude_patterns: plan.file_selection?.exclude_patterns?.join(', ') || '',
    },
  });

  const isEnabled = watch('is_enabled');
  const dbMode = watch('db_mode');

  const { bypassGuard, safeNavigate } = useUnsavedChanges(isDirty);

  useTenantFormGuard({
    onTenantChanged: () => {
      router.push('/plans');
    },
  });

  const onSubmit = async (values: BackupPlanEditFormValues) => {
    const payload: UpdateBackupPlanRequest = {
      name: values.name.trim(),
      engine_type: plan.engine_type,
      storage_target_id: values.storage_target_id || undefined,
      schedule: {
        cron_expression: values.is_enabled ? values.cron_expression?.trim() : undefined,
        timezone: values.timezone || 'UTC',
        is_enabled: values.is_enabled,
      },
      retention_policy: {
        keep_last_n: values.keep_last_n !== null && values.keep_last_n !== undefined ? Number(values.keep_last_n) : undefined,
        keep_days: values.keep_days !== null && values.keep_days !== undefined ? Number(values.keep_days) : undefined,
      },
      status: values.status,
    };

    if (plan.backup_type === 'mysql_database') {
      const dbs = values.db_mode === 'selected'
        ? (values.selected_databases || '').split(/[,\n]/).map((s) => s.trim()).filter(Boolean)
        : [];
      payload.database_selection = {
        mode: values.db_mode || 'all',
        databases: dbs,
      };
    } else {
      const pathList = (values.paths || '').split(/[,\n]/).map((s) => s.trim()).filter(Boolean);
      const excludeList = (values.exclude_patterns || '').split(/[,\n]/).map((s) => s.trim()).filter(Boolean);
      payload.file_selection = {
        paths: pathList,
        exclude_patterns: excludeList,
      };
    }

    try {
      bypassGuard();
      await updatePlan.mutateAsync({ id, data: payload });
      router.push(`/plans/${id}`);
    } catch {
      // Handled by onError toast
    }
  };

  return (
    <div className="max-w-2xl space-y-6">
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={() => safeNavigate(`/plans/${id}`)}
          className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          aria-label="Back to plan"
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
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
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
            <FormField label="Plan Name" htmlFor="plan-name" required error={errors.name?.message}>
              <input
                id="plan-name"
                type="text"
                {...register('name')}
                aria-invalid={Boolean(errors.name)}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </FormField>

            <FormField label="Status" htmlFor="plan-status" required error={errors.status?.message}>
              <Controller
                name="status"
                control={control}
                render={({ field }) => (
                  <Select
                    id="plan-status"
                    value={field.value}
                    onChange={(e) => field.onChange(e.target.value as PlanStatus)}
                    options={[
                      { value: 'active', label: 'Active (Scheduled)' },
                      { value: 'paused', label: 'Paused (Suspended)' },
                    ]}
                  />
                )}
              />
            </FormField>

            <div className="pt-1 pb-1">
              <label className="flex items-center gap-2 text-sm text-foreground cursor-pointer">
                <input
                  type="checkbox"
                  {...register('is_enabled')}
                  className="rounded border-input text-primary focus:ring-ring"
                />
                <span>Enable automatic scheduled executions</span>
              </label>
            </div>

            {isEnabled && (
              <div className="grid grid-cols-2 gap-3">
                <FormField
                  label="Cron Expression"
                  htmlFor="plan-cron"
                  required
                  error={errors.cron_expression?.message}
                  description="e.g. 0 2 * * *"
                >
                  <input
                    id="plan-cron"
                    type="text"
                    {...register('cron_expression')}
                    aria-invalid={Boolean(errors.cron_expression)}
                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </FormField>

                <FormField label="Timezone" htmlFor="plan-tz" required error={errors.timezone?.message}>
                  <Controller
                    name="timezone"
                    control={control}
                    render={({ field }) => (
                      <Select
                        id="plan-tz"
                        value={field.value}
                        onChange={field.onChange}
                        options={[
                          { value: 'UTC', label: 'UTC' },
                          { value: 'America/New_York', label: 'America/New_York' },
                          { value: 'America/Chicago', label: 'America/Chicago' },
                          { value: 'America/Los_Angeles', label: 'America/Los_Angeles' },
                          { value: 'Europe/London', label: 'Europe/London' },
                          { value: 'Europe/Berlin', label: 'Europe/Berlin' },
                          { value: 'Europe/Paris', label: 'Europe/Paris' },
                          { value: 'Asia/Dubai', label: 'Asia/Dubai' },
                          { value: 'Asia/Tehran', label: 'Asia/Tehran' },
                          { value: 'Asia/Tokyo', label: 'Asia/Tokyo' },
                        ]}
                      />
                    )}
                  />
                </FormField>
              </div>
            )}

            {plan.backup_type === 'mysql_database' && (
              <div className="space-y-3 pt-2 border-t">
                <FormField label="Database Selection Mode" htmlFor="db-mode">
                  <Controller
                    name="db_mode"
                    control={control}
                    render={({ field }) => (
                      <Select
                        id="db-mode"
                        value={field.value || 'all'}
                        onChange={(e) => field.onChange(e.target.value as 'all' | 'selected')}
                        options={[
                          { value: 'all', label: 'All Databases (Full Instance)' },
                          { value: 'selected', label: 'Specific Databases Only' },
                        ]}
                      />
                    )}
                  />
                </FormField>

                {dbMode === 'selected' && (
                  <FormField
                    label="Databases (comma-separated)"
                    htmlFor="db-names"
                    description="Specify one or more database names"
                    error={errors.selected_databases?.message}
                  >
                    <input
                      id="db-names"
                      type="text"
                      {...register('selected_databases')}
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
                  error={errors.paths?.message}
                >
                  <textarea
                    id="plan-paths"
                    rows={3}
                    {...register('paths')}
                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </FormField>

                <FormField
                  label="Exclude Patterns (comma-separated)"
                  htmlFor="plan-excludes"
                  description="Globs or patterns to exclude, e.g. *.log, cache/*"
                  error={errors.exclude_patterns?.message}
                >
                  <input
                    id="plan-excludes"
                    type="text"
                    {...register('exclude_patterns')}
                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </FormField>
              </div>
            )}

            <FormField label="Storage Destination" htmlFor="plan-storage" required error={errors.storage_target_id?.message}>
              <Controller
                name="storage_target_id"
                control={control}
                render={({ field }) => (
                  <Select
                    id="plan-storage"
                    value={field.value || ''}
                    onChange={field.onChange}
                    options={
                      storageTargets?.map((t) => ({
                        value: t.id,
                        label: `${t.name} (${t.type})`,
                      })) || []
                    }
                  />
                )}
              />
            </FormField>

            <div className="grid grid-cols-2 gap-3 pt-2 border-t">
              <FormField label="Keep Last N Runs" htmlFor="keep-n" error={errors.keep_last_n?.message}>
                <input
                  id="keep-n"
                  type="number"
                  min={0}
                  {...register('keep_last_n')}
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>

              <FormField label="Keep Days" htmlFor="keep-days" error={errors.keep_days?.message}>
                <input
                  id="keep-days"
                  type="number"
                  min={0}
                  {...register('keep_days')}
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>
            </div>

            <div className="flex justify-end gap-3 pt-4 border-t">
              <button
                type="button"
                onClick={() => safeNavigate(`/plans/${id}`)}
                className="rounded-md border border-input bg-background px-4 py-2 text-sm font-medium hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={isSubmitting || updatePlan.isPending}
                className="inline-flex items-center justify-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 focus:outline-none disabled:opacity-50 transition-colors"
              >
                {updatePlan.isPending || isSubmitting ? 'Saving...' : 'Save Changes'}
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

