import { render, screen, fireEvent } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import { SiteDiscoverySection } from '../../src/components/DRPlanDetail/SiteDiscoverySection';
import { DRPlan, DiscoveredVM, DiscoveredDisk } from '../../src/models/types';

expect.extend(toHaveNoViolations);

function makePlanWithSiteDiscovery(
  opts: {
    primaryVMs?: DiscoveredVM[];
    secondaryVMs?: DiscoveredVM[];
    primaryLastDiscovery?: string;
    secondaryLastDiscovery?: string;
  } = {},
): DRPlan {
  return {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRPlan',
    metadata: { name: 'test-plan', uid: '1', creationTimestamp: '' },
    spec: {
      maxConcurrentFailovers: 1,
      primarySite: 'dc-west',
      secondarySite: 'dc-east',
      volumeReplicationDriver: { type: 'noop' },
    },
    status: {
      phase: 'SteadyState',
      primarySiteDiscovery: opts.primaryVMs
        ? {
            vms: opts.primaryVMs,
            discoveredVMCount: opts.primaryVMs.length,
            lastDiscoveryTime: opts.primaryLastDiscovery ?? new Date().toISOString(),
          }
        : undefined,
      secondarySiteDiscovery: opts.secondaryVMs
        ? {
            vms: opts.secondaryVMs,
            discoveredVMCount: opts.secondaryVMs.length,
            lastDiscoveryTime: opts.secondaryLastDiscovery ?? new Date().toISOString(),
          }
        : undefined,
    },
  };
}

