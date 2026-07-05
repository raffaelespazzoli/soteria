import {
  getRetryRejectedMessage,
  RETRY_GROUPS_ANNOTATION,
  RETRY_ALL_FAILED,
} from '../../src/hooks/useRetryDRGroup';
import { Condition } from '../../src/models/types';

jest.mock('../../src/providers', () => ({
  useProvider: () => ({
    patchResource: jest.fn(),
    useWatchResource: jest.fn(() => [[], true, null]),
    createResource: jest.fn(),
    DocumentTitle: () => null,
  }),
}));

describe('getRetryRejectedMessage', () => {
  it('returns null when no conditions', () => {
    expect(getRetryRejectedMessage(undefined)).toBeNull();
    expect(getRetryRejectedMessage([])).toBeNull();
  });

  it('returns message when RetryRejected condition is True', () => {
    const conditions: Condition[] = [
      { type: 'RetryRejected', status: 'True', message: 'VM unreachable' },
    ];
    expect(getRetryRejectedMessage(conditions)).toBe('VM unreachable');
  });

  it('returns null when RetryRejected condition is False', () => {
    const conditions: Condition[] = [
      { type: 'RetryRejected', status: 'False', message: 'old rejection' },
    ];
    expect(getRetryRejectedMessage(conditions)).toBeNull();
  });

  it('returns null when RetryRejected has no message', () => {
    const conditions: Condition[] = [
      { type: 'RetryRejected', status: 'True' },
    ];
    expect(getRetryRejectedMessage(conditions)).toBeNull();
  });
});

describe('useRetryDRGroup constants', () => {
  it('exposes the retry groups annotation key', () => {
    expect(RETRY_GROUPS_ANNOTATION).toBe('soteria.io/retry-groups');
  });

  it('exposes the all-failed sentinel', () => {
    expect(RETRY_ALL_FAILED).toBe('all-failed');
  });
});
