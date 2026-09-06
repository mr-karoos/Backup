import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { storageEditSchema } from '@/lib/forms/schemas';
import { EditStorageTargetDialog } from '@/app/(dashboard)/storage/[id]/page';
import * as AuthContextModule from '@/lib/auth/auth-context';
import { apiClient } from '@/lib/api/api-client';
import { queryKeys } from '@/lib/query/query-client';
import type { StorageTargetResponse } from '@/types/domain';

// Mock useRouter with a stable object reference across renders
const mockRouter = {
  push: vi.fn(),
  replace: vi.fn(),
  prefetch: vi.fn(),
};
vi.mock('next/navigation', () => ({
  useRouter: () => mockRouter,
  useParams: () => ({ id: 'st-1' }),
}));

// Mock useToast to prevent background auto-dismiss setTimeout handles
const mockToast = vi.fn();
vi.mock('@/lib/toast/toast-context', () => ({
  useToast: () => ({
    toast: mockToast,
    dismiss: vi.fn(),
  }),
  ToastProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        {children}
      </QueryClientProvider>
    );
  };
}

let currentOrgId: string = 'org-1';
function ControlledDialogHarness({ target }: { target: StorageTargetResponse }) {
  const [open, setOpen] = React.useState(true);
  return (
    <EditStorageTargetDialog
      target={target}
      open={open}
      onClose={() => setOpen(false)}
    />
  );
}

