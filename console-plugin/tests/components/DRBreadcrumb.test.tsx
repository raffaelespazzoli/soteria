import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import DRBreadcrumb from '../../src/components/shared/DRBreadcrumb';

expect.extend(toHaveNoViolations);

jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

describe('DRBreadcrumb', () => {
  it('always renders the Disaster Recovery root link', () => {
    render(<DRBreadcrumb />);
    const link = screen.getByRole('link', { name: /disaster recovery/i });
    expect(link).toHaveAttribute('href', '/disaster-recovery');
  });

  it('renders a navigation element with breadcrumb label', () => {
    render(<DRBreadcrumb />);
    expect(screen.getByRole('navigation', { name: /breadcrumb/i })).toBeInTheDocument();
  });

  describe('with planName', () => {
    it('renders the plan name as active breadcrumb item', () => {
      render(<DRBreadcrumb planName="erp-full-stack" />);
      expect(screen.getByText('erp-full-stack')).toBeInTheDocument();
    });

    it('does not render plan name as a link when it is the active item', () => {
      render(<DRBreadcrumb planName="erp-full-stack" />);
      const links = screen.getAllByRole('link');
      const planLink = links.find((l) => l.textContent === 'erp-full-stack');
      expect(planLink).toBeUndefined();
    });
  });

  describe('with executionName', () => {
    it('renders the execution name as active breadcrumb item', () => {
      render(<DRBreadcrumb executionName="exec-001" />);
      expect(screen.getByText('exec-001')).toBeInTheDocument();
    });
  });

  describe('with planName and executionName', () => {
    it('renders plan name as a clickable link', () => {
      render(
        <DRBreadcrumb planName="erp-full-stack" executionName="exec-001" />,
      );
      const planLink = screen.getByRole('link', { name: 'erp-full-stack' });
      expect(planLink).toHaveAttribute(
        'href',
        '/disaster-recovery/plans/erp-full-stack',
      );
    });

    it('renders execution name as active (non-link) breadcrumb item', () => {
      render(
        <DRBreadcrumb planName="erp-full-stack" executionName="exec-001" />,
      );
      expect(screen.getByText('exec-001')).toBeInTheDocument();
      const links = screen.getAllByRole('link');
      const execLink = links.find((l) => l.textContent === 'exec-001');
      expect(execLink).toBeUndefined();
    });
  });

  it('has no accessibility violations with all segments', async () => {
    const { container } = render(
      <DRBreadcrumb planName="erp-full-stack" executionName="exec-001" />,
    );
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations with plan only', async () => {
    const { container } = render(
      <DRBreadcrumb planName="erp-full-stack" />,
    );
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
