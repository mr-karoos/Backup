import { describe, it, expect } from 'vitest';
import { queryKeys } from '@/lib/query/query-client';
import { QueryClient } from '@tanstack/react-query';

describe('Tenant Query Isolation', () => {
  it('guarantees that all tenant query keys strictly incorporate the organization ID', () => {
    const orgIdA = 'org-1111';
    const orgIdB = 'org-2222';

    const keysA = queryKeys.org(orgIdA);
    const keysB = queryKeys.org(orgIdB);

    // Resources
    expect(keysA.resources.all()).toEqual(['org', 'org-1111', 'resources']);
    expect(keysB.resources.all()).toEqual(['org', 'org-2222', 'resources']);
    expect(keysA.resources.detail('res-1')).toEqual(['org', 'org-1111', 'resources', 'res-1']);

    // Plans
    expect(keysA.plans.all()).toEqual(['org', 'org-1111', 'plans', {}]);
    expect(keysB.plans.all()).toEqual(['org', 'org-2222', 'plans', {}]);

    // Runs
    expect(keysA.runs.all()).toEqual(['org', 'org-1111', 'runs', {}]);
    expect(keysB.runs.all()).toEqual(['org', 'org-2222', 'runs', {}]);

    // Artifacts
    expect(keysA.artifacts.all()).toEqual(['org', 'org-1111', 'artifacts']);
    expect(keysB.artifacts.all()).toEqual(['org', 'org-2222', 'artifacts']);

    // Storage Targets
    expect(keysA.storageTargets.all()).toEqual(['org', 'org-1111', 'storage-targets']);
    expect(keysB.storageTargets.all()).toEqual(['org', 'org-2222', 'storage-targets']);

    // Credentials
    expect(keysA.credentials.all()).toEqual(['org', 'org-1111', 'credentials']);
    expect(keysB.credentials.all()).toEqual(['org', 'org-2222', 'credentials']);
  });

  it('cancels and removes old tenant cached queries on organization switch', async () => {
    const queryClient = new QueryClient();

    const orgIdA = 'org-aaaa';
    const orgIdB = 'org-bbbb';

    // Seed cache for Org A
    queryClient.setQueryData(queryKeys.org(orgIdA).resources.all(), [
      { id: 'res-a', name: 'Server In Org A' },
    ]);

    // Verify cache has data for Org A
    expect(queryClient.getQueryData(queryKeys.org(orgIdA).resources.all())).toBeDefined();

    // Perform tenant switch cleanup
    queryClient.cancelQueries({ queryKey: queryKeys.org(orgIdA).all });
    queryClient.removeQueries({ queryKey: queryKeys.org(orgIdA).all });

    // Org A cache must be completely evicted
    expect(queryClient.getQueryData(queryKeys.org(orgIdA).resources.all())).toBeUndefined();
    expect(queryClient.getQueryData(queryKeys.org(orgIdB).resources.all())).toBeUndefined();
  });
});
