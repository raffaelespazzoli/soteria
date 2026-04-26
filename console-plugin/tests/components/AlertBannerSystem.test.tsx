import { render, screen, fireEvent } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import AlertBannerSystem from '../../src/components/DRDashboard/AlertBannerSystem';
import { DRPlan } from '../../src/models/types';

expect.extend(toHaveNoViolations);

function makePlan(
  name: string,
  healthStatus: 'True' | 'False' | 'Unknown',
  reason?: string,
  message?: string,
): DRPlan {
  return {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRPlan',
    metadata: { name, uid: name, creationTimestamp: '' },
    spec: {
      waveLabel: 'wave',
      maxConcurrentFailovers: 1,
      primarySite: 'site-a',
      secondarySite: 'site-b',
    },
    status: {
      phase: 'SteadyState',
      conditions: [
        {
          type: 'ReplicationHealthy',
          status: healthStatus,
          reason,
          message,
        },
      ],
    },
  };
}

const healthyPlan = makePlan('plan-healthy', 'True', 'Healthy', 'RPO: 12s');
const errorPlan1 = makePlan('plan-broken-1', 'False', 'Error', 'Replication broken');
const errorPlan2 = makePlan('plan-broken-2', 'False', 'Error', 'Storage unreachable');
const degradedPlan1 = makePlan('plan-degraded-1', 'False', 'Degraded', 'RPO: 120s');
const degradedPlan2 = makePlan('plan-degraded-2', 'False', 'Degraded', 'RPO: 90s');
const unknownPlan = makePlan('plan-unknown', 'Unknown');

