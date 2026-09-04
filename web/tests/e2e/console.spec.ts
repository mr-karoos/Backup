import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

// Canonical UUIDs conforming to A.2 backend standards
const USER_1_ID = '00000000-0000-0000-0000-000000000001';
const USER_2_ID = '00000000-0000-0000-0000-000000000002';
const ORG_1_ID = '11111111-1111-1111-1111-111111111111';
const ORG_2_ID = '22222222-2222-2222-2222-222222222222';
const RESOURCE_1_ID = '33333333-3333-3333-3333-333333333301';
const RESOURCE_2_ID = '33333333-3333-3333-3333-333333333302';
const PLAN_1_ID = '44444444-4444-4444-4444-444444444401';
const TARGET_1_ID = '55555555-5555-5555-5555-555555555501';
const TARGET_2_ID = '55555555-5555-5555-5555-555555555502';
const RUN_1_ID = '66666666-6666-6666-6666-666666666601';
const JOB_1_ID = '77777777-7777-7777-7777-777777777701';
const ARTIFACT_1_ID = '88888888-8888-8888-8888-888888888801';
const ARTIFACT_2_ID = '88888888-8888-8888-8888-888888888802';
const CREDENTIAL_1_ID = '99999999-9999-9999-9999-999999999901';

const mockUser = {
  id: USER_1_ID,
  email: 'admin@domain.com',
  full_name: 'Admin Operator',
  is_system_admin: true,
};

const mockViewerUser = {
  id: USER_2_ID,
  email: 'viewer@domain.com',
  full_name: 'Viewer Operator',
  is_system_admin: false,
};

const mockMemberships = [
  {
    organization_id: ORG_1_ID,
    organization_name: 'Acme Primary',
    organization_slug: 'acme-primary',
    is_default_internal: true,
    role: 'admin',
    status: 'active',
    permissions: ['resource:read', 'resource:write', 'credential:read'],
  },
  {
    organization_id: ORG_2_ID,
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
    id: RESOURCE_1_ID,
    name: 'Primary Database Server',
    type: 'ubuntu_ssh',
    status: 'active',
    last_connection_test_at: '2026-08-01T12:00:00Z',
    last_connection_status: 'success',
    created_at: '2026-07-15T08:00:00Z',
    connector: {
      host: '10.0.0.15',
      port: 22,
      auth_type: 'ssh_private_key',
      username: 'backup-user',
      credential_name: 'Production SSH Key',
      host_key_fingerprint: 'SHA256:abc123def456',
    },
  },
  {
    id: RESOURCE_2_ID,
    name: 'Shared Web Hosting',
    type: 'cpanel',
    status: 'active',
    last_connection_test_at: '2026-08-02T10:00:00Z',
    last_connection_status: 'success',
    created_at: '2026-07-16T08:00:00Z',
    connector: {
      host: 'cpanel.domain.com',
      port: 2083,
      auth_type: 'cpanel_api_token',
      username: 'cpanel-user',
      credential_name: 'Production cPanel Token',
    },
  },
];

const mockPlans = [
  {
    id: PLAN_1_ID,
    resource_id: RESOURCE_1_ID,
    resource_name: 'Primary Database Server',
    name: 'Nightly Database Backup',
    backup_type: 'mysql_database',
    engine_type: 'direct_stream',
    storage_target_id: TARGET_1_ID,
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
    id: RUN_1_ID,
    job_id: JOB_1_ID,
    resource_id: RESOURCE_1_ID,
    attempt_number: 1,
    status: 'success',
    started_at: '2026-08-04T02:00:00Z',
    ended_at: '2026-08-04T02:03:15Z',
    duration_seconds: 195,
    total_artifact_size_bytes: 157286400, // 150 MB
    artifacts_count: 2,
    created_at: '2026-08-04T02:00:00Z',
  },
];

const mockArtifacts = [
  {
    id: ARTIFACT_1_ID,
    run_id: RUN_1_ID,
    resource_id: RESOURCE_1_ID,
    artifact_name: 'production_db.sql.gz',
    size_bytes: 104857600,
    checksum_sha256: '9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08',
    compression_type: 'gzip',
    verification_status: 'verified',
    verified_at: '2026-08-04T02:04:00Z',
    created_at: '2026-08-04T02:03:15Z',
  },
  {
    id: ARTIFACT_2_ID,
    run_id: RUN_1_ID,
    resource_id: RESOURCE_1_ID,
    artifact_name: 'public_html.tar.gz',
    size_bytes: 52428800,
    checksum_sha256: 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
    compression_type: 'gzip',
    verification_status: 'unverified',
    verified_at: null,
    created_at: '2026-08-04T02:03:15Z',
  },
];

