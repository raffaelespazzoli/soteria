import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import { PlanConfiguration } from '../../src/components/DRPlanDetail/PlanConfiguration';
import { DRPlan } from '../../src/models/types';

expect.extend(toHaveNoViolations);

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  useK8sWatchResource: jest.fn(() => [null, false, null]),
}));

const mockPlan: DRPlan = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRPlan',
  metadata: {
    name: 'erp-full-stack',
    uid: '1',
    creationTimestamp: '2026-04-02T10:00:00Z',
    labels: {
      'app.kubernetes.io/part-of': 'erp-system',
      'soteria.io/tier': 'critical',
    },
    annotations: {
      'soteria.io/description': 'ERP full-stack DR plan',
      'kubernetes.io/managed-by': 'soteria',
    },
  },
  spec: {
    maxConcurrentFailovers: 4,
    primarySite: 'dc1-prod',
    secondarySite: 'dc2-prod',
  },
  status: {
    phase: 'SteadyState',
    activeSite: 'dc1-prod',
    replicationHealth: [
      {
        name: 'drgroup-1',
        namespace: 'erp-db',
        health: 'Healthy',
        lastChecked: '2026-04-25T15:00:00Z',
      },
      {
        name: 'drgroup-2',
        namespace: 'erp-apps',
        health: 'Degraded',
        lastChecked: '2026-04-25T14:50:00Z',
      },
    ],
    conditions: [
      {
        type: 'ReplicationHealthy',
        status: 'True',
        reason: 'Healthy',
        message: 'RPO: 12s',
        lastTransitionTime: '2026-04-25T15:00:00Z',
      },
    ],
  },
};

const mockPlanMinimal: DRPlan = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRPlan',
  metadata: { name: 'minimal-plan', uid: '2', creationTimestamp: '' },
  spec: {
    maxConcurrentFailovers: 1,
    primarySite: 'site-a',
    secondarySite: 'site-b',
  },
  status: { phase: 'SteadyState' },
};

