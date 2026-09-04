/**
 * Pure, centralized formatting utilities for bytes, dates, durations, and statuses.
 */

export function formatBytes(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined || isNaN(bytes) || bytes < 0) {
    return '0 B';
  }
  if (bytes === 0) return '0 B';

  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  const safeIndex = Math.min(i, sizes.length - 1);
  const value = (bytes / Math.pow(k, safeIndex)).toFixed(1);

  return `${value.endsWith('.0') ? value.slice(0, -2) : value} ${sizes[safeIndex]}`;
}

export function formatDate(isoString: string | null | undefined): string {
  if (!isoString) return '—';
  try {
    const d = new Date(isoString);
    if (isNaN(d.getTime())) return '—';
    return new Intl.DateTimeFormat(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    }).format(d);
  } catch {
    return '—';
  }
}

export function formatDuration(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined || isNaN(seconds)) {
    return '—';
  }
  if (seconds < 0) return '0s';
  if (seconds < 60) return `${seconds}s`;

  const mins = Math.floor(seconds / 60);
  const remainingSecs = seconds % 60;
  if (mins < 60) {
    return `${mins}m ${remainingSecs}s`;
  }

  const hours = Math.floor(mins / 60);
  const remainingMins = mins % 60;
  return `${hours}h ${remainingMins}m`;
}

export function truncateId(id: string): string {
  if (!id) return '';
  if (id.length <= 8) return id;
  return id.substring(0, 8) + '...';
}

export function getStatusBadgeVariant(status: string): {
  label: string;
  variant: 'default' | 'secondary' | 'outline' | 'destructive' | 'success';
} {
  switch (status?.toLowerCase()) {
    case 'success':
    case 'verified':
    case 'active':
    case 'ok':
      return { label: status, variant: 'success' };
    case 'running':
    case 'pending':
      return { label: status, variant: 'secondary' };
    case 'paused':
      return { label: status, variant: 'outline' };
    case 'failed':
    case 'cancelled':
    case 'archived':
    case 'unavailable':
      return { label: status, variant: 'destructive' };
    default:
      return { label: status || 'unknown', variant: 'outline' };
  }
}
