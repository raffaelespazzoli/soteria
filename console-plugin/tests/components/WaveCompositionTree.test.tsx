import { render, screen, fireEvent } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import { WaveCompositionTree } from '../../src/components/DRPlanDetail/WaveCompositionTree';
import { DRPlan } from '../../src/models/types';

expect.extend(toHaveNoViolations);

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  useK8sWatchResource: jest.fn(() => [null, false, null]),
}));

const mockPlanWithWaves: DRPlan = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRPlan',
  metadata: {
    name: 'erp-full-stack',
    uid: '1',
    creationTimestamp: '2026-04-02T10:00:00Z',
    labels: { 'app.kubernetes.io/part-of': 'erp-system' },
    annotations: { 'soteria.io/description': 'ERP full-stack DR plan' },
  },
  spec: {
    maxConcurrentFailovers: 4,
    primarySite: 'dc1-prod',
    secondarySite: 'dc2-prod',
    volumeReplicationDriver: 'noop',
  },
  status: {
    phase: 'SteadyState',
    activeSite: 'dc1-prod',
    discoveredVMCount: 12,
    waves: [
      {
        waveKey: '1',
        vms: [
          { name: 'erp-db-1', namespace: 'erp-db' },
          { name: 'erp-db-2', namespace: 'erp-db' },
          { name: 'erp-db-3', namespace: 'erp-db' },
        ],
        groups: [
          {
            name: 'drgroup-1',
            namespace: 'erp-db',
            consistencyLevel: 'namespace',
            vmNames: ['erp-db-1', 'erp-db-2', 'erp-db-3'],
          },
        ],
      },
      {
        waveKey: '2',
        vms: [
          { name: 'erp-app-1', namespace: 'erp-apps' },
          { name: 'erp-app-2', namespace: 'erp-apps' },
          { name: 'erp-app-3', namespace: 'erp-apps' },
          { name: 'erp-app-4', namespace: 'erp-apps' },
          { name: 'erp-app-5', namespace: 'erp-standalone' },
        ],
        groups: [
          {
            name: 'drgroup-2',
            namespace: 'erp-apps',
            consistencyLevel: 'namespace',
            vmNames: ['erp-app-1', 'erp-app-2', 'erp-app-3', 'erp-app-4'],
          },
          {
            name: 'drgroup-3',
            namespace: 'erp-standalone',
            consistencyLevel: 'vm',
            vmNames: ['erp-app-5'],
          },
        ],
      },
      {
        waveKey: '3',
        vms: [
          { name: 'erp-web-1', namespace: 'erp-web' },
          { name: 'erp-web-2', namespace: 'erp-web' },
          { name: 'erp-web-3', namespace: 'erp-web' },
          { name: 'erp-web-4', namespace: 'erp-web' },
        ],
        groups: [
          {
            name: 'drgroup-4',
            namespace: 'erp-web',
            consistencyLevel: 'namespace',
            vmNames: ['erp-web-1', 'erp-web-2', 'erp-web-3', 'erp-web-4'],
          },
        ],
      },
    ],
    replicationHealth: [
      {
        name: 'drgroup-1',
        namespace: 'erp-db',
        health: 'Degraded',
        lastChecked: '2026-04-25T15:00:00Z',
      },
      {
        name: 'drgroup-2',
        namespace: 'erp-apps',
        health: 'Healthy',
        lastChecked: '2026-04-25T15:00:00Z',
      },
      {
        name: 'drgroup-3',
        namespace: 'erp-standalone',
        health: 'Healthy',
        lastChecked: '2026-04-25T15:00:00Z',
      },
      {
        name: 'drgroup-4',
        namespace: 'erp-web',
        health: 'Healthy',
        lastChecked: '2026-04-25T15:00:00Z',
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

const mockPlanNoWaves: DRPlan = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRPlan',
  metadata: { name: 'empty-plan', uid: '2', creationTimestamp: '' },
  spec: {
    maxConcurrentFailovers: 2,
    primarySite: 'dc1',
    secondarySite: 'dc2',
    volumeReplicationDriver: 'noop',
  },
  status: { phase: 'SteadyState', waves: [] },
};

describe('WaveCompositionTree', () => {
  it('renders 3 wave nodes with labels', () => {
    render(<WaveCompositionTree plan={mockPlanWithWaves} />);
    expect(screen.getByText(/Wave 1/)).toBeInTheDocument();
    expect(screen.getByText(/Wave 2/)).toBeInTheDocument();
    expect(screen.getByText(/Wave 3/)).toBeInTheDocument();
  });

  it('shows VM count per wave header', () => {
    render(<WaveCompositionTree plan={mockPlanWithWaves} />);
    expect(screen.getByText(/3 VMs/)).toBeInTheDocument();
    expect(screen.getByText(/5 VMs/)).toBeInTheDocument();
    expect(screen.getByText(/4 VMs/)).toBeInTheDocument();
  });

  it('shows aggregate health badge per wave with counts', () => {
    render(<WaveCompositionTree plan={mockPlanWithWaves} />);
    expect(screen.getByText('1 Degraded')).toBeInTheDocument();
    expect(screen.getAllByText('All Healthy').length).toBeGreaterThanOrEqual(2);
  });

  it('renders TreeView with aria-label', () => {
    render(<WaveCompositionTree plan={mockPlanWithWaves} />);
    expect(screen.getByLabelText('Wave composition')).toBeInTheDocument();
  });

  it('renders tree items as role=treeitem', () => {
    render(<WaveCompositionTree plan={mockPlanWithWaves} />);
    const treeItems = screen.getAllByRole('treeitem');
    expect(treeItems.length).toBeGreaterThanOrEqual(3);
  });

  it('expands wave to reveal DRGroup chunks with VM and VG sub-nodes', () => {
    render(<WaveCompositionTree plan={mockPlanWithWaves} />);
    const wave1Button = screen.getAllByRole('treeitem')[0].querySelector('button');
    fireEvent.click(wave1Button!);
    expect(screen.getByText(/DRGroup chunk 1/)).toBeInTheDocument();
    expect(screen.getByText('Virtual Machines (3)')).toBeInTheDocument();
    expect(screen.getByText('erp-db-1')).toBeInTheDocument();
    expect(screen.getByText('erp-db-2')).toBeInTheDocument();
    expect(screen.getByText('erp-db-3')).toBeInTheDocument();
  });

  it('namespace-consistent VMs show NS label under Virtual Machines group', () => {
    render(<WaveCompositionTree plan={mockPlanWithWaves} />);
    const wave1Button = screen.getAllByRole('treeitem')[0].querySelector('button');
    fireEvent.click(wave1Button!);
    expect(screen.getByText('Virtual Machines (3)')).toBeInTheDocument();
    const nsLabels = screen.getAllByText('NS: erp-db');
    expect(nsLabels.length).toBe(3);
  });

  it('VM-level consistency VMs show VM-level text', () => {
    render(<WaveCompositionTree plan={mockPlanWithWaves} />);
    const wave2Button = screen.getAllByRole('treeitem')[1].querySelector('button');
    fireEvent.click(wave2Button!);
    expect(screen.getByText('VM-level')).toBeInTheDocument();
  });

  it('renders VM nodes when groups are absent but vms exist', () => {
    const planVMsOnly: DRPlan = {
      ...mockPlanWithWaves,
      status: {
        ...mockPlanWithWaves.status!,
        waves: [
          {
            waveKey: '1',
            vms: [
              { name: 'vm-a', namespace: 'ns-a' },
              { name: 'vm-b', namespace: 'ns-b' },
            ],
          },
        ],
      },
    };
    render(<WaveCompositionTree plan={planVMsOnly} />);
    expect(screen.getByText(/Wave 1/)).toBeInTheDocument();
    expect(screen.getByText(/2 VMs/)).toBeInTheDocument();
    const wave1Button = screen.getAllByRole('treeitem')[0].querySelector('button');
    fireEvent.click(wave1Button!);
    expect(screen.getByText('vm-a')).toBeInTheDocument();
    expect(screen.getByText('vm-b')).toBeInTheDocument();
  });

  it('renders correctly with empty waves array', () => {
    render(<WaveCompositionTree plan={mockPlanNoWaves} />);
    expect(screen.getByText('No waves configured for this plan')).toBeInTheDocument();
  });

  it('renders correctly when status.waves is undefined', () => {
    const planNoStatus: DRPlan = {
      ...mockPlanNoWaves,
      status: { phase: 'SteadyState' },
    };
    render(<WaveCompositionTree plan={planNoStatus} />);
    expect(screen.getByText('No waves configured for this plan')).toBeInTheDocument();
  });

  it('has no accessibility violations', async () => {
    const { container } = render(<WaveCompositionTree plan={mockPlanWithWaves} />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations with empty waves', async () => {
    const { container } = render(<WaveCompositionTree plan={mockPlanNoWaves} />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  describe('VG disk composition', () => {
    const planWithCrossSiteDisks: DRPlan = {
      ...mockPlanWithWaves,
      status: {
        ...mockPlanWithWaves.status!,
        preflight: {
          totalVMs: 12,
          waves: [
            {
              waveKey: '1',
              vmCount: 3,
              chunks: [
                {
                  name: 'chunk-1',
                  vmCount: 3,
                  vmNames: ['erp-db-1', 'erp-db-2', 'erp-db-3'],
                  volumeGroups: [
                    {
                      name: 'vg-db',
                      disks: [
                        {
                          name: 'disk-root',
                          sites: [
                            { site: 'dc1-prod', pvcName: 'pvc-root', pvcNamespace: 'erp-db' },
                            { site: 'dc2-prod', pvcName: 'pvc-root-dr', pvcNamespace: 'erp-db' },
                          ],
                        },
                        {
                          name: 'disk-data',
                          sites: [
                            { site: 'dc1-prod', pvcName: 'pvc-data', pvcNamespace: 'erp-db' },
                            { site: 'dc2-prod', pvcName: 'pvc-data-dr', pvcNamespace: 'erp-db' },
                          ],
                        },
                      ],
                    },
                  ],
                },
              ],
            },
          ],
        },
      },
    };

    it('VG sub-node shows disk list when expanded', () => {
      render(<WaveCompositionTree plan={planWithCrossSiteDisks} />);
      const wave1Button = screen.getAllByRole('treeitem')[0].querySelector('button');
      fireEvent.click(wave1Button!);
      expect(screen.getByText('Volume Groups (1)')).toBeInTheDocument();
      const vgGroupButton = screen.getByText('Volume Groups (1)').closest('[role="treeitem"]')?.querySelector('button');
      fireEvent.click(vgGroupButton!);
      expect(screen.getByText('vg-db')).toBeInTheDocument();
      expect(screen.getByText('2 disks')).toBeInTheDocument();
    });

    it('disk node shows per-site PVC mapping with site labels', () => {
      render(<WaveCompositionTree plan={planWithCrossSiteDisks} />);
      const wave1Button = screen.getAllByRole('treeitem')[0].querySelector('button');
      fireEvent.click(wave1Button!);
      const vgGroupButton = screen.getByText('Volume Groups (1)').closest('[role="treeitem"]')?.querySelector('button');
      fireEvent.click(vgGroupButton!);
      const vgButton = screen.getByText('vg-db').closest('[role="treeitem"]')?.querySelector('button');
      fireEvent.click(vgButton!);
      expect(screen.getByText('disk-root')).toBeInTheDocument();
      expect(screen.getByText('disk-data')).toBeInTheDocument();
      const diskRootButton = screen.getByText('disk-root').closest('[role="treeitem"]')?.querySelector('button');
      fireEvent.click(diskRootButton!);
      expect(screen.getByText('dc1-prod')).toBeInTheDocument();
      expect(screen.getByText('dc2-prod')).toBeInTheDocument();
    });

    it('VG node does not show site label when disks have per-site sites', () => {
      render(<WaveCompositionTree plan={planWithCrossSiteDisks} />);
      const wave1Button = screen.getAllByRole('treeitem')[0].querySelector('button');
      fireEvent.click(wave1Button!);
      const vgGroupButton = screen.getByText('Volume Groups (1)').closest('[role="treeitem"]')?.querySelector('button');
      fireEvent.click(vgGroupButton!);
      const vgNode = screen.getByText('vg-db').closest('[role="treeitem"]');
      const labels = vgNode?.querySelectorAll('.pf-v5-c-label, .pf-v6-c-label');
      const siteLabels = Array.from(labels ?? []).filter((l) => l.textContent?.includes('dc1-prod') || l.textContent?.includes('dc2-prod'));
      expect(siteLabels.length).toBe(0);
    });

    it('backward compat — old format with flat pvcName/pvcNamespace renders correctly', () => {
      const planFlatDisks: DRPlan = {
        ...mockPlanWithWaves,
        status: {
          ...mockPlanWithWaves.status!,
          preflight: {
            totalVMs: 12,
            waves: [
              {
                waveKey: '1',
                vmCount: 3,
                chunks: [
                  {
                    name: 'chunk-1',
                    vmCount: 3,
                    vmNames: ['erp-db-1', 'erp-db-2', 'erp-db-3'],
                    volumeGroups: [
                      {
                        name: 'vg-db',
                        disks: [
                          { name: 'disk-root', pvcName: 'pvc-root', pvcNamespace: 'erp-db' },
                        ],
                      },
                    ],
                  },
                ],
              },
            ],
          },
        },
      };
      render(<WaveCompositionTree plan={planFlatDisks} />);
      const wave1Button = screen.getAllByRole('treeitem')[0].querySelector('button');
      fireEvent.click(wave1Button!);
      const vgGroupButton = screen.getByText('Volume Groups (1)').closest('[role="treeitem"]')?.querySelector('button');
      fireEvent.click(vgGroupButton!);
      const vgButton = screen.getByText('vg-db').closest('[role="treeitem"]')?.querySelector('button');
      fireEvent.click(vgButton!);
      expect(screen.getByText(/disk-root → pvc-root \(erp-db\)/)).toBeInTheDocument();
    });

    it('backward compat — VG with site field renders VG-level site label', () => {
      const planWithVGSite: DRPlan = {
        ...mockPlanWithWaves,
        status: {
          ...mockPlanWithWaves.status!,
          preflight: {
            totalVMs: 12,
            waves: [
              {
                waveKey: '1',
                vmCount: 3,
                chunks: [
                  {
                    name: 'chunk-1',
                    vmCount: 3,
                    vmNames: ['erp-db-1', 'erp-db-2', 'erp-db-3'],
                    volumeGroups: [
                      {
                        name: 'vg-db',
                        site: 'dc1-prod',
                        disks: [
                          { name: 'disk-root', pvcName: 'pvc-root', pvcNamespace: 'erp-db' },
                        ],
                      },
                    ],
                  },
                ],
              },
            ],
          },
        },
      };
      render(<WaveCompositionTree plan={planWithVGSite} />);
      const wave1Button = screen.getAllByRole('treeitem')[0].querySelector('button');
      fireEvent.click(wave1Button!);
      const vgGroupButton = screen.getByText('Volume Groups (1)').closest('[role="treeitem"]')?.querySelector('button');
      fireEvent.click(vgGroupButton!);
      expect(screen.getByText('dc1-prod')).toBeInTheDocument();
    });

    it('backward compat — string[] volumeGroups render as names only (no disk nodes)', () => {
      const planStringVGs: DRPlan = {
        ...mockPlanWithWaves,
        status: {
          ...mockPlanWithWaves.status!,
          preflight: {
            totalVMs: 12,
            waves: [
              {
                waveKey: '1',
                vmCount: 3,
                chunks: [
                  {
                    name: 'chunk-1',
                    vmCount: 3,
                    vmNames: ['erp-db-1', 'erp-db-2', 'erp-db-3'],
                    volumeGroups: ['vg-string-1' as unknown as import('../../src/models/types').PreflightVolumeGroup],
                  },
                ],
              },
            ],
          },
        },
      };
      render(<WaveCompositionTree plan={planStringVGs} />);
      const wave1Button = screen.getAllByRole('treeitem')[0].querySelector('button');
      fireEvent.click(wave1Button!);
      expect(screen.getByText('Volume Groups (1)')).toBeInTheDocument();
      const vgGroupButton = screen.getByText('Volume Groups (1)').closest('[role="treeitem"]')?.querySelector('button');
      fireEvent.click(vgGroupButton!);
      expect(screen.getByText('vg-string-1')).toBeInTheDocument();
    });

    it('has no accessibility violations with cross-site disk nodes', async () => {
      const { container } = render(<WaveCompositionTree plan={planWithCrossSiteDisks} />);
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });
  });
});
