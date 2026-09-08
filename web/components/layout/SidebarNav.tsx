'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { usePermissions } from '@/lib/auth/permissions';
import { cn } from '@/lib/utils';
import {
  LayoutDashboard,
  Server,
  Calendar,
  History,
  Archive,
  HardDrive,
  KeyRound,
  Settings,
  Activity,
  Wrench,
  FileText,
  type LucideIcon,
} from 'lucide-react';

interface NavItem {
  title: string;
  href: string;
  icon: LucideIcon;
  adminOnly?: boolean;
  requiredPermission?: 'canViewCredentials' | 'canViewAuditLogs';
}

const navItems: NavItem[] = [
  {
    title: 'Dashboard',
    href: '/',
    icon: LayoutDashboard,
  },
  {
    title: 'Resources',
    href: '/resources',
    icon: Server,
  },
  {
    title: 'Backup Plans',
    href: '/plans',
    icon: Calendar,
  },
  {
    title: 'Backup Runs',
    href: '/runs',
    icon: History,
  },
  {
    title: 'Artifacts',
    href: '/artifacts',
    icon: Archive,
  },
  {
    title: 'Storage Targets',
    href: '/storage',
    icon: HardDrive,
  },
  {
    title: 'Maintenance',
    href: '/maintenance',
    icon: Wrench,
  },
  {
    title: 'Audit Logs',
    href: '/audit-logs',
    icon: FileText,
    requiredPermission: 'canViewAuditLogs',
  },
  {
    title: 'Credentials',
    href: '/credentials',
    icon: KeyRound,
    adminOnly: true, // Only organization admin or system admin
  },
  {
    title: 'Settings',
    href: '/settings',
    icon: Settings,
  },
  {
    title: 'System Health',
    href: '/health',
    icon: Activity,
  },
];

export function SidebarNav({ onNavigate }: { onNavigate?: () => void }) {
  const pathname = usePathname();
  const { canViewCredentials, canViewAuditLogs } = usePermissions();

  const visibleItems = navItems.filter((item) => {
    if (item.adminOnly && !canViewCredentials) {
      return false;
    }
    if (item.requiredPermission === 'canViewAuditLogs' && !canViewAuditLogs) {
      return false;
    }
    return true;
  });

  return (
    <nav className="flex flex-col space-y-1" aria-label="Main Navigation">
      {visibleItems.map((item) => {
        const isActive =
          item.href === '/'
            ? pathname === '/'
            : pathname === item.href || pathname.startsWith(`${item.href}/`);

        const Icon = item.icon;

        return (
          <Link
            key={item.href}
            href={item.href}
            onClick={onNavigate}
            aria-current={isActive ? 'page' : undefined}
            className={cn(
              'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors min-h-[44px] md:min-h-[36px]',
              isActive
                ? 'bg-primary/10 text-primary font-semibold'
                : 'text-muted-foreground hover:bg-muted hover:text-foreground'
            )}
          >
            <Icon className={cn('h-4 w-4 shrink-0', isActive ? 'text-primary' : 'text-muted-foreground')} />
            <span>{item.title}</span>
          </Link>
        );
      })}
    </nav>
  );
}
