import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

// Standard mock responses matching Go backend contracts
const mockUser = {
  id: 'user-001',
  email: 'admin@domain.com',
  full_name: 'Admin Operator',
  is_system_admin: true,
};

const mockViewerUser = {
  id: 'user-002',
  email: 'viewer@domain.com',
  full_name: 'Viewer Operator',
  is_system_admin: false,
};

const mockMemberships = [
  {
    organization_id: 'org-1111',
    organization_name: 'Acme Primary',
    organization_slug: 'acme-primary',
    is_default_internal: true,
    role: 'admin',
    status: 'active',
    permissions: ['resource:read', 'resource:write', 'credential:read'],
  },
  {
    organization_id: 'org-2222',
    organization_name: 'Beta Secondary',
    organization_slug: 'beta-secondary',
    is_default_internal: false,
    role: 'viewer',
    status: 'active',
    permissions: ['resource:read'],
  },
];

const mockResources = [
  {
    id: 'res-101',
    name: 'Primary Database Server',
    type: 'linux_server',
    status: 'active',
    last_connection_test_at: '2026-08-01T12:00:00Z',
    last_connection_status: 'success',
    created_at: '2026-07-15T08:00:00Z',
    connector: {
      host: '10.0.0.15',
      port: 22,
      auth_type: 'ssh_key',
      username: 'backup-user',
      credential_name: 'Production SSH Key',
    },
  },
];

const mockPlans = [
  {
    id: 'plan-201',
    resource_id: 'res-101',
    resource_name: 'Primary Database Server',
    name: 'Nightly Database Backup',
    backup_type: 'mysql_database',
    engine_type: 'mysqldump',
    storage_target_id: 'tgt-301',
    status: 'active',
    database_selection: {
      mode: 'selected',
      databases: ['production_db', 'billing_db'],
    },
    schedule: {
      is_enabled: true,
      cron_expression: '0 2 * * *',
      timezone: 'UTC',
      next_run_at: '2026-08-05T02:00:00Z',
    },
    retention_policy: {
      keep_last_n: 14,
      keep_days: 30,
    },
    created_at: '2026-07-20T09:00:00Z',
  },
];

const mockRuns = [
  {
    id: 'run-501',
    job_id: 'job-401',
    resource_id: 'res-101',
    attempt_number: 1,
    status: 'success',
    started_at: '2026-08-04T02:00:00Z',
    ended_at: '2026-08-04T02:03:15Z',
    duration_seconds: 195,
    total_artifact_size_bytes: 104857600, // 100 MB
    artifacts_count: 1,
    created_at: '2026-08-04T02:00:00Z',
  },
];

const mockArtifacts = [
  {
    id: 'art-601',
    run_id: 'run-501',
    resource_id: 'res-101',
    artifact_name: 'production_db_20260804.sql.gz',
    size_bytes: 104857600,
    checksum_sha256: '9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08',
    compression_type: 'gzip',
    verification_status: 'verified',
    verified_at: '2026-08-04T02:04:00Z',
    created_at: '2026-08-04T02:03:15Z',
  },
];

const mockStorageTargets = [
  {
    id: 'tgt-301',
    name: 'Primary S3 Bucket',
    type: 's3',
    status: 'active',
    is_default: true,
    s3_config: {
      bucket: 'corporate-backups',
      region: 'eu-central-1',
      endpoint: '',
      force_path_style: false,
    },
    created_at: '2026-07-10T12:00:00Z',
    updated_at: '2026-07-10T12:00:00Z',
  },
];

const mockCredentials = [
  {
    id: 'cred-901',
    name: 'Production SSH Key',
    type: 'ssh_private_key',
    fingerprint: 'SHA256:abc123def456ghi789jkl012mno345pqr678stu901',
    key_version: 1,
    created_at: '2026-07-10T11:00:00Z',
  },
];

const mockOrgDetail = {
  id: 'org-1111',
  name: 'Acme Primary',
  slug: 'acme-primary',
  is_default_internal: true,
  status: 'active',
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
};

