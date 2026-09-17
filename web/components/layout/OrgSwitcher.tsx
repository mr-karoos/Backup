'use client';

import { useAuth } from '@/lib/auth/auth-context';
import { cn } from '@/lib/utils';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Button } from '@/components/ui/button';
import { Building2, Check, ChevronsUpDown } from 'lucide-react';

export function OrgSwitcher() {
  const { memberships, activeOrgId, activeMembership, switchOrganization } = useAuth();

  if (memberships.length === 0) {
    return (
      <div className="flex items-center gap-2 px-3 py-1.5 text-xs text-muted-foreground">
        <Building2 className="h-4 w-4" />
        <span>No Organization</span>
      </div>
    );
  }

  const currentName = activeMembership?.organization_name || 'Select Organization';

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className="w-full justify-between gap-2 px-3 text-left font-normal h-10 md:h-9"
          aria-label="Switch organization"
        >
          <div className="flex items-center gap-2 truncate">
            <Building2 className="h-4 w-4 shrink-0 text-muted-foreground" />
            <span className="truncate font-medium text-foreground">{currentName}</span>
          </div>
          <ChevronsUpDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-60">
        <DropdownMenuLabel className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
          Organizations
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {memberships.map((membership) => {
          const isSelected = membership.organization_id === activeOrgId;
          return (
            <DropdownMenuItem
              key={membership.organization_id}
              onClick={() => switchOrganization(membership.organization_id)}
              className="flex items-center justify-between cursor-pointer py-2"
            >
              <div className="flex flex-col truncate pr-2">
                <span className={cn('text-sm truncate', isSelected ? 'font-semibold text-foreground' : 'font-medium text-foreground')}>
                  {membership.organization_name}
                </span>
                <span className="text-xs text-muted-foreground capitalize">
                  {membership.role}
                </span>
              </div>
              {isSelected && <Check className="h-4 w-4 text-primary shrink-0 stroke-[2.5]" />}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
