import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import PhaseBadge from '../../src/components/shared/PhaseBadge';
import { EffectivePhase } from '../../src/utils/drPlanUtils';

expect.extend(toHaveNoViolations);

const ALL_PHASES: { phase: EffectivePhase; label: string; isTransient: boolean }[] = [
  { phase: 'SteadyState', label: 'Steady State', isTransient: false },
  { phase: 'FailingOver', label: 'Failing Over', isTransient: true },
  { phase: 'FailedOver', label: 'Failed Over', isTransient: false },
  { phase: 'Reprotecting', label: 'Reprotecting', isTransient: true },
  { phase: 'DRedSteadyState', label: 'DR Steady State', isTransient: false },
  { phase: 'FailingBack', label: 'Failing Back', isTransient: true },
  { phase: 'FailedBack', label: 'Failed Back', isTransient: false },
  { phase: 'Restoring', label: 'Restoring', isTransient: true },
];

describe('PhaseBadge', () => {
  it.each(ALL_PHASES)(
    'renders label "$label" for phase $phase',
    ({ phase, label }) => {
      render(<PhaseBadge phase={phase} />);
      expect(screen.getByText(label)).toBeInTheDocument();
    },
  );

  it.each(ALL_PHASES.filter((p) => p.isTransient))(
    'includes screen-reader "(in progress)" text for transient phase $phase',
    ({ phase }) => {
      const { container } = render(<PhaseBadge phase={phase} />);
      expect(container.textContent).toContain('(in progress)');
    },
  );

  it.each(ALL_PHASES.filter((p) => !p.isTransient))(
    'does not include "(in progress)" for rest phase $phase',
    ({ phase }) => {
      const { container } = render(<PhaseBadge phase={phase} />);
      expect(container.textContent).not.toContain('(in progress)');
    },
  );

  it.each(ALL_PHASES)(
    'has no accessibility violations for phase $phase',
    async ({ phase }) => {
      const { container } = render(<PhaseBadge phase={phase} />);
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    },
  );
});
