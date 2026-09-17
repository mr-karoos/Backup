import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuLabel,
} from '@/components/ui/dropdown-menu';
import { OrgSwitcher } from '@/components/layout/OrgSwitcher';
import { Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
import { Button } from '@/components/ui/button';
import * as AuthContextModule from '@/lib/auth/auth-context';

describe('Frontend Overlay & Contrast Regressions', () => {
  describe('Dialog Primitives', () => {
    it('renders dialog content and overlay with high-contrast surfaces and boundaries', () => {
      render(
        <Dialog open={true}>
          <DialogContent data-testid="dialog-content">
            <DialogHeader>
              <DialogTitle data-testid="dialog-title">Test Title</DialogTitle>
              <DialogDescription data-testid="dialog-desc">Test Description</DialogDescription>
            </DialogHeader>
          </DialogContent>
        </Dialog>
      );

      const content = screen.getByTestId('dialog-content');
      expect(content.className).toContain('bg-background');
      expect(content.className).toContain('text-foreground');
      expect(content.className).toContain('border-border');
      expect(content.className).toContain('shadow-xl');

      const title = screen.getByTestId('dialog-title');
      expect(title.className).toContain('text-foreground');

      const desc = screen.getByTestId('dialog-desc');
      expect(desc.className).toContain('text-muted-foreground');
    });
  });

  describe('Dropdown Menu Primitives', () => {
    it('renders dropdown menu content and separator with explicit borders and readable tokens', () => {
      render(
        <DropdownMenu open={true}>
          <DropdownMenuTrigger asChild>
            <button>Open</button>
          </DropdownMenuTrigger>
          <DropdownMenuContent data-testid="dropdown-content">
            <DropdownMenuLabel data-testid="dropdown-label">Section</DropdownMenuLabel>
            <DropdownMenuSeparator data-testid="dropdown-separator" />
            <DropdownMenuItem data-testid="dropdown-item">Item 1</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      );

      const content = screen.getByTestId('dropdown-content');
      expect(content.className).toContain('bg-popover');
      expect(content.className).toContain('text-popover-foreground');
      expect(content.className).toContain('border-border');
      expect(content.className).toContain('shadow-lg');

      const separator = screen.getByTestId('dropdown-separator');
      expect(separator.className).toContain('bg-border');

      const item = screen.getByTestId('dropdown-item');
      expect(item.className).toContain('text-foreground');
      expect(item.className).toContain('focus:bg-accent');
      expect(item.className).toContain('focus:text-accent-foreground');
    });
  });

  describe('OrgSwitcher Component', () => {
    it('renders org switcher trigger button with foreground text and clear styling', () => {
      vi.spyOn(AuthContextModule, 'useAuth').mockReturnValue({
        status: 'authenticated',
        user: { id: 'u1', email: 'admin@domain.com', full_name: 'Admin', is_system_admin: false },
        memberships: [
          {
            organization_id: 'org-1',
            organization_name: 'Acme Corp',
            organization_slug: 'acme-corp',
            is_default_internal: true,
            role: 'admin',
            status: 'active',
            permissions: ['all'],
          },
        ],
        activeOrgId: 'org-1',
        activeMembership: {
          organization_id: 'org-1',
          organization_name: 'Acme Corp',
          organization_slug: 'acme-corp',
          is_default_internal: true,
          role: 'admin',
          status: 'active',
          permissions: ['all'],
        },
        isSystemAdmin: false,
        userRole: 'admin',
        login: vi.fn(),
        logout: vi.fn(),
        switchOrganization: vi.fn(),
      });

      render(<OrgSwitcher />);

      const triggerBtn = screen.getByRole('button', { name: /switch organization/i });
      expect(triggerBtn).toBeInTheDocument();
      expect(screen.getByText('Acme Corp').className).toContain('text-foreground');
    });
  });

  describe('Form Controls & Buttons', () => {
    it('Input component applies border-input, bg-background, and text-foreground', () => {
      render(<Input data-testid="test-input" placeholder="Test placeholder" />);
      const input = screen.getByTestId('test-input');
      expect(input.className).toContain('border-input');
      expect(input.className).toContain('bg-background');
      expect(input.className).toContain('text-foreground');
      expect(input.className).toContain('shadow-xs');
    });

    it('Select component applies border-input, bg-background, and text-foreground', () => {
      render(
        <Select data-testid="test-select" options={[{ value: '1', label: 'Option 1' }]} />
      );
      const select = screen.getByTestId('test-select');
      expect(select.className).toContain('border-input');
      expect(select.className).toContain('bg-background');
      expect(select.className).toContain('text-foreground');
      expect(select.className).toContain('shadow-xs');
    });

    it('Button outline variant applies border-input, bg-background, and text-foreground', () => {
      render(<Button variant="outline">Outline Action</Button>);
      const btn = screen.getByRole('button', { name: 'Outline Action' });
      expect(btn.className).toContain('border-input');
      expect(btn.className).toContain('bg-background');
      expect(btn.className).toContain('text-foreground');
      expect(btn.className).toContain('shadow-xs');
    });
  });
});
