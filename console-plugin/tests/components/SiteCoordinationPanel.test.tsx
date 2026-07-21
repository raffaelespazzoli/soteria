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
      east: { demotionComplete: true },
      west: {},
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

    expect(screen.getByText('Demoting Volumes')).toBeInTheDocument();
    expect(screen.getByText('Demotion Synced')).toBeInTheDocument();
    expect(screen.getByText('Promoting Volumes')).toBeInTheDocument();
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
        east: { demotionComplete: true },
        west: { step0Complete: true },
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
      east: { demotionComplete: true },
      west: { step0Complete: true },
    });

    render(
      <SiteCoordinationPanel execution={exec} sourceSite="east" targetSite="west" />,
    );

    expect(screen.getByTestId('site-coordination-panel')).toBeInTheDocument();
  });

  it('returns null for reprotect mode (no coordination steps)', () => {
    const exec = makeReprotectExecution({});

    const { container } = render(
      <SiteCoordinationPanel execution={exec} sourceSite="east" targetSite="west" />,
    );

    expect(container.innerHTML).toBe('');
  });

  it('returns null for terminal execution (isActive=false)', () => {
    const exec: DRExecution = {
      apiVersion: 'soteria.io/v1alpha1',
      kind: 'DRExecution',
      metadata: { name: 'done-exec', uid: '4' },
      spec: { planName: 'test-plan', mode: 'planned_migration' },
      status: {
        isActive: false,
        phase: 'Succeeded',
        result: 'Succeeded',
        siteStatuses: {
          east: { demotionComplete: true },
          west: { step0Complete: true },
        },
        waves: [{ waveIndex: 0, groups: [] }],
      },
    };

    const { container } = render(
      <SiteCoordinationPanel execution={exec} sourceSite="west" targetSite="east" />,
    );

    expect(container.innerHTML).toBe('');
  });

  it('has no accessibility violations', async () => {
    const exec = makePlannedExecution({
      east: { demotionComplete: true },
      west: {},
    });

    const { container } = render(
      <SiteCoordinationPanel execution={exec} sourceSite="east" targetSite="west" />,
    );

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
