'use client';

import React, { useState, useEffect } from 'react';
import { useRouter, usePathname } from 'next/navigation';
import { useAuth } from '@/lib/auth/auth-context';
import { SidebarNav } from './SidebarNav';
import { OrgSwitcher } from './OrgSwitcher';
import { HealthBadge } from './HealthBadge';
import { ThemeToggle } from './ThemeToggle';
import { UserMenu } from './UserMenu';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog';
import { Menu, ShieldCheck, ChevronRight } from 'lucide-react';
import Link from 'next/link';

export function AppShell({ children }: { children: React.ReactNode }) {
  const { status } = useAuth();
  const router = useRouter();
  const pathname = usePathname();
  const [mobileOpen, setMobileOpen] = useState(false);

  // Close mobile drawer on route change
  const [prevPathname, setPrevPathname] = useState(pathname);
  if (pathname !== prevPathname) {
    setPrevPathname(pathname);
    setMobileOpen(false);
  }

  // Route protection
  useEffect(() => {
    if (status === 'unauthenticated') {
      router.replace('/login');
    }
  }, [status, router]);

  // Loading state during session bootstrap
  if (status === 'booting') {
    return (
      <div className="flex h-screen w-full items-center justify-center bg-background text-foreground">
        <div className="flex flex-col items-center space-y-4">
          <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-primary text-primary-foreground shadow-md animate-pulse">
            <ShieldCheck className="h-7 w-7" />
          </div>
          <p className="text-sm font-medium text-muted-foreground animate-pulse">
            Initializing Backup Platform session...
          </p>
        </div>
      </div>
    );
  }

  if (status === 'unauthenticated') {
    return null; // Will redirect via useEffect
  }

  // Derive simple breadcrumbs from pathname
  const pathSegments = pathname.split('/').filter(Boolean);
  const breadcrumbItems = pathSegments.map((segment, index) => {
    const href = '/' + pathSegments.slice(0, index + 1).join('/');
    const title = segment.charAt(0).toUpperCase() + segment.slice(1).replace(/-/g, ' ');
    return { title, href };
  });

  return (
    <div className="flex min-h-screen bg-background text-foreground">
      {/* Desktop Sidebar */}
      <aside className="hidden md:flex w-64 flex-col border-r bg-card shrink-0">
        <div className="flex h-16 items-center gap-2.5 border-b px-4">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-xs">
            <ShieldCheck className="h-5 w-5" />
          </div>
          <div className="flex flex-col">
            <span className="text-sm font-bold tracking-tight">Backup Platform</span>
            <span className="text-[10px] text-muted-foreground uppercase tracking-widest font-mono">
              Console
            </span>
          </div>
        </div>

        <div className="p-3 border-b">
          <OrgSwitcher />
        </div>

        <div className="flex-1 overflow-y-auto p-3">
          <SidebarNav />
        </div>

        <div className="border-t p-3 text-xs text-muted-foreground text-center">
          <span>Phase F1A Console</span>
        </div>
      </aside>

      {/* Mobile Drawer Navigation */}
      <Dialog open={mobileOpen} onOpenChange={setMobileOpen}>
        <DialogContent className="fixed inset-y-0 left-0 z-50 h-full w-72 border-r bg-card p-0 shadow-lg duration-200 translate-x-0 sm:max-w-xs">
          <div className="flex h-16 items-center gap-2.5 border-b px-4">
            <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-xs">
              <ShieldCheck className="h-5 w-5" />
            </div>
            <DialogTitle className="text-sm font-bold tracking-tight">Backup Platform</DialogTitle>
          </div>

          <div className="p-3 border-b">
            <OrgSwitcher />
          </div>

          <div className="flex-1 overflow-y-auto p-3">
            <SidebarNav onNavigate={() => setMobileOpen(false)} />
          </div>
        </DialogContent>
      </Dialog>

      {/* Main Content Area */}
      <div className="flex flex-1 flex-col overflow-hidden min-w-0">
        {/* Top Header */}
        <header className="sticky top-0 z-30 flex h-16 items-center justify-between border-b bg-background/95 backdrop-blur px-4 md:px-6 gap-4">
          <div className="flex items-center gap-3">
            {/* Mobile menu trigger */}
            <Button
              variant="ghost"
              size="icon"
              className="md:hidden h-10 w-10 min-h-[44px] min-w-[44px]"
              onClick={() => setMobileOpen(true)}
              aria-label="Open navigation menu"
            >
              <Menu className="h-5 w-5" />
            </Button>

            {/* Breadcrumbs */}
            <nav aria-label="Breadcrumbs" className="hidden sm:flex items-center text-xs text-muted-foreground">
              <Link href="/" className="hover:text-foreground transition-colors font-medium">
                Home
              </Link>
              {breadcrumbItems.map((item, idx) => (
                <span key={item.href} className="flex items-center">
                  <ChevronRight className="h-3.5 w-3.5 mx-1 opacity-50" />
                  {idx === breadcrumbItems.length - 1 ? (
                    <span className="font-semibold text-foreground truncate max-w-[160px]">
                      {item.title}
                    </span>
                  ) : (
                    <Link href={item.href} className="hover:text-foreground transition-colors truncate max-w-[120px]">
                      {item.title}
                    </Link>
                  )}
                </span>
              ))}
            </nav>
          </div>

          {/* Right utility icons */}
          <div className="flex items-center gap-2 md:gap-3">
            <HealthBadge />
            <ThemeToggle />
            <div className="h-4 w-px bg-border mx-0.5" />
            <UserMenu />
          </div>
        </header>

        {/* Page Content Body */}
        <main id="main-content" className="flex-1 overflow-y-auto p-4 md:p-6 lg:p-8">
          {children}
        </main>
      </div>
    </div>
  );
}