describe('AlertBannerSystem', () => {
  const mockOnFilterByHealth = jest.fn();

  beforeEach(() => {
    mockOnFilterByHealth.mockClear();
  });

  // AC1: Danger banner for broken replication
  describe('danger banner (broken replication)', () => {
    it('renders danger alert when Error plans exist', () => {
      render(
        <AlertBannerSystem plans={[errorPlan1, healthyPlan]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      const alert = screen.getByText(/1 DR Plan running UNPROTECTED/);
      expect(alert).toBeInTheDocument();
    });

    it('shows correct count for multiple Error plans', () => {
      render(
        <AlertBannerSystem
          plans={[errorPlan1, errorPlan2, healthyPlan]}
          onFilterByHealth={mockOnFilterByHealth}
        />,
      );
      expect(screen.getByText(/2 DR Plans running UNPROTECTED/)).toBeInTheDocument();
    });

    it('uses singular grammar for 1 Error plan', () => {
      render(
        <AlertBannerSystem plans={[errorPlan1]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      expect(screen.getByText('1 DR Plan running UNPROTECTED — replication broken')).toBeInTheDocument();
    });

    it('uses plural grammar for multiple Error plans', () => {
      render(
        <AlertBannerSystem
          plans={[errorPlan1, errorPlan2]}
          onFilterByHealth={mockOnFilterByHealth}
        />,
      );
      expect(screen.getByText('2 DR Plans running UNPROTECTED — replication broken')).toBeInTheDocument();
    });
  });

  // AC2: Warning banner for degraded replication
  describe('warning banner (degraded replication)', () => {
    it('renders warning alert when Degraded plans exist', () => {
      render(
        <AlertBannerSystem plans={[degradedPlan1, healthyPlan]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      expect(screen.getByText(/1 plan with degraded replication/)).toBeInTheDocument();
    });

    it('shows correct count for multiple Degraded plans', () => {
      render(
        <AlertBannerSystem
          plans={[degradedPlan1, degradedPlan2]}
          onFilterByHealth={mockOnFilterByHealth}
        />,
      );
      expect(screen.getByText(/2 plans with degraded replication/)).toBeInTheDocument();
    });

    it('uses singular grammar for 1 Degraded plan', () => {
      render(
        <AlertBannerSystem plans={[degradedPlan1]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      expect(screen.getByText('1 plan with degraded replication')).toBeInTheDocument();
    });

    it('uses plural grammar for multiple Degraded plans', () => {
      render(
        <AlertBannerSystem
          plans={[degradedPlan1, degradedPlan2]}
          onFilterByHealth={mockOnFilterByHealth}
        />,
      );
      expect(screen.getByText('2 plans with degraded replication')).toBeInTheDocument();
    });
  });

  // AC3: No banners when healthy
  describe('no banners when healthy', () => {
    it('renders nothing when all plans are Healthy', () => {
      const { container } = render(
        <AlertBannerSystem plans={[healthyPlan]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      expect(container.querySelector('.pf-v6-c-alert')).toBeNull();
    });

    it('renders nothing when all plans are Unknown', () => {
      const { container } = render(
        <AlertBannerSystem plans={[unknownPlan]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      expect(container.querySelector('.pf-v6-c-alert')).toBeNull();
    });

    it('renders nothing when plans array is empty', () => {
      const { container } = render(
        <AlertBannerSystem plans={[]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      expect(container.querySelector('.pf-v6-c-alert')).toBeNull();
    });

    it('renders nothing with only Healthy and Unknown plans', () => {
      const { container } = render(
        <AlertBannerSystem
          plans={[healthyPlan, unknownPlan]}
          onFilterByHealth={mockOnFilterByHealth}
        />,
      );
      expect(container.querySelector('.pf-v6-c-alert')).toBeNull();
    });
  });

  // AC4: Automatic banner resolution
  describe('automatic banner resolution', () => {
    it('danger banner disappears when Error condition resolves', () => {
      const { rerender } = render(
        <AlertBannerSystem plans={[errorPlan1]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      expect(screen.getByText(/UNPROTECTED/)).toBeInTheDocument();

      rerender(
        <AlertBannerSystem plans={[healthyPlan]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      expect(screen.queryByText(/UNPROTECTED/)).toBeNull();
    });

    it('warning banner disappears when Degraded condition resolves', () => {
      const { rerender } = render(
        <AlertBannerSystem plans={[degradedPlan1]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      expect(screen.getByText(/degraded replication/)).toBeInTheDocument();

      rerender(
        <AlertBannerSystem plans={[healthyPlan]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      expect(screen.queryByText(/degraded replication/)).toBeNull();
    });
  });

  // Both banners simultaneously, danger above warning
  describe('both banners render simultaneously', () => {
    it('renders both danger and warning banners with mixed plans', () => {
      render(
        <AlertBannerSystem
          plans={[errorPlan1, degradedPlan1, healthyPlan]}
          onFilterByHealth={mockOnFilterByHealth}
        />,
      );
      expect(screen.getByText(/UNPROTECTED/)).toBeInTheDocument();
      expect(screen.getByText(/degraded replication/)).toBeInTheDocument();
    });

    it('danger banner appears above warning banner', () => {
      const { container } = render(
        <AlertBannerSystem
          plans={[errorPlan1, degradedPlan1]}
          onFilterByHealth={mockOnFilterByHealth}
        />,
      );
      const alerts = container.querySelectorAll('.pf-v6-c-alert');
      expect(alerts.length).toBe(2);
      expect(alerts[0]).toHaveClass('pf-m-danger');
      expect(alerts[1]).toHaveClass('pf-m-warning');
    });
  });

  // AC5: Banner action link filters table
  describe('action links', () => {
    it('danger banner action link calls onFilterByHealth with Error', () => {
      render(
        <AlertBannerSystem plans={[errorPlan1]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      const link = screen.getByText('View affected plans');
      fireEvent.click(link);
      expect(mockOnFilterByHealth).toHaveBeenCalledWith('Error');
    });

    it('warning banner action link calls onFilterByHealth with Degraded', () => {
      render(
        <AlertBannerSystem plans={[degradedPlan1]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      const link = screen.getByText('View affected plans');
      fireEvent.click(link);
      expect(mockOnFilterByHealth).toHaveBeenCalledWith('Degraded');
    });

    it('both banners have separate action links', () => {
      render(
        <AlertBannerSystem
          plans={[errorPlan1, degradedPlan1]}
          onFilterByHealth={mockOnFilterByHealth}
        />,
      );
      const links = screen.getAllByText('View affected plans');
      expect(links).toHaveLength(2);

      fireEvent.click(links[0]);
      expect(mockOnFilterByHealth).toHaveBeenCalledWith('Error');

      fireEvent.click(links[1]);
      expect(mockOnFilterByHealth).toHaveBeenCalledWith('Degraded');
    });
  });

  // AC6: Accessibility
  describe('accessibility', () => {
    it('danger-only scenario has no accessibility violations', async () => {
      const { container } = render(
        <AlertBannerSystem plans={[errorPlan1, errorPlan2]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });

    it('warning-only scenario has no accessibility violations', async () => {
      const { container } = render(
        <AlertBannerSystem plans={[degradedPlan1]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });

    it('both-visible scenario has no accessibility violations', async () => {
      const { container } = render(
        <AlertBannerSystem
          plans={[errorPlan1, degradedPlan1, healthyPlan]}
          onFilterByHealth={mockOnFilterByHealth}
        />,
      );
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });

    it('empty scenario has no accessibility violations', async () => {
      const { container } = render(
        <AlertBannerSystem plans={[healthyPlan]} onFilterByHealth={mockOnFilterByHealth} />,
      );
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });
  });
});
