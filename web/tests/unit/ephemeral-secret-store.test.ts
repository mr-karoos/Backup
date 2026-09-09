import { describe, it, expect, beforeEach } from 'vitest';
import {
  storeEphemeralSecret,
  consumeEphemeralSecret,
  clearEphemeralSecret,
  clearAllEphemeralSecrets,
} from '@/lib/security/ephemeral-secret-store';

describe('EphemeralSecretStore', () => {
  beforeEach(() => {
    clearAllEphemeralSecrets();
  });

  it('stores and consumes secret once, ensuring immediate deletion from memory', () => {
    const invocationId = 'test-inv-1';
    const secretPayload = {
      secret: 'super-secret-ssh-key',
      passphrase: 'key-passphrase',
    };

    storeEphemeralSecret(invocationId, secretPayload);

    // First consume should return the payload
    const retrieved = consumeEphemeralSecret<typeof secretPayload>(invocationId);
    expect(retrieved).toEqual(secretPayload);

    // Second consume MUST return undefined (payload consumed & purged)
    const secondRetrieve = consumeEphemeralSecret(invocationId);
    expect(secondRetrieve).toBeUndefined();
  });

  it('clearEphemeralSecret removes secret from memory before consumption', () => {
    const invocationId = 'test-inv-2';
    storeEphemeralSecret(invocationId, { access_key_id: 'AKIA', secret_access_key: 'SECRET' });

    clearEphemeralSecret(invocationId);

    const retrieved = consumeEphemeralSecret(invocationId);
    expect(retrieved).toBeUndefined();
  });

  it('clearAllEphemeralSecrets wipes all stored secrets', () => {
    storeEphemeralSecret('inv-a', { secret: 'a' });
    storeEphemeralSecret('inv-b', { secret: 'b' });

    clearAllEphemeralSecrets();

    expect(consumeEphemeralSecret('inv-a')).toBeUndefined();
    expect(consumeEphemeralSecret('inv-b')).toBeUndefined();
  });

  it('handles empty or missing invocationId safely', () => {
    storeEphemeralSecret('', { secret: 'invalid' });
    expect(consumeEphemeralSecret('')).toBeUndefined();

    // Calling clear on empty or non-existent invocationId should not throw
    expect(() => clearEphemeralSecret('')).not.toThrow();
    expect(() => clearEphemeralSecret('non-existent-id')).not.toThrow();
  });
});