async function setupApiMocks(page: any, userRole: 'admin' | 'viewer' = 'admin') {
  await page.route('**/api/v1/**', async (route: any) => {
    const url = route.request().url();

    // Health
    if (url.includes('/api/v1/health')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'ok' }),
      });
    }

    // Refresh
    if (url.includes('/api/v1/auth/refresh')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            tokens: {
              access_token: 'mock-refreshed-jwt',
              token_type: 'Bearer',
              expires_in: 900,
            },
          },
        }),
      });
    }

    // Login
    if (url.includes('/api/v1/auth/login')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            user: userRole === 'admin' ? mockUser : mockViewerUser,
            tokens: {
              access_token: 'mock-access-jwt',
              token_type: 'Bearer',
              expires_in: 900,
            },
            default_organization_id: 'org-1111',
          },
        }),
      });
    }

    // Me
    if (url.includes('/api/v1/auth/me')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            user: userRole === 'admin' ? mockUser : mockViewerUser,
            memberships: mockMemberships,
          },
        }),
      });
    }

    // Resources
    if (url.includes('/api/v1/resources/res-101')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockResources[0] }),
      });
    }
    if (url.includes('/api/v1/resources')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockResources }),
      });
    }

    // Plans
    if (url.includes('/api/v1/backup-plans/plan-201')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockPlans[0] }),
      });
    }
    if (url.includes('/api/v1/backup-plans')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockPlans }),
      });
    }

    // Runs
    if (url.includes('/api/v1/backup-runs/run-501')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockRuns[0] }),
      });
    }
    if (url.includes('/api/v1/backup-runs')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockRuns }),
      });
    }

    // Artifacts
    if (url.includes('/api/v1/backup-artifacts/art-601')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockArtifacts[0] }),
      });
    }
    if (url.includes('/api/v1/backup-artifacts')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockArtifacts }),
      });
    }

    // Storage
    if (url.includes('/api/v1/storage-targets/tgt-301')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockStorageTargets[0] }),
      });
    }
    if (url.includes('/api/v1/storage-targets')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockStorageTargets }),
      });
    }

    // Credentials
    if (url.includes('/api/v1/credentials')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockCredentials }),
      });
    }

    // Organization Detail
    if (url.includes('/api/v1/organizations/org-1111')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockOrgDetail }),
      });
    }

    // Default 200
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: [] }),
    });
  });
}

