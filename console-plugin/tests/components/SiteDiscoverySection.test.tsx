import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import { SiteDiscoverySection } from '../../src/components/DRPlanDetail/SiteDiscoverySection';
import { DRPlan, DiscoveredVM } from '../../src/models/types';

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
});
