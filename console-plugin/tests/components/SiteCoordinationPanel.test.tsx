import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import SiteCoordinationPanel from '../../src/components/ExecutionDetail/SiteCoordinationPanel';
import { DRExecution } from '../../src/models/types';

expect.extend(toHaveNoViolations);

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({}));

function makePlannedExecution(
  siteStatuses?: DRExecution['status']['siteStatuses'],
  waves?: DRExecution['status']['waves'],
): DRExecution {
  return {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRExecution',
    metadata: { name: 'test-exec', uid: '1' },
    spec: { planName: 'test-plan', mode: 'planned_migration' },
    status: {
      isActive: true,
      phase: 'Executing',
      siteStatuses,
      waves,
    },
  };
}

function makeReprotectExecution(
  siteStatuses?: DRExecution['status']['siteStatuses'],
): DRExecution {
  return {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRExecution',
    metadata: { name: 'test-reprotect', uid: '2' },
    spec: { planName: 'test-plan', mode: 'reprotect' },
    status: {
      isActive: true,
      phase: 'Executing',
      siteStatuses,
    },
  };
}

describe('SiteCoordinationPanel', () => {
  it('renders source and target lanes for planned migration', () => {
    const exec = makePlannedExecution({
      east: { vmsStopped: true },
      west: { resyncPending: true },
    });

    render(
      <SiteCoordinationPanel execution={exec} sourceSite="east" targetSite="west" />,
    );

    expect(screen.getByTestId('site-coordination-panel')).toBeInTheDocument();
    expect(screen.getByTestId('site-lane-east')).toBeInTheDocument();
    expect(screen.getByTestId('site-lane-west')).toBeInTheDocument();
    expect(screen.getByText('Source')).toBeInTheDocument();
    expect(screen.getByText('Target')).toBeInTheDocument();
  });

  it('shows pending steps when siteStatuses is empty', () => {
    const exec = makePlannedExecution({});

    render(
      <SiteCoordinationPanel execution={exec} sourceSite="east" targetSite="west" />,
    );

    expect(screen.getByText('Stopping VMs')).toBeInTheDocument();
    expect(screen.getByText('Handoff Complete')).toBeInTheDocument();
    expect(screen.getByText('Syncing Volumes')).toBeInTheDocument();
    expect(screen.getByText('Volumes Synced')).toBeInTheDocument();
  });

  it('returns null for disaster mode', () => {
    const exec: DRExecution = {
      apiVersion: 'soteria.io/v1alpha1',
      kind: 'DRExecution',
      metadata: { name: 'disaster', uid: '3' },
      spec: { planName: 'test-plan', mode: 'disaster' },
      status: { isActive: true, phase: 'Executing' },
    };

    const { container } = render(
      <SiteCoordinationPanel execution={exec} sourceSite="east" targetSite="west" />,
    );

    expect(container.innerHTML).toBe('');
  });

  it('hides when all steps complete and waves have started', () => {
    const exec = makePlannedExecution(
      {
        east: { vmsStopped: true, step0Complete: true },
        west: { resyncPending: true, resyncComplete: true },
      },
      [{ waveIndex: 0, groups: [] }],
    );

    const { container } = render(
      <SiteCoordinationPanel execution={exec} sourceSite="east" targetSite="west" />,
    );

    expect(container.innerHTML).toBe('');
  });

  it('stays visible when all steps complete but no waves yet', () => {
    const exec = makePlannedExecution({
      east: { vmsStopped: true, step0Complete: true },
      west: { resyncPending: true, resyncComplete: true },
    });

    render(
      <SiteCoordinationPanel execution={exec} sourceSite="east" targetSite="west" />,
    );

    expect(screen.getByTestId('site-coordination-panel')).toBeInTheDocument();
  });

  it('renders passive lane for reprotect mode', () => {
    const exec = makeReprotectExecution({
      west: { resyncComplete: false },
    });

    render(
      <SiteCoordinationPanel execution={exec} sourceSite="east" targetSite="west" />,
    );

    expect(screen.getByText('Passive')).toBeInTheDocument();
    expect(screen.getByText('Ensuring Replication')).toBeInTheDocument();
    expect(screen.queryByText('Source')).not.toBeInTheDocument();
  });

  it('has no accessibility violations', async () => {
    const exec = makePlannedExecution({
      east: { vmsStopped: true },
      west: {},
    });

    const { container } = render(
      <SiteCoordinationPanel execution={exec} sourceSite="east" targetSite="west" />,
    );

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
