/**
 * In-Memory Ephemeral Secret Store.
 * 
 * Prevents credential secrets (SSH keys, passwords, S3 secret keys, API tokens)
 * from ever being captured in TanStack Query's MutationCache.
 * 
 * Flow:
 * 1. Form generates an opaque invocationId (e.g. crypto.randomUUID()).
 * 2. Form stores the secret payload here via storeEphemeralSecret(invocationId, payload).
 * 3. Form passes only { invocationId, metadata: { name, type } } as TanStack mutation variables.
 * 4. mutationFn consumes the secret payload via consumeEphemeralSecret(invocationId),
 *    which retrieves and immediately wipes the secret reference in memory.
 * 5. In finally block, clearEphemeralSecret(invocationId) ensures zero retention.
 */

const ephemeralSecrets = new Map<string, unknown>();

export function storeEphemeralSecret(invocationId: string, secretPayload: unknown): void {
  if (!invocationId) return;
  ephemeralSecrets.set(invocationId, secretPayload);
}

export function consumeEphemeralSecret<T = unknown>(invocationId: string): T | undefined {
  if (!invocationId) return undefined;
  const secret = ephemeralSecrets.get(invocationId) as T | undefined;
  ephemeralSecrets.delete(invocationId);
  return secret;
}

export function clearEphemeralSecret(invocationId: string): void {
  if (!invocationId) return;
  ephemeralSecrets.delete(invocationId);
}

export function clearAllEphemeralSecrets(): void {
  ephemeralSecrets.clear();
}
