'use client';

import * as React from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import {
  ArrowLeft,
  Calendar,
  ShieldAlert,
  Database,
  FolderTree,
  CheckCircle2,
  ChevronRight,
  ChevronLeft,
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
import type {
  ResourceResponse,
  StorageTargetResponse,
  BackupType,
  EngineType,
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

  // Form State
  const [name, setName] = React.useState('');
  const [selectedResourceId, setSelectedResourceId] = React.useState('');
  const [backupType, setBackupType] = React.useState<BackupType>('mysql_database');
  const [engineType] = React.useState<EngineType>('direct_stream');

  // MySQL Selection
  const [dbMode, setDbMode] = React.useState<'all' | 'selected'>('all');
  const [selectedDatabases, setSelectedDatabases] = React.useState<string[]>([]);
  const [manualDbInput, setManualDbInput] = React.useState('');
  const [discoveredDbs, setDiscoveredDbs] = React.useState<string[]>([]);

  // Website Files Selection
  const [paths, setPaths] = React.useState<string>('/var/www/html');
  const [excludes, setExcludes] = React.useState<string>('*.log, cache/*, tmp/*');

  // Schedule
  const [schedulePreset, setSchedulePreset] = React.useState<'daily' | '12h' | 'weekly' | 'custom'>('daily');
  const [cronExpression, setCronExpression] = React.useState('0 2 * * *');
  const [timezone, setTimezone] = React.useState('UTC');

  // Storage & Retention
  const [selectedStorageTargetId, setSelectedStorageTargetId] = React.useState('');
  const [keepLastN, setKeepLastN] = React.useState<number>(7);
  const [keepDays, setKeepDays] = React.useState<number>(30);

  const [validationError, setValidationError] = React.useState<string | null>(null);

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

  const defaultResourceId = resources?.find((r) => r.status === 'active')?.id || '';
  const resourceId = selectedResourceId || defaultResourceId;
  const setResourceId = setSelectedResourceId;

  const defaultStorageTargetId =
    storageTargets?.find((t) => t.is_default && t.status === 'active')?.id ||
    storageTargets?.find((t) => t.status === 'active')?.id ||
    '';
  const storageTargetId = selectedStorageTargetId || defaultStorageTargetId;
  const setStorageTargetId = setSelectedStorageTargetId;

  // Schedule presets updater
  const handlePresetChange = (preset: 'daily' | '12h' | 'weekly' | 'custom') => {
    setSchedulePreset(preset);
    if (preset === 'daily') setCronExpression('0 2 * * *');
    else if (preset === '12h') setCronExpression('0 */12 * * *');
    else if (preset === 'weekly') setCronExpression('0 2 * * 0');
  };

  const handleDiscover = async () => {
    if (!resourceId) return;
    try {
      const res = await discoverDbs.mutateAsync(resourceId);
      const names = res.map((d) => d.name);
      setDiscoveredDbs(names);
      if (names.length > 0 && selectedDatabases.length === 0) {
        setSelectedDatabases(names);
      }
    } catch {
      // toast shown
    }
  };

  const isDirty = Boolean(name || resourceId);
  useUnsavedChanges(isDirty);

  useTenantFormGuard({
    onTenantChanged: () => {
      setName('');
      setResourceId('');
      router.push('/plans');
    },
  });

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

  const selectedResource = resources?.find((r) => r.id === resourceId);
  const selectedStorage = storageTargets?.find((t) => t.id === storageTargetId);

  const validateStep = (): boolean => {
    setValidationError(null);
    if (step === 0) {
      if (!name.trim()) {
        setValidationError('Plan name is required.');
        return false;
      }
      if (!resourceId) {
        setValidationError('Please select a target resource.');
        return false;
      }
    } else if (step === 2) {
      if (backupType === 'mysql_database') {
        if (dbMode === 'selected' && selectedDatabases.length === 0) {
          const manual = manualDbInput.split(/[,\n]/).map((s) => s.trim()).filter(Boolean);
          if (manual.length === 0) {
            setValidationError('Please specify at least one database name.');
            return false;
          }
        }
      } else {
        const pList = paths.split(/[,\n]/).map((s) => s.trim()).filter(Boolean);
        if (pList.length === 0) {
          setValidationError('Please specify at least one directory path.');
          return false;
        }
      }
    } else if (step === 3) {
      const parts = cronExpression.trim().split(/\s+/);
      if (parts.length !== 5) {
        setValidationError('Please provide a valid 5-field cron expression (e.g. "0 2 * * *").');
        return false;
      }
    } else if (step === 4) {
      if (!storageTargetId) {
        setValidationError('Please select a storage destination.');
        return false;
      }
    }
    return true;
  };

  const handleNext = () => {
    if (validateStep()) {
      setStep((s) => Math.min(s + 1, STEPS.length - 1));
    }
  };

  const handleBack = () => {
    setValidationError(null);
    setStep((s) => Math.max(s - 1, 0));
  };

  const handleSubmit = async () => {
    if (!validateStep()) return;

    const payload: CreateBackupPlanRequest = {
      name: name.trim(),
      resource_id: resourceId,
      backup_type: backupType,
      engine_type: engineType,
      storage_target_id: storageTargetId || undefined,
      schedule: {
        is_enabled: true,
        cron_expression: cronExpression.trim(),
        timezone: timezone || 'UTC',
      },
      retention_policy: {
        keep_last_n: Number(keepLastN) || undefined,
        keep_days: Number(keepDays) || undefined,
      },
    };

    if (backupType === 'mysql_database') {
      let dbs: string[] = [];
      if (dbMode === 'selected') {
        const manual = manualDbInput.split(/[,\n]/).map((s) => s.trim()).filter(Boolean);
        dbs = Array.from(new Set([...selectedDatabases, ...manual]));
      }
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
        <Link
          href="/plans"
          className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          aria-label="Back to plans"
        >
          <ArrowLeft className="h-5 w-5" />
        </Link>
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

      {validationError && (
        <div className="rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive">
          {validationError}
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
              <FormField label="Plan Name" htmlFor="plan-name" required>
                <input
                  id="plan-name"
                  type="text"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="e.g. Daily Production Database Backup"
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>

              <FormField
                label="Target Resource"
                htmlFor="plan-resource"
                required
                description="Select an active server to execute this backup plan on"
              >
                <Select
                  id="plan-resource"
                  value={resourceId}
                  onChange={(e) => setResourceId(e.target.value)}
                  disabled={loadingResources}
                  options={[
                    { value: '', label: '-- Select Resource --' },
                    ...(resources
                      ?.filter((r) => r.status === 'active')
                      .map((r) => ({
                        value: r.id,
                        label: `${r.name} (${r.type})`,
                      })) || []),
                  ]}
                />
              </FormField>
            </div>
          )}

          {/* Step 1: Backup Type */}
          {step === 1 && (
            <div className="space-y-4">
              <p className="text-sm text-muted-foreground">
                Select the backup category to create. Direct streaming engine will be utilized.
              </p>
              <div className="grid grid-cols-2 gap-4">
                <div
                  onClick={() => setBackupType('mysql_database')}
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
                    Consistent mysqldump execution with gzip compression streamed directly to destination storage.
                  </p>
                </div>

                <div
                  onClick={() => setBackupType('website_files')}
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
                    Directory tarball archive of web roots, configuration files, and assets with exclusion pattern support.
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
                          onChange={() => setDbMode('all')}
                          className="text-primary focus:ring-ring"
                        />
                        <span>Back up all databases</span>
                      </label>
                      <label className="flex items-center gap-2 text-sm text-foreground cursor-pointer">
                        <input
                          type="radio"
                          name="db-mode"
                          checked={dbMode === 'selected'}
                          onChange={() => setDbMode('selected')}
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
                            {discoverDbs.isPending ? 'Scanning...' : 'Scan from Host'}
                          </button>
                        )}
                      </div>

                      {discoveredDbs.length > 0 && (
                        <div className="space-y-1 rounded-md border p-3 bg-muted/20">
                          <p className="text-xs text-muted-foreground mb-2">
                            Select discovered databases:
                          </p>
                          <div className="grid grid-cols-2 gap-2">
                            {discoveredDbs.map((name) => (
                              <label
                                key={name}
                                className="flex items-center gap-2 text-xs font-mono text-foreground cursor-pointer"
                              >
                                <input
                                  type="checkbox"
                                  checked={selectedDatabases.includes(name)}
                                  onChange={(e) => {
                                    if (e.target.checked) {
                                      setSelectedDatabases([...selectedDatabases, name]);
                                    } else {
                                      setSelectedDatabases(
                                        selectedDatabases.filter((n) => n !== name)
                                      );
                                    }
                                  }}
                                  className="rounded border-input text-primary focus:ring-ring"
                                />
                                <span>{name}</span>
                              </label>
                            ))}
                          </div>
                        </div>
                      )}

                      <FormField
                        label="Additional or Manual Database Names"
                        htmlFor="manual-dbs"
                        description="Comma or newline separated database names"
                      >
                        <textarea
                          id="manual-dbs"
                          rows={3}
                          value={manualDbInput}
                          onChange={(e) => setManualDbInput(e.target.value)}
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
                    label="Directory Paths"
                    htmlFor="file-paths"
                    required
                    description="Comma or newline separated absolute paths (POSIX format)"
                  >
                    <textarea
                      id="file-paths"
                      rows={3}
                      value={paths}
                      onChange={(e) => setPaths(e.target.value)}
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
                      value={excludes}
                      onChange={(e) => setExcludes(e.target.value)}
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
                description="Minute Hour Day-of-Month Month Day-of-Week"
              >
                <input
                  id="cron-exp"
                  type="text"
                  value={cronExpression}
                  onChange={(e) => setCronExpression(e.target.value)}
                  placeholder="0 2 * * *"
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </FormField>

              <FormField label="Timezone" htmlFor="schedule-tz" required>
                <Select
                  id="schedule-tz"
                  value={timezone}
                  onChange={(e) => setTimezone(e.target.value)}
                  options={[
                    { value: 'UTC', label: 'UTC (Coordinated Universal Time)' },
                    { value: 'America/New_York', label: 'America/New_York (EST/EDT)' },
                    { value: 'America/Los_Angeles', label: 'America/Los_Angeles (PST/PDT)' },
                    { value: 'Europe/London', label: 'Europe/London (GMT/BST)' },
                    { value: 'Europe/Berlin', label: 'Europe/Berlin (CET/CEST)' },
                    { value: 'Asia/Tehran', label: 'Asia/Tehran (+03:30)' },
                    { value: 'Asia/Tokyo', label: 'Asia/Tokyo (JST)' },
                  ]}
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
                description="Destination for backup archives"
              >
                <Select
                  id="storage-target-plan"
                  value={storageTargetId}
                  onChange={(e) => setStorageTargetId(e.target.value)}
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
              </FormField>

              <div className="grid grid-cols-2 gap-4 pt-2 border-t">
                <FormField
                  label="Keep Last N Backups"
                  htmlFor="retention-count"
                  description="Number of successful runs to retain (0 = unlimited)"
                >
                  <input
                    id="retention-count"
                    type="number"
                    min={0}
                    value={keepLastN}
                    onChange={(e) => setKeepLastN(Number(e.target.value))}
                    className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </FormField>

                <FormField
                  label="Keep for N Days"
                  htmlFor="retention-days"
                  description="Maximum age in days before pruning (0 = unlimited)"
                >
                  <input
                    id="retention-days"
                    type="number"
                    min={0}
                    value={keepDays}
                    onChange={(e) => setKeepDays(Number(e.target.value))}
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
                  <span className="font-semibold text-foreground">{name}</span>
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
                  <span className="font-mono text-foreground">{cronExpression} ({timezone})</span>
                </div>
                <div className="flex justify-between border-b pb-2">
                  <span className="text-muted-foreground">Storage Destination:</span>
                  <span className="text-foreground">{selectedStorage?.name || storageTargetId}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Retention Policy:</span>
                  <span className="text-foreground">
                    Keep {keepLastN || 'all'} runs / {keepDays ? `${keepDays} days` : 'forever'}
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
              <Link
                href="/plans"
                className="rounded-md border border-input bg-background px-4 py-2 text-sm font-medium hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                Cancel
              </Link>
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
                onClick={handleSubmit}
                disabled={createPlan.isPending}
                className="inline-flex items-center gap-1.5 rounded-md bg-emerald-600 px-5 py-2 text-sm font-medium text-white hover:bg-emerald-500 disabled:opacity-50 transition-colors"
              >
                <CheckCircle2 className="h-4 w-4" />
                {createPlan.isPending ? 'Creating...' : 'Create Backup Plan'}
              </button>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
