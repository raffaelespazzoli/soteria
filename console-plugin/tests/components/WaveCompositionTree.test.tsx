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

  it('expands wave to reveal DRGroup chunks and per-VM rows', () => {
    render(<WaveCompositionTree plan={mockPlanWithWaves} />);
    const wave1Button = screen.getAllByRole('treeitem')[0].querySelector('button');
    fireEvent.click(wave1Button!);
    expect(screen.getByText(/DRGroup chunk 1/)).toBeInTheDocument();
    expect(screen.getByText('erp-db-1')).toBeInTheDocument();
    expect(screen.getByText('erp-db-2')).toBeInTheDocument();
    expect(screen.getByText('erp-db-3')).toBeInTheDocument();
  });

  it('namespace-consistent VMs show NS label', () => {
    render(<WaveCompositionTree plan={mockPlanWithWaves} />);
    const wave1Button = screen.getAllByRole('treeitem')[0].querySelector('button');
    fireEvent.click(wave1Button!);
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
});
