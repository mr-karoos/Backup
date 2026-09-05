'use client';

import * as React from 'react';
import { useRouter } from 'next/navigation';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { Play, X, AlertCircle } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import { useCreateBackupJob } from '@/lib/api/mutations';
import { usePermissions } from '@/lib/auth/permissions';
import { FormField } from '@/components/ui/form-field';
import { Select } from '@/components/ui/select';
import type { BackupType, EngineType, StorageTargetResponse } from '@/types/domain';

export interface ManualBackupDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // If running from a plan:
  plan?: {
    id: string;
    name: string;
    resourceName: string;
    backupType: string;
  };
  // If running ad-hoc from a resource:
  resource?: {
    id: string;
    name: string;
  };
}

export function ManualBackupDialog({
  open,
  onOpenChange,
  plan,
  resource,
}: ManualBackupDialogProps) {
  const router = useRouter();
  const { activeOrgId } = useAuth();
  const { canExecutePlanBackup, canExecuteAdHocBackup } = usePermissions();
  const createJob = useCreateBackupJob();
  const { data: storageTargets, isLoading: loadingStorage } = useQuery<StorageTargetResponse[]>({
    queryKey: activeOrgId ? queryKeys.org(activeOrgId).storageTargets.all() : ['disabled'],
    queryFn: () => apiClient.get<StorageTargetResponse[]>('/storage-targets'),
    enabled: !!activeOrgId,
  });

  // Ad-hoc form state
  const [backupType, setBackupType] = React.useState<BackupType>('mysql_database');
  const [selectedStorageTargetId, setSelectedStorageTargetId] = React.useState<string>('');
  const defaultStorageTargetId =
    storageTargets?.find((t) => t.is_default && t.status === 'active')?.id ||
    storageTargets?.find((t) => t.status === 'active')?.id ||
    '';
  const storageTargetId = selectedStorageTargetId || defaultStorageTargetId;
  const [databasesInput, setDatabasesInput] = React.useState<string>('');
  const [pathsInput, setPathsInput] = React.useState<string>('/var/www/html');
  const [excludeInput, setExcludeInput] = React.useState<string>('');
  const [validationError, setValidationError] = React.useState<string | null>(null);

  const isPlanMode = Boolean(plan);
  const canExecute = isPlanMode ? canExecutePlanBackup : canExecuteAdHocBackup;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setValidationError(null);

    if (!canExecute) {
      setValidationError('You do not have permission to execute this backup job.');
      return;
    }

    if (isPlanMode && plan) {
      await createJob.mutateAsync({
        backup_plan_id: plan.id,
      });
      onOpenChange(false);
      router.push('/runs');
      return;
    }

    // Ad-hoc validation
    if (!resource) return;
    if (!storageTargetId) {
      setValidationError('Please select a storage target.');
      return;
    }

    const engineType: EngineType = 'direct_stream';
    let targetSpec: { databases?: string[]; paths?: string[]; exclude_patterns?: string[] } = {};

    if (backupType === 'mysql_database') {
      const dbs = databasesInput
        .split(/[,\n]/)
        .map((s) => s.trim())
        .filter(Boolean);
      if (dbs.length === 0) {
        setValidationError('Please specify at least one database name for MySQL backup.');
        return;
      }
      targetSpec = { databases: dbs };
    } else {
      const paths = pathsInput
        .split(/[,\n]/)
        .map((s) => s.trim())
        .filter(Boolean);
      if (paths.length === 0) {
        setValidationError('Please specify at least one directory path for files backup.');
        return;
      }
      const excludes = excludeInput
        .split(/[,\n]/)
        .map((s) => s.trim())
        .filter(Boolean);
      targetSpec = { paths, exclude_patterns: excludes };
    }

    await createJob.mutateAsync({
      resource_id: resource.id,
      backup_type: backupType,
      engine_type: engineType,
      storage_target_id: storageTargetId,
      target_spec: targetSpec,
    });
    onOpenChange(false);
    router.push('/runs');
  };

  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/60 backdrop-blur-sm data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />
        <DialogPrimitive.Content
          aria-describedby="backup-dialog-description"
          className="fixed left-[50%] top-[50%] z-50 grid w-full max-w-lg translate-x-[-50%] translate-y-[-50%] gap-4 border border-zinc-800 bg-zinc-900 p-6 shadow-2xl duration-200 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 sm:rounded-lg"
        >
          <div className="flex items-center justify-between">
            <div className="flex items-center space-x-2">
              <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-emerald-950/60 border border-emerald-800/50 text-emerald-400">
                <Play className="h-5 w-5 fill-current" />
              </div>
              <div>
                <DialogPrimitive.Title className="text-lg font-semibold text-zinc-100">
                  {isPlanMode ? 'Execute Backup Plan' : 'Run Ad-Hoc Backup'}
                </DialogPrimitive.Title>
                <DialogPrimitive.Description
                  id="backup-dialog-description"
                  className="text-xs text-zinc-400"
                >
                  {isPlanMode
                    ? `Queue immediate execution for plan "${plan?.name}".`
                    : `Trigger manual backup on resource "${resource?.name}".`}
                </DialogPrimitive.Description>
              </div>
            </div>
            <DialogPrimitive.Close
              aria-label="Close"
              className="rounded-sm opacity-70 transition-opacity hover:opacity-100 focus:outline-none text-zinc-400 hover:text-zinc-100"
            >
              <X className="h-5 w-5" />
            </DialogPrimitive.Close>
          </div>

          {validationError && (
            <div className="flex items-center gap-2 rounded-md border border-rose-800/60 bg-rose-950/40 p-3 text-xs text-rose-300">
              <AlertCircle className="h-4 w-4 shrink-0" />
              <span>{validationError}</span>
            </div>
          )}

          <form onSubmit={handleSubmit} className="space-y-4">
            {isPlanMode ? (
              <div className="rounded-lg border border-zinc-800 bg-zinc-950 p-4 space-y-2 text-sm text-zinc-300">
                <div className="flex justify-between">
                  <span className="text-zinc-500">Plan:</span>
                  <span className="font-medium text-zinc-200">{plan?.name}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-zinc-500">Target Resource:</span>
                  <span className="text-zinc-200">{plan?.resourceName}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-zinc-500">Backup Type:</span>
                  <span className="capitalize text-zinc-200">
                    {plan?.backupType.replace('_', ' ')}
                  </span>
                </div>
                <p className="mt-2 text-xs text-zinc-400 border-t border-zinc-850 pt-2">
                  This plan will execute with its pre-configured engine, selection, and retention policy.
                </p>
              </div>
            ) : (
              <div className="space-y-4">
                <FormField label="Backup Type" htmlFor="backup-type" required>
                  <Select
                    id="backup-type"
                    value={backupType}
                    onChange={(e) => setBackupType(e.target.value as BackupType)}
                    options={[
                      { value: 'mysql_database', label: 'MySQL Database' },
                      { value: 'website_files', label: 'Website Files' },
                    ]}
                  />
                </FormField>

                <FormField
                  label="Storage Target"
                  htmlFor="storage-target"
                  required
                  description="Target destination for generated backup archives"
                >
                  <Select
                    id="storage-target"
                    value={storageTargetId}
                    onChange={(e) => setSelectedStorageTargetId(e.target.value)}
                    disabled={loadingStorage}
                    options={
                      storageTargets
                        ?.filter((t) => t.status === 'active')
                        .map((t) => ({
                          value: t.id,
                          label: `${t.name} (${t.type})`,
                        })) || []
                    }
                  />
                </FormField>

                {backupType === 'mysql_database' ? (
                  <FormField
                    label="Database Names"
                    htmlFor="databases"
                    required
                    description="Comma or newline separated MySQL database names"
                  >
                    <textarea
                      id="databases"
                      rows={3}
                      value={databasesInput}
                      onChange={(e) => setDatabasesInput(e.target.value)}
                      placeholder="db_production, db_analytics"
                      className="w-full rounded-md border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm text-zinc-100 placeholder-zinc-500 focus:border-emerald-500 focus:outline-none font-mono"
                    />
                  </FormField>
                ) : (
                  <>
                    <FormField
                      label="Directory Paths"
                      htmlFor="paths"
                      required
                      description="Absolute POSIX directory paths to back up"
                    >
                      <input
                        id="paths"
                        type="text"
                        value={pathsInput}
                        onChange={(e) => setPathsInput(e.target.value)}
                        placeholder="/var/www/html"
                        className="w-full rounded-md border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm text-zinc-100 placeholder-zinc-500 focus:border-emerald-500 focus:outline-none font-mono"
                      />
                    </FormField>

                    <FormField
                      label="Exclude Patterns (Optional)"
                      htmlFor="excludes"
                      description="Comma separated patterns to skip (e.g. *.log, cache/*)"
                    >
                      <input
                        id="excludes"
                        type="text"
                        value={excludeInput}
                        onChange={(e) => setExcludeInput(e.target.value)}
                        placeholder="*.log, cache/*, tmp/*"
                        className="w-full rounded-md border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm text-zinc-100 placeholder-zinc-500 focus:border-emerald-500 focus:outline-none font-mono"
                      />
                    </FormField>
                  </>
                )}
              </div>
            )}

            <div className="mt-6 flex justify-end gap-3 pt-2">
              <button
                type="button"
                onClick={() => onOpenChange(false)}
                className="rounded-md border border-zinc-700 bg-zinc-800 px-4 py-2 text-sm font-medium text-zinc-200 hover:bg-zinc-700 focus:outline-none"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={createJob.isPending || !canExecute}
                className="inline-flex items-center justify-center gap-2 rounded-md bg-emerald-600 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-500 focus:outline-none disabled:opacity-50"
              >
                {createJob.isPending ? 'Queuing...' : 'Run Backup Now'}
              </button>
            </div>
          </form>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