const mockStorageTargets = [
  {
    id: TARGET_1_ID,
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
  {
    id: TARGET_2_ID,
    name: 'MinIO Secondary Target',
    type: 's3_compatible',
    status: 'active',
    is_default: false,
    created_at: '2026-07-11T12:00:00Z',
    updated_at: '2026-07-11T12:00:00Z',
  },
];

const mockCredentials = [
  {
    id: CREDENTIAL_1_ID,
    name: 'Production SSH Key',
    type: 'ssh_private_key',
    fingerprint: 'SHA256:abc123def456ghi789jkl012mno345pqr678stu901',
    key_version: 1,
    created_at: '2026-07-10T11:00:00Z',
  },
];

const mockOrgDetail = {
  id: ORG_1_ID,
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
            default_organization_id: ORG_1_ID,
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
    if (url.includes(`/api/v1/resources/${RESOURCE_1_ID}`)) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockResources[0] }),
      });
    }
    if (url.includes(`/api/v1/resources/${RESOURCE_2_ID}`)) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockResources[1] }),
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
    if (url.includes(`/api/v1/backup-plans/${PLAN_1_ID}`)) {
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
    if (url.includes(`/api/v1/backup-runs/${RUN_1_ID}`)) {
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
    if (url.includes(`/api/v1/backup-artifacts/${ARTIFACT_1_ID}`)) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockArtifacts[0] }),
      });
    }
    if (url.includes(`/api/v1/backup-artifacts/${ARTIFACT_2_ID}`)) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockArtifacts[1] }),
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
    if (url.includes(`/api/v1/storage-targets/${TARGET_1_ID}`)) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockStorageTargets[0] }),
      });
    }
    if (url.includes(`/api/v1/storage-targets/${TARGET_2_ID}`)) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: mockStorageTargets[1] }),
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
    if (url.includes(`/api/v1/organizations/${ORG_1_ID}`)) {
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
            default_organization_id: ORG_1_ID,
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

  test('4. Resources list and detail navigation with canonical UUID and A.2 types', async ({
    page,
  }) => {
    await setupApiMocks(page);
    await page.goto('/resources');

    await expect(page.getByRole('heading', { name: 'Protected Resources' })).toBeVisible();
    const resourceLink = page.getByRole('link', { name: 'Primary Database Server', exact: true });
    await expect(resourceLink).toBeVisible();

    // Click resource link
    await resourceLink.click();
    await expect(page).toHaveURL(`/resources/${RESOURCE_1_ID}`);
    await expect(page.getByRole('heading', { name: 'Primary Database Server' })).toBeVisible();
    await expect(page.getByText('10.0.0.15:22')).toBeVisible();
    await expect(page.getByText('Ubuntu (SSH)')).toBeVisible();
  });

  test('5. Backup Plans list and detail navigation with human schedule and direct_stream engine', async ({
    page,
  }) => {
    await setupApiMocks(page);
    await page.goto('/plans');

    await expect(page.getByRole('heading', { name: 'Backup Plans' })).toBeVisible();
    const planLink = page.getByRole('link', { name: 'Nightly Database Backup', exact: true });
    await expect(planLink).toBeVisible();

    await planLink.click();
    await expect(page).toHaveURL(`/plans/${PLAN_1_ID}`);
    await expect(page.getByRole('heading', { name: 'Nightly Database Backup' })).toBeVisible();
    await expect(page.getByText('Daily at 02:00')).toBeVisible();
    await expect(page.getByText('0 2 * * *')).toBeVisible();
    await expect(page.getByText('MySQL Database')).toBeVisible();
    await expect(page.getByText('direct_stream')).toBeVisible();
  });

  test('6. Backup Runs list and detail navigation with canonical UUID', async ({ page }) => {
    await setupApiMocks(page);
    await page.goto('/runs');

    await expect(page.getByRole('heading', { name: 'Backup Run History' })).toBeVisible();
    const runLink = page.getByRole('link', { name: /66666666/ }).first();
    await expect(runLink).toBeVisible();

    await runLink.click();
    await expect(page).toHaveURL(`/runs/${RUN_1_ID}`);
    await expect(page.getByRole('heading', { name: new RegExp(`Run ${RUN_1_ID}`) })).toBeVisible();
  });

  test('7. Backup Artifacts list and detail navigation with verified and unverified statuses', async ({
    page,
  }) => {
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
    await expect(page.getByRole('link', { name: 'production_db.sql.gz', exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'public_html.tar.gz', exact: true })).toBeVisible();

    // Click verified artifact
    await page.getByRole('link', { name: 'production_db.sql.gz', exact: true }).click();
    await expect(page).toHaveURL(`/artifacts/${ARTIFACT_1_ID}`);
    await expect(page.getByRole('heading', { name: 'production_db.sql.gz' })).toBeVisible();
  });

  test('8. Storage Targets list and detail navigation with S3 and S3-Compatible', async ({
    page,
  }) => {
    await setupApiMocks(page);
    await page.goto('/storage');

    await expect(page.getByRole('heading', { name: 'Storage Targets' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Primary S3 Bucket', exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'MinIO Secondary Target', exact: true })).toBeVisible();

    // Click S3 compatible target
    await page.getByRole('link', { name: 'MinIO Secondary Target', exact: true }).click();
    await expect(page).toHaveURL(`/storage/${TARGET_2_ID}`);
    await expect(page.getByRole('heading', { name: 'MinIO Secondary Target' })).toBeVisible();
    await expect(page.getByText('S3 Compatible')).toBeVisible();
  });

  test('9. Dark Mode Toggle adds dark class to document root', async ({ page }) => {
    await setupApiMocks(page);
    await page.goto('/');

    await page.click('button[aria-label="Toggle theme"]');
    await page.click('text=Dark');

    const hasDarkClass = await page.evaluate(() =>
      document.documentElement.classList.contains('dark')
    );
    expect(hasDarkClass).toBe(true);
  });

  test('10. Accessibility Smoke Check with Axe (zero serious/critical violations)', async ({ page }) => {
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
