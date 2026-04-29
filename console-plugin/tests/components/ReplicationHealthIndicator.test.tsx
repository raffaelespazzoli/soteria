import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import ReplicationHealthIndicator from '../../src/components/shared/ReplicationHealthIndicator';
import { ReplicationHealth } from '../../src/utils/drPlanUtils';

expect.extend(toHaveNoViolations);

describe('ReplicationHealthIndicator', () => {
  const cases: { health: ReplicationHealth; expectedLabel: string; expectedAria: string }[] = [
    {
      health: { status: 'Healthy' },
      expectedLabel: 'Healthy',
      expectedAria: 'Replication healthy',
    },
    {
      health: { status: 'Degraded' },
      expectedLabel: 'Degraded',
      expectedAria: 'Replication degraded',
    },
    {
      health: { status: 'Error' },
      expectedLabel: 'Error',
      expectedAria: 'Replication error',
    },
    {
      health: { status: 'Unknown' },
      expectedLabel: 'Unknown',
      expectedAria: 'Replication unknown',
    },
  ];

  it.each(cases)(
    'renders $health.status state with correct label and aria',
    ({ health, expectedLabel, expectedAria }) => {
      render(<ReplicationHealthIndicator health={health} />);
      expect(screen.getByText(expectedLabel)).toBeInTheDocument();
      expect(screen.getByRole('status')).toHaveAttribute('aria-label', expectedAria);
    },
  );

  it.each(cases)(
    'has no accessibility violations for $health.status',
    async ({ health }) => {
      const { container } = render(<ReplicationHealthIndicator health={health} />);
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    },
  );
});
