import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { k8sPatch } from '@openshift-console/dynamic-plugin-sdk';
import { useRetryDRGroup, RETRY_GROUPS_ANNOTATION, RETRY_ALL_FAILED } from '../../src/hooks/useRetryDRGroup';
import { DRExecution } from '../../src/models/types';

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  k8sPatch: jest.fn(),
}));

const mockedK8sPatch = k8sPatch as jest.MockedFunction<typeof k8sPatch>;

const baseExecution: DRExecution = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRExecution',
  metadata: { name: 'erp-failover-001', uid: '1' },
  spec: { planName: 'erp-full-stack', mode: 'disaster' },
  status: {
    result: 'PartiallySucceeded',
    startTime: '2026-04-28T10:00:00Z',
    completionTime: '2026-04-28T10:10:00Z',
    waves: [
      {
        waveIndex: 0,
        startTime: '2026-04-28T10:00:00Z',
        completionTime: '2026-04-28T10:05:00Z',
        groups: [{ name: 'drgroup-1', result: 'Completed', vmNames: ['db-1'] }],
      },
      {
        waveIndex: 1,
        startTime: '2026-04-28T10:05:00Z',
        completionTime: '2026-04-28T10:10:00Z',
        groups: [
          { name: 'drgroup-3', result: 'Failed', vmNames: ['app-1', 'app-2'], error: 'storage timeout' },
        ],
      },
    ],
  },
};

interface HookOutputProps {
  executionName: string;
  execution: DRExecution | null;
}

const HookOutput: React.FC<HookOutputProps> = ({ executionName, execution }) => {
  const { retry, retryAll, isRetrying, retryError, retriedGroup } = useRetryDRGroup(executionName, execution);
  return (
    <div>
      <span data-testid="isRetrying">{String(isRetrying)}</span>
      <span data-testid="retryError">{retryError ?? ''}</span>
      <span data-testid="retriedGroup">{retriedGroup ?? ''}</span>
      <button data-testid="retry-single" onClick={() => retry('drgroup-3')}>Retry Single</button>
      <button data-testid="retry-all" onClick={() => retryAll()}>Retry All</button>
    </div>
  );
};