test.describe('Backup Platform Frontend E2E Suite', () => {
  test('1. Successful Login flow to Dashboard', async ({ page }) => {
    // Intercept bootstrap refresh to return 401 initially (forcing login page)
    await page.route('**/api/v1/auth/refresh', async (route) => {
      route.fulfill({ status: 401, json: { error: { code: 'UNAUTHORIZED' } } });
    });

    await page.route('**/api/v1/auth/login', async (route) => {
      route.fulfill({
        status: 200,
        json: {
          data: {
            user: mockUser,
            tokens: { access_token: 'valid-jwt', token_type: 'Bearer', expires_in: 900 },
            default_organization_id: 'org-1111',
          },
        },
      });
    });

    await page.route('**/api/v1/auth/me', async (route) => {
      route.fulfill({
        status: 200,
        json: {
          data: {
            user: mockUser,
            memberships: mockMemberships,
          },
        },
      });
    });

    await page.goto('/login');

    await expect(page.getByRole('heading', { name: 'Backup Platform' })).toBeVisible();

    await page.fill('#email', 'admin@domain.com');
    await page.fill('#password', 'SecretP@ssword123');
    await page.click('button[type="submit"]');

    // Should redirect to dashboard
    await expect(page).toHaveURL('/');
    await expect(page.getByRole('heading', { name: 'Dashboard Overview' })).toBeVisible();
  });

  test('2. Initial Bootstrap Session loads Dashboard directly if refresh cookie valid', async ({
    page,
  }) => {
    await setupApiMocks(page);

    await page.goto('/');

    await expect(page.getByRole('heading', { name: 'Dashboard Overview' })).toBeVisible();
    await expect(page.getByText('Acme Primary')).toBeVisible();
  });

  test('3. Role-Aware Navigation: Admin can see Credentials; Viewer cannot', async ({ page }) => {
    // Admin user
    await setupApiMocks(page, 'admin');
    await page.goto('/');
    await expect(page.getByRole('link', { name: 'Credentials' })).toBeVisible();

    // Viewer user
    await setupApiMocks(page, 'viewer');
    await page.goto('/');
    await expect(page.getByRole('link', { name: 'Credentials' })).not.toBeVisible();
  });

  test('4. Resources list and detail navigation', async ({ page }) => {
    await setupApiMocks(page);
    await page.goto('/resources');

    await expect(page.getByRole('heading', { name: 'Protected Resources' })).toBeVisible();
    const resourceLink = page.getByRole('link', { name: 'Primary Database Server', exact: true });
    await expect(resourceLink).toBeVisible();

    // Click resource link
    await resourceLink.click();
    await expect(page).toHaveURL('/resources/res-101');
    await expect(page.getByRole('heading', { name: 'Primary Database Server' })).toBeVisible();
    await expect(page.getByText('10.0.0.15:22')).toBeVisible();
  });

  test('5. Backup Plans list and detail navigation', async ({ page }) => {
    await setupApiMocks(page);
    await page.goto('/plans');

    await expect(page.getByRole('heading', { name: 'Backup Plans' })).toBeVisible();
    const planLink = page.getByRole('link', { name: 'Nightly Database Backup', exact: true });
    await expect(planLink).toBeVisible();

    await planLink.click();
    await expect(page).toHaveURL('/plans/plan-201');
    await expect(page.getByRole('heading', { name: 'Nightly Database Backup' })).toBeVisible();
    await expect(page.getByText('0 2 * * *')).toBeVisible();
  });

  test('6. Backup Runs list and detail navigation', async ({ page }) => {
    await setupApiMocks(page);
    await page.goto('/runs');

    await expect(page.getByRole('heading', { name: 'Backup Run History' })).toBeVisible();
    const runLink = page.getByRole('link', { name: /run-501/ }).first();
    await expect(runLink).toBeVisible();

    await runLink.click();
    await expect(page).toHaveURL('/runs/run-501');
    await expect(page.getByRole('heading', { name: /Run run-501/ })).toBeVisible();
  });

  test('7. Backup Artifacts list and detail navigation (send NO query params)', async ({ page }) => {
    let capturedQuery = '';
    await page.route('**/api/v1/backup-artifacts**', async (route) => {
      const url = new URL(route.request().url());
      capturedQuery = url.search;
      if (url.pathname === '/api/v1/backup-artifacts') {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: mockArtifacts }),
        });
      }
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockArtifacts[0] }),
      });
    });

    await setupApiMocks(page);
    await page.goto('/artifacts');

    expect(capturedQuery).toBe(''); // No query parameters sent
    await expect(page.getByRole('heading', { name: 'Backup Artifacts' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'production_db_20260804.sql.gz', exact: true })).toBeVisible();
  });

  test('8. Dark Mode Toggle adds dark class to document root', async ({ page }) => {
    await setupApiMocks(page);
    await page.goto('/');

    await page.click('button[aria-label="Toggle theme"]');
    await page.click('text=Dark');

    const hasDarkClass = await page.evaluate(() =>
      document.documentElement.classList.contains('dark')
    );
    expect(hasDarkClass).toBe(true);
  });

  test('9. Accessibility Smoke Check with Axe (zero serious/critical violations)', async ({ page }) => {
    await setupApiMocks(page);

    // 1. Check Login Page
    await page.route('**/api/v1/auth/refresh', async (route) => {
      route.fulfill({ status: 401, json: { error: { code: 'UNAUTHORIZED' } } });
    });
    await page.goto('/login');
    const loginAxe = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze();
    const loginViolations = loginAxe.violations.filter(
      (v) => v.impact === 'critical' || v.impact === 'serious'
    );
    expect(loginViolations).toEqual([]);

    // 2. Check Dashboard
    await setupApiMocks(page);
    await page.goto('/');
    const dashboardAxe = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa'])
      .analyze();
    const dashboardViolations = dashboardAxe.violations.filter(
      (v) => v.impact === 'critical' || v.impact === 'serious'
    );
    expect(dashboardViolations).toEqual([]);
  });
});
