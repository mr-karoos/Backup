'use client';

import * as React from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { useForm, Controller } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import {
  ArrowLeft,
  Calendar,
  ShieldAlert,
  Database,
  FolderTree,
  CheckCircle2,
  ChevronRight,
  ChevronLeft,
  AlertTriangle,
} from 'lucide-react';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { useAuth } from '@/lib/auth/auth-context';
import { usePermissions } from '@/lib/auth/permissions';
import { useTenantFormGuard } from '@/lib/hooks/use-tenant-form-guard';
import { useUnsavedChanges } from '@/lib/hooks/use-unsaved-changes';
import { useCreateBackupPlan, useDiscoverDatabases } from '@/lib/api/mutations';
import { FormField } from '@/components/ui/form-field';
import { Select } from '@/components/ui/select';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import {
  backupPlanSchema,
  type BackupPlanFormValues,
} from '@/lib/forms/schemas';
import type {
  ResourceResponse,
  StorageTargetResponse,
  CreateBackupPlanRequest,
} from '@/types/domain';

const STEPS = [
  'Resource',
  'Backup Type',
  'Content Selection',
  'Schedule',
  'Storage & Retention',
  'Review',
] as const;

export default function NewBackupPlanPage() {
  const router = useRouter();
  const { activeOrgId } = useAuth();
  const { canCreatePlan } = usePermissions();
  const createPlan = useCreateBackupPlan();
  const discoverDbs = useDiscoverDatabases();

  const [step, setStep] = React.useState<number>(0);
  const [schedulePreset, setSchedulePreset] = React.useState<'daily' | '12h' | 'weekly' | 'custom'>('daily');
  const [discoveredDbs, setDiscoveredDbs] = React.useState<string[]>([]);
  const [stepValidationError, setStepValidationError] = React.useState<string | null>(null);

  // Fetch resources
  const { data: resources, isLoading: loadingResources } = useQuery<ResourceResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).resources.all() : ['disabled'],
    queryFn: () => apiClient.get<ResourceResponse[]>('/resources'),
    enabled: !!activeOrgId,
  });

  // Fetch storage targets
  const { data: storageTargets, isLoading: loadingStorage } = useQuery<StorageTargetResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).storageTargets.all() : ['disabled'],
    queryFn: () => apiClient.get<StorageTargetResponse[]>('/storage-targets'),
    enabled: !!activeOrgId,
  });

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    trigger,
    control,
    reset,
    formState: { errors, isDirty, isSubmitting },
  } = useForm<BackupPlanFormValues>({
    resolver: zodResolver(backupPlanSchema),
    defaultValues: {
      name: '',
      resource_id: '',
      backup_type: 'mysql_database',
      storage_target_id: '',
      is_enabled: true,
      cron_expression: '0 2 * * *',
      timezone: 'UTC',
      keep_last_n: 7,
      keep_days: 30,
      db_mode: 'all',
      selected_databases: [],
      manual_databases: '',
      paths: '/var/www/html',
      exclude_patterns: '*.log, cache/*, tmp/*',
    },
  });

  const resourceId = watch('resource_id');
  const backupType = watch('backup_type');
  const dbMode = watch('db_mode');
  const selectedDatabases = watch('selected_databases') || [];
  const isEnabled = watch('is_enabled');
  const cronExpression = watch('cron_expression') || '';
  const timezone = watch('timezone');
  const storageTargetId = watch('storage_target_id');
  const keepLastN = watch('keep_last_n');
  const keepDays = watch('keep_days');
  const planName = watch('name');

  // Set default resource once loaded
  React.useEffect(() => {
    if (resources && !resourceId) {
      const defaultRes = resources.find((r) => r.status === 'active' && r.type === 'ubuntu_ssh');
      if (defaultRes) {
        setValue('resource_id', defaultRes.id);
      }
    }
  }, [resources, resourceId, setValue]);

  // Set default storage target once loaded
  React.useEffect(() => {
    if (storageTargets && !storageTargetId) {
      const defaultTarget =
        storageTargets.find((t) => t.is_default && t.status === 'active') ||
        storageTargets.find((t) => t.status === 'active');
      if (defaultTarget) {
        setValue('storage_target_id', defaultTarget.id);
      }
    }
  }, [storageTargets, storageTargetId, setValue]);

  const { bypassGuard, safeNavigate } = useUnsavedChanges(isDirty);

  useTenantFormGuard({
    onTenantChanged: () => {
      reset();
      router.push('/plans');
    },
  });

  const selectedResource = resources?.find((r) => r.id === resourceId);
  const selectedStorage = storageTargets?.find((t) => t.id === storageTargetId);

  // Schedule presets updater
  const handlePresetChange = (preset: 'daily' | '12h' | 'weekly' | 'custom') => {
    setSchedulePreset(preset);
    if (preset === 'daily') setValue('cron_expression', '0 2 * * *', { shouldValidate: true });
    else if (preset === '12h') setValue('cron_expression', '0 */12 * * *', { shouldValidate: true });
    else if (preset === 'weekly') setValue('cron_expression', '0 2 * * 0', { shouldValidate: true });
  };

  const handleDiscover = async () => {
    if (!resourceId) return;
    try {
      const res = await discoverDbs.mutateAsync(resourceId);
      const names = res.map((d) => d.name);
      setDiscoveredDbs(names);
      if (names.length > 0 && selectedDatabases.length === 0) {
        setValue('selected_databases', names, { shouldDirty: true, shouldValidate: true });
      }
    } catch {
      // handled by mutation toast
    }
  };

  if (!canCreatePlan) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold tracking-tight text-foreground">Create Backup Plan</h1>
        <Card className="border-destructive/20 bg-destructive/5 p-6 text-center">
          <div className="flex flex-col items-center space-y-3">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10 text-destructive">
              <ShieldAlert className="h-6 w-6" />
            </div>
            <h3 className="text-base font-semibold text-foreground">Permission Denied</h3>
            <p className="text-sm text-muted-foreground max-w-md">
              You do not have permission to create backup plans for this organization.
            </p>
            <Link
              href="/plans"
              className="inline-flex items-center gap-2 text-sm text-primary hover:underline mt-2"
            >
              <ArrowLeft className="h-4 w-4" /> Back to Plans
            </Link>
          </div>
        </Card>
      </div>
    );
  }

  const validateCurrentStep = async (): Promise<boolean> => {
    setStepValidationError(null);
    if (step === 0) {
      const valid = await trigger(['name', 'resource_id']);
      if (!valid) return false;
      if (selectedResource && selectedResource.type !== 'ubuntu_ssh') {
        setStepValidationError('Backup operations are not supported for this resource type yet.');
        return false;
      }
      return true;
    } else if (step === 1) {
      return await trigger(['backup_type']);
    } else if (step === 2) {
      if (backupType === 'mysql_database') {
        return await trigger(['db_mode', 'selected_databases', 'manual_databases']);
      } else {
        return await trigger(['paths', 'exclude_patterns']);
      }
    } else if (step === 3) {
      return await trigger(['is_enabled', 'cron_expression', 'timezone']);
    } else if (step === 4) {
      return await trigger(['storage_target_id', 'keep_last_n', 'keep_days']);
    }
    return true;
  };

  const handleNext = async () => {
    const isValid = await validateCurrentStep();
    if (isValid) {
      setStep((s) => Math.min(s + 1, STEPS.length - 1));
    }
  };

  const handleBack = () => {
    setStepValidationError(null);
    setStep((s) => Math.max(s - 1, 0));
  };

  const onSubmit = async (values: BackupPlanFormValues) => {
    const payload: CreateBackupPlanRequest = {
      name: values.name.trim(),
      resource_id: values.resource_id,
      backup_type: values.backup_type,
      engine_type: 'direct_stream',
      storage_target_id: values.storage_target_id || undefined,
      schedule: {
        is_enabled: values.is_enabled,
        cron_expression: values.is_enabled ? values.cron_expression?.trim() : undefined,
        timezone: values.timezone || 'UTC',
      },
      retention_policy: {
        keep_last_n: values.keep_last_n ? Number(values.keep_last_n) : undefined,
        keep_days: values.keep_days ? Number(values.keep_days) : undefined,
      },
    };

    if (values.backup_type === 'mysql_database') {
      let dbs: string[] = [];
      if (values.db_mode === 'selected') {
        const manual = (values.manual_databases || '')
          .split(/[,\n]/)
          .map((s) => s.trim())
          .filter(Boolean);
        dbs = Array.from(new Set([...values.selected_databases, ...manual]));
      }
      payload.database_selection = {
        mode: values.db_mode,
        databases: dbs,
      };
    } else {
      const pathList = (values.paths || '')
        .split(/[,\n]/)
        .map((s) => s.trim())
        .filter(Boolean);
      const excludeList = (values.exclude_patterns || '')
        .split(/[,\n]/)
        .map((s) => s.trim())
        .filter(Boolean);
      payload.file_selection = {
        paths: pathList,
        exclude_patterns: excludeList,
      };
    }

    try {
      bypassGuard();
      await createPlan.mutateAsync(payload);
      router.push('/plans');
    } catch {
      // Handled by onError toast
    }
  };

  return (
    <div className="max-w-3xl space-y-6">
      {/* Header */}
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={() => safeNavigate('/plans')}
          className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          aria-label="Back to plans"
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">Create Backup Plan</h1>
          <p className="text-sm text-muted-foreground">
            Configure automated backup schedules, content selection, and retention policies
          </p>
        </div>
      </div>

      {/* Step Indicators */}
      <div className="grid grid-cols-6 gap-2 border-b pb-4">
        {STEPS.map((stepName, i) => (
          <div
            key={stepName}
            className={`flex flex-col items-center gap-1 text-center transition-colors ${
              i === step
                ? 'text-primary font-semibold'
                : i < step
                ? 'text-muted-foreground'
                : 'text-zinc-600'
            }`}
          >
            <div
              className={`flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold ${
                i === step
                  ? 'bg-primary text-primary-foreground ring-2 ring-primary ring-offset-2 ring-offset-background'
                  : i < step
                  ? 'bg-muted text-foreground'
                  : 'bg-zinc-800 text-zinc-500'
              }`}
            >
              {i + 1}
            </div>
            <span className="text-[11px] truncate max-w-full">{stepName}</span>
          </div>
        ))}
      </div>

      {stepValidationError && (
        <div className="rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive flex items-center gap-2">
          <AlertTriangle className="h-4 w-4 shrink-0" />
          <span>{stepValidationError}</span>
        </div>
      )}

      {/* Step Content */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base font-semibold flex items-center gap-2">
            <Calendar className="h-4 w-4 text-primary" />
            Step {step + 1}: {STEPS[step]}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {/* Step 0: Plan Name & Target Resource */}
          {step === 0 && (
            <div className="space-y-4">
              <FormField
                label="Plan Name"
                htmlFor="plan-name"
                required
                error={errors.name?.message}
              >
                <input
                  id="plan-name"
                  type="text"
                  {...register('name')}
                  placeholder="e.g. Daily Production Database Backup"
                  aria-invalid={Boolean(errors.name)}
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>

              <FormField
                label="Target Resource"
                htmlFor="plan-resource"
                required
                error={errors.resource_id?.message}
                description="Select an active Ubuntu Linux server to execute this backup plan on. Note: cPanel resources are not currently supported."
              >
                <Controller
                  name="resource_id"
                  control={control}
                  render={({ field }) => (
                    <Select
                      id="plan-resource"
                      value={field.value}
                      onChange={field.onChange}
                      disabled={loadingResources}
                      options={[
                        { value: '', label: '-- Select Resource --' },
                        ...(resources
                          ?.filter((r) => r.status === 'active')
                          .map((r) => ({
                            value: r.id,
                            label:
                              r.type === 'ubuntu_ssh'
                                ? `${r.name} (Ubuntu Linux)`
                                : `${r.name} (cPanel — Unsupported)`,
                            disabled: r.type !== 'ubuntu_ssh',
                          })) || []),
                      ]}
                    />
                  )}
                />
              </FormField>

              {selectedResource && selectedResource.type !== 'ubuntu_ssh' && (
                <div className="rounded-md border border-amber-800/40 bg-amber-950/20 p-3 text-xs text-amber-300">
                  Backup operations are not supported for this resource type yet. Please select an Ubuntu Linux resource.
                </div>
              )}
            </div>
          )}

          {/* Step 1: Backup Type */}
          {step === 1 && (
            <div className="space-y-4">
              <p className="text-sm text-muted-foreground">
                Select what type of data this plan should protect.
              </p>
              <div className="grid grid-cols-2 gap-4">
                <div
                  onClick={() => setValue('backup_type', 'mysql_database', { shouldValidate: true })}
                  className={`cursor-pointer rounded-lg border p-4 transition-all ${
                    backupType === 'mysql_database'
                      ? 'border-primary bg-primary/10 shadow-sm'
                      : 'border-input hover:border-zinc-700 bg-background'
                  }`}
                >
                  <div className="flex items-center gap-2 mb-2">
                    <Database className="h-5 w-5 text-sky-500" />
                    <span className="font-semibold text-foreground">MySQL Database</span>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Creates consistent, automated backups of your MySQL databases directly to your configured storage destination.
                  </p>
                </div>

                <div
                  onClick={() => setValue('backup_type', 'website_files', { shouldValidate: true })}
                  className={`cursor-pointer rounded-lg border p-4 transition-all ${
                    backupType === 'website_files'
                      ? 'border-primary bg-primary/10 shadow-sm'
                      : 'border-input hover:border-zinc-700 bg-background'
                  }`}
                >
                  <div className="flex items-center gap-2 mb-2">
                    <FolderTree className="h-5 w-5 text-amber-500" />
                    <span className="font-semibold text-foreground">Website Files</span>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Creates archives of web roots, application directories, and asset files with customizable exclusion patterns.
                  </p>
                </div>
              </div>
            </div>
          )}

          {/* Step 2: Content Selection */}
          {step === 2 && (
            <div className="space-y-4">
              {backupType === 'mysql_database' ? (
                <>
                  <FormField label="Database Selection Mode" htmlFor="db-mode" required>
                    <div className="flex gap-4 pt-1">
                      <label className="flex items-center gap-2 text-sm text-foreground cursor-pointer">
                        <input
                          type="radio"
                          name="db-mode"
                          checked={dbMode === 'all'}
                          onChange={() => setValue('db_mode', 'all', { shouldValidate: true })}
                          className="text-primary focus:ring-ring"
                        />
                        <span>Back up all databases</span>
                      </label>
                      <label className="flex items-center gap-2 text-sm text-foreground cursor-pointer">
                        <input
                          type="radio"
                          name="db-mode"
                          checked={dbMode === 'selected'}
                          onChange={() => setValue('db_mode', 'selected', { shouldValidate: true })}
                          className="text-primary focus:ring-ring"
                        />
                        <span>Select specific databases</span>
                      </label>
                    </div>
                  </FormField>

                  {dbMode === 'selected' && (
                    <div className="space-y-3 pt-2 border-t">
                      <div className="flex items-center justify-between">
                        <span className="text-xs font-semibold text-muted-foreground">
                          Databases to Back Up
                        </span>
                        {selectedResource?.type === 'ubuntu_ssh' && (
                          <button
                            type="button"
                            onClick={handleDiscover}
                            disabled={discoverDbs.isPending}
                            className="inline-flex items-center gap-1 text-xs text-primary hover:underline"
                          >
                            <Database className="h-3.5 w-3.5" />
                            {discoverDbs.isPending ? 'Discovering...' : 'Discover databases on server'}
                          </button>
                        )}
                      </div>

                      {discoveredDbs.length > 0 && (
                        <div className="space-y-1 rounded-md border p-3 bg-muted/20">
                          <p className="text-xs text-muted-foreground mb-2">
                            Select discovered databases:
                          </p>
                          <div className="grid grid-cols-2 gap-2">
                            {discoveredDbs.map((dbName) => (
                              <label
                                key={dbName}
                                className="flex items-center gap-2 text-xs font-mono text-foreground cursor-pointer"
                              >
                                <input
                                  type="checkbox"
                                  checked={selectedDatabases.includes(dbName)}
                                  onChange={(e) => {
                                    if (e.target.checked) {
                                      setValue('selected_databases', [...selectedDatabases, dbName], {
                                        shouldValidate: true,
                                      });
                                    } else {
                                      setValue(
                                        'selected_databases',
                                        selectedDatabases.filter((n) => n !== dbName),
                                        { shouldValidate: true }
                                      );
                                    }
                                  }}
                                  className="rounded border-input text-primary focus:ring-ring"
                                />
                                <span>{dbName}</span>
                              </label>
                            ))}
                          </div>
                        </div>
                      )}

                      <FormField
                        label="Additional or Manual Database Names"
                        htmlFor="manual-dbs"
                        description="Comma or newline separated database names"
                        error={errors.selected_databases?.message}
                      >
                        <textarea
                          id="manual-dbs"
                          rows={3}
                          {...register('manual_databases')}
                          placeholder="db_app, db_auth"
                          className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                        />
                      </FormField>
                    </div>
                  )}
                </>
              ) : (
                <>
                  <FormField
                    label="Directory Paths to Back Up"
                    htmlFor="file-paths"
                    required
                    error={errors.paths?.message}
                    description="Comma or newline separated absolute paths (POSIX format)"
                  >
                    <textarea
                      id="file-paths"
                      rows={3}
                      {...register('paths')}
                      placeholder="/var/www/html&#10;/etc/nginx"
                      className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                    />
                  </FormField>

                  <FormField
                    label="Exclude Patterns (Optional)"
                    htmlFor="exclude-patterns"
                    description="Comma separated patterns to skip from archive"
                  >
                    <input
                      id="exclude-patterns"
                      type="text"
                      {...register('exclude_patterns')}
                      placeholder="*.log, cache/*, tmp/*"
                      className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                    />
                  </FormField>
                </>
              )}
            </div>
          )}

          {/* Step 3: Schedule & Timezone */}
          {step === 3 && (
            <div className="space-y-4">
              <div className="flex items-center justify-between pb-2 border-b">
                <div>
                  <label htmlFor="toggle-schedule" className="text-sm font-semibold text-foreground cursor-pointer">
                    Enable Automated Schedule
                  </label>
                  <p className="text-xs text-muted-foreground">
                    When enabled, backups run automatically based on the schedule configured below.
                  </p>
                </div>
                <input
                  id="toggle-schedule"
                  type="checkbox"
                  {...register('is_enabled')}
                  className="h-4 w-4 rounded border-input text-primary focus:ring-ring cursor-pointer"
                />
              </div>

              {isEnabled ? (
                <>
                  <FormField label="Schedule Presets" htmlFor="schedule-preset">
                    <div className="grid grid-cols-4 gap-2 pt-1">
                      {[
                        { id: 'daily', label: 'Daily (02:00)' },
                        { id: '12h', label: 'Every 12h' },
                        { id: 'weekly', label: 'Weekly (Sun)' },
                        { id: 'custom', label: 'Custom Cron' },
                      ].map((p) => (
                        <button
                          key={p.id}
                          type="button"
                          onClick={() => handlePresetChange(p.id as any)}
                          className={`rounded-md border py-2 px-3 text-xs font-medium transition-colors ${
                            schedulePreset === p.id
                              ? 'border-primary bg-primary/10 text-primary'
                              : 'border-input hover:bg-muted text-muted-foreground'
                          }`}
                        >
                          {p.label}
                        </button>
                      ))}
                    </div>
                  </FormField>

                  <FormField
                    label="Cron Expression (5 fields)"
                    htmlFor="cron-exp"
                    required
                    error={errors.cron_expression?.message}
                    description="Minute Hour Day-of-Month Month Day-of-Week"
                  >
                    <input
                      id="cron-exp"
                      type="text"
                      {...register('cron_expression')}
                      placeholder="0 2 * * *"
                      className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                    />
                  </FormField>
                </>
              ) : (
                <div className="rounded-md border border-muted bg-muted/20 p-3 text-xs text-muted-foreground">
                  Scheduled execution is paused. Backups can still be triggered on-demand via the dashboard or API.
                </div>
              )}

              <FormField label="Timezone" htmlFor="schedule-tz" required error={errors.timezone?.message}>
                <Controller
                  name="timezone"
                  control={control}
                  render={({ field }) => (
                    <Select
                      id="schedule-tz"
                      value={field.value}
                      onChange={field.onChange}
                      options={[
                        { value: 'UTC', label: 'UTC (Coordinated Universal Time)' },
                        { value: 'America/New_York', label: 'America/New_York (EST/EDT)' },
                        { value: 'America/Chicago', label: 'America/Chicago (CST/CDT)' },
                        { value: 'America/Denver', label: 'America/Denver (MST/MDT)' },
                        { value: 'America/Los_Angeles', label: 'America/Los_Angeles (PST/PDT)' },
                        { value: 'Europe/London', label: 'Europe/London (GMT/BST)' },
                        { value: 'Europe/Berlin', label: 'Europe/Berlin (CET/CEST)' },
                        { value: 'Europe/Paris', label: 'Europe/Paris (CET/CEST)' },
                        { value: 'Asia/Dubai', label: 'Asia/Dubai (+04:00)' },
                        { value: 'Asia/Tehran', label: 'Asia/Tehran (+03:30)' },
                        { value: 'Asia/Tokyo', label: 'Asia/Tokyo (JST)' },
                      ]}
                    />
                  )}
                />
              </FormField>
            </div>
          )}

          {/* Step 4: Storage & Retention */}
          {step === 4 && (
            <div className="space-y-4">
              <FormField
                label="Storage Target"
                htmlFor="storage-target-plan"
                required
                error={errors.storage_target_id?.message}
                description="Destination for backup archives"
              >
                <Controller
                  name="storage_target_id"
                  control={control}
                  render={({ field }) => (
                    <Select
                      id="storage-target-plan"
                      value={field.value}
                      onChange={field.onChange}
                      disabled={loadingStorage}
                      options={[
                        { value: '', label: '-- Select Storage Target --' },
                        ...(storageTargets
                          ?.filter((t) => t.status === 'active')
                          .map((t) => ({
                            value: t.id,
                            label: `${t.name} (${t.type})${t.is_default ? ' [Default]' : ''}`,
                          })) || []),
                      ]}
                    />
                  )}
                />
              </FormField>

              <div className="grid grid-cols-2 gap-4 pt-2 border-t">
                <FormField
                  label="Keep Last N Backups"
                  htmlFor="retention-count"
                  description="Number of successful runs to retain (leave empty for unlimited)"
                  error={errors.keep_last_n?.message}
                >
                  <input
                    id="retention-count"
                    type="number"
                    min={0}
                    {...register('keep_last_n')}
                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </FormField>

                <FormField
                  label="Keep for N Days"
                  htmlFor="retention-days"
                  description="Maximum age in days before pruning (leave empty for unlimited)"
                  error={errors.keep_days?.message}
                >
                  <input
                    id="retention-days"
                    type="number"
                    min={0}
                    {...register('keep_days')}
                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </FormField>
              </div>
            </div>
          )}

          {/* Step 5: Review & Submit */}
          {step === 5 && (
            <div className="space-y-3 text-sm">
              <div className="rounded-lg border p-4 bg-muted/20 space-y-2">
                <div className="flex justify-between border-b pb-2">
                  <span className="text-muted-foreground">Plan Name:</span>
                  <span className="font-semibold text-foreground">{planName}</span>
                </div>
                <div className="flex justify-between border-b pb-2">
                  <span className="text-muted-foreground">Target Resource:</span>
                  <span className="text-foreground">{selectedResource?.name || resourceId}</span>
                </div>
                <div className="flex justify-between border-b pb-2">
                  <span className="text-muted-foreground">Backup Type:</span>
                  <span className="capitalize text-foreground">{backupType.replace('_', ' ')}</span>
                </div>
                <div className="flex justify-between border-b pb-2">
                  <span className="text-muted-foreground">Schedule:</span>
                  <span className="font-mono text-foreground">
                    {isEnabled ? `${cronExpression} (${timezone})` : 'Disabled (Manual execution only)'}
                  </span>
                </div>
                <div className="flex justify-between border-b pb-2">
                  <span className="text-muted-foreground">Storage Destination:</span>
                  <span className="text-foreground">{selectedStorage?.name || storageTargetId}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Retention Policy:</span>
                  <span className="text-foreground">
                    Keep {keepLastN ? `${keepLastN} runs` : 'all runs'} / {keepDays ? `${keepDays} days` : 'indefinite'}
                  </span>
                </div>
              </div>
            </div>
          )}

          {/* Navigation Buttons */}
          <div className="flex justify-between pt-6 border-t">
            {step > 0 ? (
              <button
                type="button"
                onClick={handleBack}
                className="inline-flex items-center gap-1.5 rounded-md border border-input bg-background px-4 py-2 text-sm font-medium hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <ChevronLeft className="h-4 w-4" /> Back
              </button>
            ) : (
              <button
                type="button"
                onClick={() => safeNavigate('/plans')}
                className="rounded-md border border-input bg-background px-4 py-2 text-sm font-medium hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                Cancel
              </button>
            )}

            {step < STEPS.length - 1 ? (
              <button
                type="button"
                onClick={handleNext}
                className="inline-flex items-center gap-1.5 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
              >
                Next <ChevronRight className="h-4 w-4" />
              </button>
            ) : (
              <button
                type="button"
                onClick={handleSubmit(onSubmit)}
                disabled={isSubmitting || createPlan.isPending}
                className="inline-flex items-center gap-1.5 rounded-md bg-emerald-600 px-5 py-2 text-sm font-medium text-white hover:bg-emerald-500 disabled:opacity-50 transition-colors"
              >
                <CheckCircle2 className="h-4 w-4" />
                {createPlan.isPending || isSubmitting ? 'Creating...' : 'Create Backup Plan'}
              </button>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