describe('useRetryDRGroup (hook behavior)', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockedK8sPatch.mockResolvedValue({} as never);
  });

  it('patches annotation with group name for single retry', async () => {
    const user = userEvent.setup();
    render(<HookOutput executionName="erp-failover-001" execution={baseExecution} />);

    await user.click(screen.getByTestId('retry-single'));

    expect(mockedK8sPatch).toHaveBeenCalledWith({
      model: expect.objectContaining({ kind: 'DRExecution', apiGroup: 'soteria.io' }),
      resource: { metadata: { name: 'erp-failover-001' } },
      data: [
        {
          op: 'add',
          path: '/metadata/annotations/soteria.io~1retry-groups',
          value: 'drgroup-3',
        },
      ],
    });
  });

  it('patches annotation with all-failed for retry all', async () => {
    const user = userEvent.setup();
    render(<HookOutput executionName="erp-failover-001" execution={baseExecution} />);

    await user.click(screen.getByTestId('retry-all'));

    expect(mockedK8sPatch).toHaveBeenCalledWith(
      expect.objectContaining({
        data: [
          {
            op: 'add',
            path: '/metadata/annotations/soteria.io~1retry-groups',
            value: RETRY_ALL_FAILED,
          },
        ],
      }),
    );
  });

  it('returns isRetrying true while patch is in flight', async () => {
    let resolvePromise!: () => void;
    mockedK8sPatch.mockReturnValue(
      new Promise<void>((resolve) => {
        resolvePromise = resolve;
      }) as never,
    );

    const user = userEvent.setup();
    render(<HookOutput executionName="erp-failover-001" execution={baseExecution} />);

    expect(screen.getByTestId('isRetrying').textContent).toBe('false');

    await user.click(screen.getByTestId('retry-single'));

    expect(screen.getByTestId('isRetrying').textContent).toBe('true');

    await act(async () => {
      resolvePromise();
    });
  });

  it('sets retryError on patch failure', async () => {
    mockedK8sPatch.mockRejectedValue(new Error('Forbidden'));
    const user = userEvent.setup();
    render(<HookOutput executionName="erp-failover-001" execution={baseExecution} />);

    await user.click(screen.getByTestId('retry-single'));

    expect(screen.getByTestId('retryError').textContent).toBe('Forbidden');
    expect(screen.getByTestId('isRetrying').textContent).toBe('false');
  });

  it('clears retryError on new retry attempt', async () => {
    mockedK8sPatch.mockRejectedValueOnce(new Error('Forbidden'));
    mockedK8sPatch.mockResolvedValueOnce({} as never);

    const user = userEvent.setup();
    render(<HookOutput executionName="erp-failover-001" execution={baseExecution} />);

    await user.click(screen.getByTestId('retry-single'));
    expect(screen.getByTestId('retryError').textContent).toBe('Forbidden');

    await user.click(screen.getByTestId('retry-single'));
    expect(screen.getByTestId('retryError').textContent).toBe('');
  });

  it('detects RetryRejected condition from execution watch', async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <HookOutput executionName="erp-failover-001" execution={baseExecution} />,
    );

    await user.click(screen.getByTestId('retry-single'));

    const executionWithRejection: DRExecution = {
      ...baseExecution,
      status: {
        ...baseExecution.status!,
        conditions: [
          { type: 'RetryRejected', status: 'True', message: 'VM erp-app-1 is unstable' },
        ],
      },
    };

    rerender(<HookOutput executionName="erp-failover-001" execution={executionWithRejection} />);

    expect(screen.getByTestId('retryError').textContent).toBe('VM erp-app-1 is unstable');
    expect(screen.getByTestId('isRetrying').textContent).toBe('false');
  });

  it('tracks retriedGroup for single retry', async () => {
    const user = userEvent.setup();
    render(<HookOutput executionName="erp-failover-001" execution={baseExecution} />);

    await user.click(screen.getByTestId('retry-single'));

    expect(screen.getByTestId('retriedGroup').textContent).toBe('drgroup-3');
  });

  it('tracks retriedGroup as all-failed for retry all', async () => {
    const user = userEvent.setup();
    render(<HookOutput executionName="erp-failover-001" execution={baseExecution} />);

    await user.click(screen.getByTestId('retry-all'));

    expect(screen.getByTestId('retriedGroup').textContent).toBe('all-failed');
  });

  it('surfaces RetryRejected condition on initial render without retry click', () => {
    const executionWithRejection: DRExecution = {
      ...baseExecution,
      status: {
        ...baseExecution.status!,
        conditions: [
          { type: 'RetryRejected', status: 'True', message: 'Persisted rejection on load' },
        ],
      },
    };

    render(<HookOutput executionName="erp-failover-001" execution={executionWithRejection} />);

    expect(screen.getByTestId('retryError').textContent).toBe('Persisted rejection on load');
    expect(screen.getByTestId('isRetrying').textContent).toBe('false');
  });

  it('clears isRetrying when watch shows InProgress group', async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <HookOutput executionName="erp-failover-001" execution={baseExecution} />,
    );

    await user.click(screen.getByTestId('retry-single'));
    expect(screen.getByTestId('isRetrying').textContent).toBe('true');

    const executionWithRetrying: DRExecution = {
      ...baseExecution,
      status: {
        ...baseExecution.status!,
        waves: [
          baseExecution.status!.waves![0],
          {
            waveIndex: 1,
            startTime: '2026-04-28T10:05:00Z',
            groups: [
              { name: 'drgroup-3', result: 'InProgress', vmNames: ['app-1', 'app-2'] },
            ],
          },
        ],
      },
    };

    rerender(<HookOutput executionName="erp-failover-001" execution={executionWithRetrying} />);

    expect(screen.getByTestId('isRetrying').textContent).toBe('false');
  });
});