describe('SiteDiscoverySection', () => {
  it('renders both sites with matching VMs in default style (no warning icons)', () => {
    const vms: DiscoveredVM[] = [
      { name: 'vm-a', namespace: 'ns1' },
      { name: 'vm-b', namespace: 'ns2' },
    ];
    const plan = makePlanWithSiteDiscovery({ primaryVMs: vms, secondaryVMs: vms });
    render(<SiteDiscoverySection plan={plan} />);

    expect(screen.getByText('dc-west')).toBeInTheDocument();
    expect(screen.getByText('dc-east')).toBeInTheDocument();
    expect(screen.getAllByText(/2 VMs discovered/).length).toBe(2);
    expect(screen.queryByText(/VM present on/)).not.toBeInTheDocument();
  });

  it('highlights mismatched VMs with warning icons', () => {
    const primaryVMs: DiscoveredVM[] = [
      { name: 'vm-a', namespace: 'ns1' },
      { name: 'vm-extra', namespace: 'ns1' },
    ];
    const secondaryVMs: DiscoveredVM[] = [{ name: 'vm-a', namespace: 'ns1' }];
    const plan = makePlanWithSiteDiscovery({ primaryVMs, secondaryVMs });
    render(<SiteDiscoverySection plan={plan} />);

    expect(screen.getByText('VM present on primary site only')).toBeInTheDocument();
  });

  it('shows informational text when one site is nil', () => {
    const plan = makePlanWithSiteDiscovery({
      primaryVMs: [{ name: 'vm-a', namespace: 'ns1' }],
    });
    render(<SiteDiscoverySection plan={plan} />);

    expect(screen.getByText('Waiting for dc-east to report discovery data')).toBeInTheDocument();
  });

  it('shows "not yet available" message when both sites are nil', () => {
    const plan = makePlanWithSiteDiscovery({});
    render(<SiteDiscoverySection plan={plan} />);

    expect(
      screen.getByText(/Site discovery not yet available/),
    ).toBeInTheDocument();
  });

  it('shows stale warning when lastDiscoveryTime is older than 5 minutes', () => {
    const staleTime = new Date(Date.now() - 10 * 60 * 1000).toISOString();
    const plan = makePlanWithSiteDiscovery({
      primaryVMs: [{ name: 'vm-a', namespace: 'ns1' }],
      secondaryVMs: [{ name: 'vm-a', namespace: 'ns1' }],
      primaryLastDiscovery: staleTime,
    });
    render(<SiteDiscoverySection plan={plan} />);

    expect(screen.getByText(/Discovery data from dc-west is stale/)).toBeInTheDocument();
  });

  it('does not show stale warning when lastDiscoveryTime is fresh', () => {
    const freshTime = new Date().toISOString();
    const plan = makePlanWithSiteDiscovery({
      primaryVMs: [{ name: 'vm-a', namespace: 'ns1' }],
      secondaryVMs: [{ name: 'vm-a', namespace: 'ns1' }],
      primaryLastDiscovery: freshTime,
      secondaryLastDiscovery: freshTime,
    });
    render(<SiteDiscoverySection plan={plan} />);

    expect(screen.queryByText(/is stale/)).not.toBeInTheDocument();
  });

  it('has no accessibility violations with matching VMs', async () => {
    const vms: DiscoveredVM[] = [{ name: 'vm-a', namespace: 'ns1' }];
    const plan = makePlanWithSiteDiscovery({ primaryVMs: vms, secondaryVMs: vms });
    const { container } = render(<SiteDiscoverySection plan={plan} />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations with mismatched VMs', async () => {
    const plan = makePlanWithSiteDiscovery({
      primaryVMs: [{ name: 'vm-a', namespace: 'ns1' }, { name: 'vm-extra', namespace: 'ns1' }],
      secondaryVMs: [{ name: 'vm-a', namespace: 'ns1' }],
    });
    const { container } = render(<SiteDiscoverySection plan={plan} />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations when both nil', async () => {
    const plan = makePlanWithSiteDiscovery({});
    const { container } = render(<SiteDiscoverySection plan={plan} />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations when one site is nil', async () => {
    const plan = makePlanWithSiteDiscovery({
      primaryVMs: [{ name: 'vm-a', namespace: 'ns1' }],
    });
    const { container } = render(<SiteDiscoverySection plan={plan} />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations with stale discovery data', async () => {
    const staleTime = new Date(Date.now() - 10 * 60 * 1000).toISOString();
    const plan = makePlanWithSiteDiscovery({
      primaryVMs: [{ name: 'vm-a', namespace: 'ns1' }],
      secondaryVMs: [{ name: 'vm-a', namespace: 'ns1' }],
      primaryLastDiscovery: staleTime,
    });
    const { container } = render(<SiteDiscoverySection plan={plan} />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  describe('per-VM disk expansion', () => {
    const primaryDisks: DiscoveredDisk[] = [
      { name: 'disk-root', pvcName: 'pvc-root-a', storageClass: 'ceph-rbd' },
      { name: 'disk-data', pvcName: 'pvc-data-a', storageClass: 'ceph-rbd' },
    ];
    const secondaryDisks: DiscoveredDisk[] = [
      { name: 'disk-root', pvcName: 'pvc-root-b', storageClass: 'ceph-rbd' },
      { name: 'disk-data', pvcName: 'pvc-data-b', storageClass: 'netapp-gold' },
    ];

    it('VM row is expandable when disks are present', () => {
      const plan = makePlanWithSiteDiscovery({
        primaryVMs: [{ name: 'vm-a', namespace: 'ns1', disks: primaryDisks }],
        secondaryVMs: [{ name: 'vm-a', namespace: 'ns1', disks: secondaryDisks }],
      });
      render(<SiteDiscoverySection plan={plan} />);
      const expandBtn = screen.getAllByLabelText('Show disks for vm-a')[0];
      expect(expandBtn).toBeInTheDocument();
      expect(expandBtn).toHaveAttribute('aria-expanded', 'false');
    });

    it('expanded row shows disk table with Disk Name, PVC Name, Storage Class', () => {
      const plan = makePlanWithSiteDiscovery({
        primaryVMs: [{ name: 'vm-a', namespace: 'ns1', disks: primaryDisks }],
        secondaryVMs: [{ name: 'vm-a', namespace: 'ns1', disks: secondaryDisks }],
      });
      render(<SiteDiscoverySection plan={plan} />);
      fireEvent.click(screen.getAllByLabelText('Show disks for vm-a')[0]);

      expect(screen.getByText('disk-root')).toBeInTheDocument();
      expect(screen.getByText('pvc-root-a')).toBeInTheDocument();
      expect(screen.getAllByText('ceph-rbd').length).toBeGreaterThanOrEqual(1);
    });

    it('highlights disk with different storage class on same disk name', () => {
      const plan = makePlanWithSiteDiscovery({
        primaryVMs: [{ name: 'vm-a', namespace: 'ns1', disks: primaryDisks }],
        secondaryVMs: [{ name: 'vm-a', namespace: 'ns1', disks: secondaryDisks }],
      });
      render(<SiteDiscoverySection plan={plan} />);
      fireEvent.click(screen.getAllByLabelText('Show disks for vm-a')[0]);

      expect(screen.getByText('Storage class differs from partner site')).toBeInTheDocument();
    });

    it('highlights missing disk on one site', () => {
      const plan = makePlanWithSiteDiscovery({
        primaryVMs: [{ name: 'vm-a', namespace: 'ns1', disks: [
          { name: 'disk-root', pvcName: 'pvc-root', storageClass: 'ceph-rbd' },
          { name: 'disk-extra', pvcName: 'pvc-extra', storageClass: 'ceph-rbd' },
        ] }],
        secondaryVMs: [{ name: 'vm-a', namespace: 'ns1', disks: [
          { name: 'disk-root', pvcName: 'pvc-root-b', storageClass: 'ceph-rbd' },
        ] }],
      });
      render(<SiteDiscoverySection plan={plan} />);
      fireEvent.click(screen.getAllByLabelText('Show disks for vm-a')[0]);

      expect(screen.getByText('Disk missing on partner site')).toBeInTheDocument();
    });

    it('stateless VM (no disks) shows "No PVC disks" inline without expand button', () => {
      const plan = makePlanWithSiteDiscovery({
        primaryVMs: [{ name: 'vm-stateless', namespace: 'ns1' }],
        secondaryVMs: [{ name: 'vm-stateless', namespace: 'ns1' }],
      });
      render(<SiteDiscoverySection plan={plan} />);
      expect(screen.queryByLabelText('Show disks for vm-stateless')).not.toBeInTheDocument();
      expect(screen.getAllByText('No PVC disks').length).toBeGreaterThanOrEqual(1);
    });

    it('has no accessibility violations with expanded disk view', async () => {
      const plan = makePlanWithSiteDiscovery({
        primaryVMs: [{ name: 'vm-a', namespace: 'ns1', disks: primaryDisks }],
        secondaryVMs: [{ name: 'vm-a', namespace: 'ns1', disks: secondaryDisks }],
      });
      const { container } = render(<SiteDiscoverySection plan={plan} />);
      fireEvent.click(screen.getAllByLabelText('Show disks for vm-a')[0]);

      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });

    it('has no accessibility violations with collapsed disk view', async () => {
      const plan = makePlanWithSiteDiscovery({
        primaryVMs: [{ name: 'vm-a', namespace: 'ns1', disks: primaryDisks }],
        secondaryVMs: [{ name: 'vm-a', namespace: 'ns1', disks: secondaryDisks }],
      });
      const { container } = render(<SiteDiscoverySection plan={plan} />);
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });
  });
});
