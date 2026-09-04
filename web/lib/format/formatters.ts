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
    case 'unverified':
      return { label: 'unverified', variant: 'outline' };
    case 'disabled':
    case 'paused':
      return { label: status, variant: 'outline' };
    case 'unreachable':
      return { label: 'unreachable', variant: 'destructive' };
    case 'error':
    case 'failed':
    case 'cancelled':
    case 'archived':
    case 'unavailable':
      return { label: status, variant: 'destructive' };
    default:
      return { label: status || 'unknown', variant: 'outline' };
  }
}

/**
 * Conservative deterministic cron formatter for common platform backup patterns.
 * If expression does not strictly match known safe patterns, returns "Custom schedule".
 */
export function formatCronSchedule(cron: string | null | undefined): string {
  if (!cron) return 'Manual (no schedule)';
  const trimmed = cron.trim();

  // Pattern: "0 2 * * *" or "30 14 * * *" -> Daily at HH:MM
  const dailyMatch = trimmed.match(/^(\d{1,2})\s+(\d{1,2})\s+\*\s+\*\s+\*$/);
  if (dailyMatch) {
    const min = dailyMatch[1]!.padStart(2, '0');
    const hour = dailyMatch[2]!.padStart(2, '0');
    return `Daily at ${hour}:${min}`;
  }

  // Pattern: "0 */6 * * *" -> Every 6 hours
  const stepHourMatch = trimmed.match(/^0\s+\*\/(\d{1,2})\s+\*\s+\*\s+\*$/);
  if (stepHourMatch) {
    return `Every ${stepHourMatch[1]} hours`;
  }

  // Pattern: "0 * * * *" -> Hourly
  if (trimmed === '0 * * * *') {
    return 'Hourly';
  }

  // Pattern: "*/15 * * * *" -> Every 15 minutes
  const stepMinMatch = trimmed.match(/^\*\/(\d{1,2})\s+\*\s+\*\s+\*\s+\*$/);
  if (stepMinMatch) {
    return `Every ${stepMinMatch[1]} minutes`;
  }

  // Pattern: "0 2 * * 0" -> Weekly on Sunday at 02:00
  const daysOfWeek = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
  const weeklyMatch = trimmed.match(/^(\d{1,2})\s+(\d{1,2})\s+\*\s+\*\s+([0-6])$/);
  if (weeklyMatch) {
    const min = weeklyMatch[1]!.padStart(2, '0');
    const hour = weeklyMatch[2]!.padStart(2, '0');
    const day = daysOfWeek[parseInt(weeklyMatch[3]!, 10)] ?? 'Sunday';
    return `Weekly on ${day} at ${hour}:${min}`;
  }

  return 'Custom schedule';
}

export function formatResourceType(type: string): string {
  switch (type) {
    case 'ubuntu_ssh':
      return 'Ubuntu (SSH)';
    case 'cpanel':
      return 'cPanel';
    default:
      return type;
  }
}

export function formatBackupType(type: string): string {
  switch (type) {
    case 'mysql_database':
      return 'MySQL Database';
    case 'website_files':
      return 'Website Files';
    case 'both':
      return 'Database & Website Files';
    default:
      return type;
  }
}

export function formatStorageTargetType(type: string): string {
  switch (type) {
    case 'local':
      return 'Local Storage';
    case 's3':
      return 'Amazon / Standard S3';
    case 's3_compatible':
      return 'S3 Compatible';
    default:
      return type;
  }
}