describe('PlanConfiguration', () => {
  describe('DescriptionList metadata', () => {
    it('renders plan name', () => {
      render(<PlanConfiguration plan={mockPlan} />);
      expect(screen.getByText('Name')).toBeInTheDocument();
      expect(screen.getByText('erp-full-stack')).toBeInTheDocument();
    });

    it('renders fixed wave label convention', () => {
      render(<PlanConfiguration plan={mockPlan} />);
      expect(screen.getByText('Wave Label')).toBeInTheDocument();
      expect(screen.getByText('soteria.io/wave')).toBeInTheDocument();
      expect(screen.getByText(/\(fixed convention\)/)).toBeInTheDocument();
    });

    it('renders max concurrent failovers', () => {
      render(<PlanConfiguration plan={mockPlan} />);
      expect(screen.getByText('Max Concurrent Failovers')).toBeInTheDocument();
      expect(screen.getByText('4')).toBeInTheDocument();
    });

    it('renders primary and secondary sites', () => {
      render(<PlanConfiguration plan={mockPlan} />);
      expect(screen.getByText('Primary Site')).toBeInTheDocument();
      expect(screen.getByText('dc1-prod')).toBeInTheDocument();
      expect(screen.getByText('Secondary Site')).toBeInTheDocument();
      expect(screen.getByText('dc2-prod')).toBeInTheDocument();
    });

    it('renders creation date', () => {
      render(<PlanConfiguration plan={mockPlan} />);
      expect(screen.getByText('Created')).toBeInTheDocument();
    });
  });

  describe('labels', () => {
    it('renders labels as PatternFly Label components', () => {
      render(<PlanConfiguration plan={mockPlan} />);
      expect(screen.getByText('Labels')).toBeInTheDocument();
      expect(
        screen.getAllByText('app.kubernetes.io/part-of=erp-system').length,
      ).toBeGreaterThanOrEqual(1);
      expect(screen.getByText('soteria.io/tier=critical')).toBeInTheDocument();
    });

    it('does not render labels section when plan has no labels', () => {
      render(<PlanConfiguration plan={mockPlanMinimal} />);
      expect(screen.queryByText('Labels')).not.toBeInTheDocument();
    });
  });

  describe('annotations', () => {
    it('renders external annotations and skips internal kubernetes.io ones', () => {
      render(<PlanConfiguration plan={mockPlan} />);
      expect(screen.getByText('Annotations')).toBeInTheDocument();
      expect(screen.getByText('soteria.io/description')).toBeInTheDocument();
      expect(screen.getByText('ERP full-stack DR plan')).toBeInTheDocument();
      expect(screen.queryByText('kubernetes.io/managed-by')).not.toBeInTheDocument();
    });

    it('does not render annotations section when plan has no annotations', () => {
      render(<PlanConfiguration plan={mockPlanMinimal} />);
      expect(screen.queryByText('Annotations')).not.toBeInTheDocument();
    });
  });

  describe('ReplicationHealthExpanded', () => {
    it('renders per-VG health table when health data available', () => {
      render(<PlanConfiguration plan={mockPlan} />);
      expect(screen.getByText('Replication Health')).toBeInTheDocument();
      expect(screen.getByText('drgroup-1')).toBeInTheDocument();
      expect(screen.getByText('drgroup-2')).toBeInTheDocument();
    });

    it('shows fallback when no VG health data', () => {
      render(<PlanConfiguration plan={mockPlanMinimal} />);
      expect(
        screen.getByText('Per-volume-group breakdown not available'),
      ).toBeInTheDocument();
    });
  });

  describe('graceful handling', () => {
    it('handles plan with no labels or annotations', () => {
      render(<PlanConfiguration plan={mockPlanMinimal} />);
      expect(screen.getByText('Name')).toBeInTheDocument();
      expect(screen.getByText('minimal-plan')).toBeInTheDocument();
    });

    it('shows fixed wave label convention for minimal plans', () => {
      render(<PlanConfiguration plan={mockPlanMinimal} />);
      expect(screen.getByText('Wave Label')).toBeInTheDocument();
      expect(screen.getByText('soteria.io/wave')).toBeInTheDocument();
      expect(screen.getByText(/\(fixed convention\)/)).toBeInTheDocument();
      expect(screen.queryByText('Label Selector')).not.toBeInTheDocument();
    });
  });

  describe('SiteDiscoverySection integration', () => {
    it('renders SiteDiscoverySection when plan has site discovery data', () => {
      const planWithDiscovery: DRPlan = {
        ...mockPlan,
        status: {
          ...mockPlan.status,
          primarySiteDiscovery: {
            vms: [{ name: 'vm-a', namespace: 'ns1' }],
            discoveredVMCount: 1,
            lastDiscoveryTime: new Date().toISOString(),
          },
          secondarySiteDiscovery: {
            vms: [{ name: 'vm-a', namespace: 'ns1' }],
            discoveredVMCount: 1,
            lastDiscoveryTime: new Date().toISOString(),
          },
        },
      };
      render(<PlanConfiguration plan={planWithDiscovery} />);
      expect(screen.getByText('Site Discovery')).toBeInTheDocument();
    });

    it('does not render SiteDiscoverySection when plan has no primarySite/secondarySite', () => {
      const planNoSites: DRPlan = {
        ...mockPlanMinimal,
        spec: { ...mockPlanMinimal.spec, primarySite: '', secondarySite: '' },
      };
      render(<PlanConfiguration plan={planNoSites} />);
      expect(screen.queryByText('Site Discovery')).not.toBeInTheDocument();
    });
  });

  describe('accessibility', () => {
    it('has no accessibility violations with full data', async () => {
      const { container } = render(<PlanConfiguration plan={mockPlan} />);
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });

    it('has no accessibility violations with minimal data', async () => {
      const { container } = render(<PlanConfiguration plan={mockPlanMinimal} />);
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });
  });
});
