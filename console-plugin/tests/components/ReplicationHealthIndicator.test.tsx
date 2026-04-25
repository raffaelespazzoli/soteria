import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import ReplicationHealthIndicator from '../../src/components/shared/ReplicationHealthIndicator';
import { ReplicationHealth } from '../../src/utils/drPlanUtils';

expect.extend(toHaveNoViolations);

describe('ReplicationHealthIndicator', () => {
  const cases: { health: ReplicationHealth; expectedLabel: string; expectedAria: string }[] = [
    {
      health: { status: 'Healthy', rpoSeconds: 12 },
      expectedLabel: 'Healthy',
      expectedAria: 'Replication healthy, RPO 12s',
    },
    {
      health: { status: 'Degraded', rpoSeconds: 45 },
      expectedLabel: 'Degraded',
      expectedAria: 'Replication degraded, RPO 45s',
    },
    {
      health: { status: 'Error', rpoSeconds: null },
      expectedLabel: 'Error',
      expectedAria: 'Replication error',
    },
    {
      health: { status: 'Unknown', rpoSeconds: null },
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

  it('renders RPO text when rpoSeconds is provided', () => {
    render(<ReplicationHealthIndicator health={{ status: 'Healthy', rpoSeconds: 120 }} />);
    expect(screen.getByText('RPO 2m')).toBeInTheDocument();
  });

  it('does not render RPO text when rpoSeconds is null', () => {
    render(<ReplicationHealthIndicator health={{ status: 'Unknown', rpoSeconds: null }} />);
    expect(screen.queryByText(/RPO/)).not.toBeInTheDocument();
  });

  it.each(cases)(
    'has no accessibility violations for $health.status',
    async ({ health }) => {
      const { container } = render(<ReplicationHealthIndicator health={health} />);
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    },
  );
});