describe('Storage Edit with React Hook Form + Zod', () => {
  let queryClient: QueryClient;

  const sampleS3Target: StorageTargetResponse = {
    id: 'st-1',
    name: 'Primary S3 Storage',
    type: 's3',
    status: 'active',
    is_default: true,
    s3_config: {
      bucket: 'backup-bucket',
      region: 'us-east-1',
      endpoint: 'https://s3.amazonaws.com',
      force_path_style: false,
    },
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  };

  const sampleS3CompatibleTarget: StorageTargetResponse = {
    id: 'st-2',
    name: 'MinIO Local Storage',
    type: 's3_compatible',
    status: 'active',
    is_default: false,
    s3_config: {
      bucket: 'minio-bucket',
      region: 'us-east-1',
      endpoint: 'http://localhost:9000',
      force_path_style: true,
    },
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  };

  beforeEach(() => {
    vi.clearAllMocks();
    currentOrgId = 'org-1';
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });

    vi.spyOn(AuthContextModule, 'useAuth').mockImplementation(() => ({
      status: 'authenticated',
      user: { id: 'u1', email: 'admin@domain.com', full_name: 'Admin', is_system_admin: false },
      memberships: [],
      activeOrgId: currentOrgId,
      activeMembership: {
        organization_id: currentOrgId,
        organization_name: currentOrgId === 'org-1' ? 'Org 1' : 'Org 2',
        organization_slug: currentOrgId,
        is_default_internal: true,
        role: 'admin',
        status: 'active',
        permissions: ['storage:read', 'storage:write'],
      },
      isSystemAdmin: false,
      userRole: 'admin',
      login: vi.fn(),
      logout: vi.fn(),
      switchOrganization: vi.fn(),
    }));
  });

  describe('storageEditSchema Validation', () => {
    it('accepts valid S3 target payload', () => {
      const parsed = storageEditSchema.safeParse({
        name: 'Updated S3 Storage',
        type: 's3',
        bucket: 'new-bucket',
        region: 'eu-west-1',
        endpoint: 'https://s3.eu-west-1.amazonaws.com',
        force_path_style: true,
      });
      expect(parsed.success).toBe(true);
      if (parsed.success) {
        expect(parsed.data.name).toBe('Updated S3 Storage');
        expect(parsed.data.bucket).toBe('new-bucket');
        expect(parsed.data.region).toBe('eu-west-1');
        expect(parsed.data.force_path_style).toBe(true);
      }
    });

    it('rejects empty or whitespace-only name', () => {
      const empty = storageEditSchema.safeParse({
        name: '',
        type: 's3',
        bucket: 'bucket',
        region: 'us-east-1',
      });
      expect(empty.success).toBe(false);

      const whitespace = storageEditSchema.safeParse({
        name: '   ',
        type: 's3',
        bucket: 'bucket',
        region: 'us-east-1',
      });
      expect(whitespace.success).toBe(false);
    });

    it('rejects name exceeding 255 characters', () => {
      const parsed = storageEditSchema.safeParse({
        name: 'a'.repeat(256),
        type: 's3',
        bucket: 'bucket',
        region: 'us-east-1',
      });
      expect(parsed.success).toBe(false);
    });

    it('rejects S3 target missing bucket or region', () => {
      const missingBucket = storageEditSchema.safeParse({
        name: 'S3 Target',
        type: 's3',
        bucket: '',
        region: 'us-east-1',
      });
      expect(missingBucket.success).toBe(false);
      if (!missingBucket.success) {
        expect(missingBucket.error.issues[0]?.path).toContain('bucket');
      }

      const missingRegion = storageEditSchema.safeParse({
        name: 'S3 Target',
        type: 's3',
        bucket: 'my-bucket',
        region: '',
      });
      expect(missingRegion.success).toBe(false);
      if (!missingRegion.success) {
        expect(missingRegion.error.issues[0]?.path).toContain('region');
      }
    });

    it('accepts s3_compatible name-only update without requiring bucket/region', () => {
      const parsed = storageEditSchema.safeParse({
        name: 'Renamed S3 Compatible Target',
        type: 's3_compatible',
      });
      expect(parsed.success).toBe(true);
      if (parsed.success) {
        expect(parsed.data.name).toBe('Renamed S3 Compatible Target');
      }
    });

    it('accepts minimal name-only when type is omitted', () => {
      const parsed = storageEditSchema.safeParse({
        name: 'Simple Storage Target',
      });
      expect(parsed.success).toBe(true);
    });
  });

  describe('EditStorageTargetDialog Component Lifecycle & Security', () => {
    it('renders prefilled form fields for S3 storage target', async () => {
      render(
        <EditStorageTargetDialog
          target={sampleS3Target}
          open={true}
          onClose={vi.fn()}
        />,
        { wrapper: createWrapper(queryClient) }
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/target name/i)).toHaveValue('Primary S3 Storage');
        expect(screen.getByLabelText(/bucket name/i)).toHaveValue('backup-bucket');
        expect(screen.getByLabelText(/region/i)).toHaveValue('us-east-1');
        expect(screen.getByLabelText(/endpoint/i)).toHaveValue('https://s3.amazonaws.com');
      });
    });

    it('renders s3_compatible notice and does not display bucket/region edit inputs', async () => {
      render(
        <EditStorageTargetDialog
          target={sampleS3CompatibleTarget}
          open={true}
          onClose={vi.fn()}
        />,
        { wrapper: createWrapper(queryClient) }
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/target name/i)).toHaveValue('MinIO Local Storage');
        expect(screen.getByText(/endpoint and bucket parameters for s3-compatible targets are immutable/i)).toBeInTheDocument();
        expect(screen.queryByLabelText(/bucket name/i)).not.toBeInTheDocument();
        expect(screen.queryByLabelText(/region/i)).not.toBeInTheDocument();
      });
    });

    it('closes immediately on Cancel when form is pristine without window.confirm', async () => {
      const onClose = vi.fn();
      const confirmSpy = vi.spyOn(window, 'confirm');

      render(
        <EditStorageTargetDialog
          target={sampleS3Target}
          open={true}
          onClose={onClose}
        />,
        { wrapper: createWrapper(queryClient) }
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/target name/i)).toHaveValue('Primary S3 Storage');
      });

      fireEvent.click(screen.getByRole('button', { name: /cancel/i }));

      expect(confirmSpy).not.toHaveBeenCalled();
      expect(onClose).toHaveBeenCalledTimes(1);
    });

    it('warns with window.confirm on Cancel when form is dirty; stays open if rejected', async () => {
      const onClose = vi.fn();
      const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);

      render(
        <EditStorageTargetDialog
          target={sampleS3Target}
          open={true}
          onClose={onClose}
        />,
        { wrapper: createWrapper(queryClient) }
      );

      const nameInput = await screen.findByLabelText(/target name/i);
      fireEvent.change(nameInput, { target: { value: 'Modified Name' } });

      fireEvent.click(screen.getByRole('button', { name: /cancel/i }));

      expect(confirmSpy).toHaveBeenCalledWith(
        'You have unsaved changes. Are you sure you want to discard them?'
      );
      expect(onClose).not.toHaveBeenCalled();
    });

    it('closes and resets form when dirty Cancel is confirmed by user', async () => {
      const onClose = vi.fn();
      vi.spyOn(window, 'confirm').mockReturnValue(true);

      render(
        <EditStorageTargetDialog
          target={sampleS3Target}
          open={true}
          onClose={onClose}
        />,
        { wrapper: createWrapper(queryClient) }
      );

      const nameInput = await screen.findByLabelText(/target name/i);
      fireEvent.change(nameInput, { target: { value: 'Modified Name' } });

      fireEvent.click(screen.getByRole('button', { name: /cancel/i }));

      expect(onClose).toHaveBeenCalledTimes(1);
    });

    it('keeps dialog open and preserves edits on backend mutation failure', async () => {
      const onClose = vi.fn();
      vi.spyOn(apiClient, 'put').mockRejectedValueOnce(new Error('Network error'));

      render(
        <EditStorageTargetDialog
          target={sampleS3Target}
          open={true}
          onClose={onClose}
        />,
        { wrapper: createWrapper(queryClient) }
      );

      const nameInput = await screen.findByLabelText(/target name/i);
      fireEvent.change(nameInput, { target: { value: 'Attempted Name Change' } });

      fireEvent.click(screen.getByRole('button', { name: /save changes/i }));

      await waitFor(() => {
        expect(screen.getByLabelText(/target name/i)).toHaveValue('Attempted Name Change');
        expect(onClose).not.toHaveBeenCalled();
      });
    });

    it('submits valid changes, updates cache immediately, and makes exactly 1 PUT with 0 GET detail calls', async () => {
      const onClose = vi.fn();
      const updatedResponse: StorageTargetResponse = {
        ...sampleS3Target,
        name: 'Updated Production S3',
        s3_config: {
          ...sampleS3Target.s3_config!,
          bucket: 'updated-bucket',
          region: 'us-west-2',
        },
      };

      const putSpy = vi.spyOn(apiClient, 'put').mockResolvedValueOnce(updatedResponse);
      const getSpy = vi.spyOn(apiClient, 'get');

      // Prime existing cache
      queryClient.setQueryData(
        queryKeys.org('org-1').storage.detail('st-1'),
        sampleS3Target
      );

      render(
        <EditStorageTargetDialog
          target={sampleS3Target}
          open={true}
          onClose={onClose}
        />,
        { wrapper: createWrapper(queryClient) }
      );

      const nameInput = await screen.findByLabelText(/target name/i);
      const bucketInput = screen.getByLabelText(/bucket name/i);
      const regionInput = screen.getByLabelText(/region/i);

      fireEvent.change(nameInput, { target: { value: 'Updated Production S3' } });
      fireEvent.change(bucketInput, { target: { value: 'updated-bucket' } });
      fireEvent.change(regionInput, { target: { value: 'us-west-2' } });

      fireEvent.click(screen.getByRole('button', { name: /save changes/i }));

      await waitFor(() => {
        expect(putSpy).toHaveBeenCalledTimes(1);
        expect(putSpy).toHaveBeenCalledWith(
          '/storage-targets/st-1',
          {
            name: 'Updated Production S3',
            s3_config: {
              bucket: 'updated-bucket',
              region: 'us-west-2',
              endpoint: 'https://s3.amazonaws.com',
              force_path_style: false,
            },
          },
          { tenantOrgId: 'org-1' }
        );
        expect(onClose).toHaveBeenCalledTimes(1);
      });

      // Assert 0 GET detail calls were triggered
      const detailGetCalls = getSpy.mock.calls.filter(([url]) =>
        (url as string).includes('/storage-targets/st-1')
      );
      expect(detailGetCalls.length).toBe(0);

      // Assert authoritative cache update
      const cached = queryClient.getQueryData<StorageTargetResponse>(
        queryKeys.org('org-1').storage.detail('st-1')
      );
      expect(cached).toEqual(updatedResponse);

      // Assert tenant isolation: org-2 cache remains untouched
      const otherTenantCached = queryClient.getQueryData(
        queryKeys.org('org-2').storage.detail('st-1')
      );
      expect(otherTenantCached).toBeUndefined();
    });

    it('resets and closes safely on tenant change without prompting', async () => {
      const confirmSpy = vi.spyOn(window, 'confirm');

      currentOrgId = 'org-1';

      const { rerender } = render(<ControlledDialogHarness target={sampleS3Target} />, {
        wrapper: createWrapper(queryClient),
      });

      const nameInput = screen.getByLabelText(/target name/i);
      fireEvent.change(nameInput, { target: { value: 'Dirty Name Before Tenant Switch' } });

      // Switch active tenant org
      currentOrgId = 'org-2';

      rerender(<ControlledDialogHarness target={sampleS3Target} />);

      await waitFor(() => {
        expect(screen.queryByLabelText(/target name/i)).not.toBeInTheDocument();
        expect(confirmSpy).not.toHaveBeenCalled();
      });
    });
  });
});
