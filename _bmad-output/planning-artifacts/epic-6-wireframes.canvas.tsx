import {
  Button,
  Callout,
  Card,
  CardBody,
  CardHeader,
  Code,
  Divider,
  Grid,
  H1,
  H2,
  H3,
  Pill,
  Row,
  Spacer,
  Stack,
  Stat,
  Table,
  Text,
  useHostTheme,
} from 'cursor/canvas';
import { useState, type CSSProperties } from 'react';

function StatusBadge({ label, tone }: { label: string; tone: 'success' | 'warning' | 'danger' | 'info' | 'neutral' }) {
  const toneMap: Record<string, 'success' | 'warning' | 'info' | 'added' | 'deleted' | 'neutral'> = {
    success: 'success', warning: 'warning', danger: 'deleted', info: 'info', neutral: 'neutral',
  };
  return <Pill active tone={toneMap[tone]} size="sm">{label}</Pill>;
}

function WireframeBox({ label, style }: { label: string; style?: CSSProperties }) {
  const theme = useHostTheme();
  return (
    <div style={{
      border: `1px dashed ${theme.stroke.secondary}`, borderRadius: 4,
      padding: '6px 10px', color: theme.text.secondary, fontSize: 11,
      textAlign: 'center' as const, ...style,
    }}>
      {label}
    </div>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  const theme = useHostTheme();
  return (
    <Text size="small" tone="secondary" style={{ fontStyle: 'italic', borderLeft: `2px solid ${theme.accent}`, paddingLeft: 8 }}>
      {children}
    </Text>
  );
}

/* ─── Phase State Machine ─── */
type RestPhase = 'SteadyState' | 'FailedOver' | 'DRedSteadyState' | 'FailedBack';
type TransientPhase = 'FailingOver' | 'Reprotecting' | 'FailingBack' | 'Restoring';
type DemoPhase = RestPhase | TransientPhase;

const REST_PHASES: { id: RestPhase; label: string; description: string; vm: string; dc1: string; dc2: string; replication: string }[] = [
  { id: 'SteadyState', label: 'Steady State', description: 'Normal operations', vm: 'VM on DC1', dc1: 'Active (source)', dc2: 'Passive (target)', replication: 'DC1 → DC2' },
  { id: 'FailedOver', label: 'Failed Over', description: 'Running on DR site', vm: 'VM on DC2', dc1: 'Passive / down', dc2: 'Active (promoted)', replication: 'None' },
  { id: 'DRedSteadyState', label: 'DR-ed Steady State', description: 'Protected on DR site', vm: 'VM on DC2', dc1: 'Passive (target)', dc2: 'Active (source)', replication: 'DC2 → DC1' },
  { id: 'FailedBack', label: 'Failed Back', description: 'Returned to origin', vm: 'VM on DC1', dc1: 'Active (promoted)', dc2: 'Passive / down', replication: 'None' },
];

const TRANSITIONS: { from: RestPhase; to: RestPhase; action: string; transient: TransientPhase; isDanger: boolean }[] = [
  { from: 'SteadyState', to: 'FailedOver', action: 'Failover', transient: 'FailingOver', isDanger: true },
  { from: 'FailedOver', to: 'DRedSteadyState', action: 'Reprotect', transient: 'Reprotecting', isDanger: false },
  { from: 'DRedSteadyState', to: 'FailedBack', action: 'Failback', transient: 'FailingBack', isDanger: false },
  { from: 'FailedBack', to: 'SteadyState', action: 'Restore', transient: 'Restoring', isDanger: false },
];

function isTransient(phase: DemoPhase): phase is TransientPhase {
  return ['FailingOver', 'Reprotecting', 'FailingBack', 'Restoring'].includes(phase);
}

function getTransientInfo(phase: TransientPhase) {
  return TRANSITIONS.find(t => t.transient === phase)!;
}

function getRestTransition(phase: RestPhase) {
  return TRANSITIONS.find(t => t.from === phase);
}

function PhaseNode({ phase, isActive, isTransitioning }: {
  phase: typeof REST_PHASES[number]; isActive: boolean; isTransitioning: boolean;
}) {
  const theme = useHostTheme();
  const bg = isActive ? theme.accent : 'transparent';
  const border = isActive ? theme.accent : isTransitioning ? theme.accent : theme.stroke.secondary;
  const textColor = isActive ? '#fff' : theme.text.primary;
  const subColor = isActive ? 'rgba(255,255,255,0.8)' : theme.text.secondary;
  const opacity = isActive || isTransitioning ? 1 : 0.35;

  return (
    <div style={{
      border: `2px ${isTransitioning ? 'dashed' : 'solid'} ${border}`,
      borderRadius: 10, padding: '14px 16px',
      background: isActive ? bg : 'transparent',
      opacity, minWidth: 180, position: 'relative' as const,
    }}>
      <Stack gap={6}>
        <Text weight="bold" style={{ color: textColor, fontSize: 14 }}>{phase.label}</Text>
        <Text size="small" style={{ color: subColor }}>{phase.description}</Text>
        <Divider />
        <Text size="small" style={{ color: subColor }}>{phase.vm}</Text>
        <Text size="small" style={{ color: subColor }}>DC1: {phase.dc1}</Text>
        <Text size="small" style={{ color: subColor }}>DC2: {phase.dc2}</Text>
        {phase.replication !== 'None' ? (
          <Text size="small" weight="semibold" style={{ color: subColor }}>Repl: {phase.replication}</Text>
        ) : (
          <Text size="small" style={{ color: subColor, fontStyle: 'italic' }}>No replication</Text>
        )}
      </Stack>
    </div>
  );
}

function TransitionEdge({ action, isDanger, state, position }: {
  action: string;
  isDanger: boolean;
  state: 'idle' | 'available' | 'in-progress';
  position: 'horizontal' | 'vertical-down' | 'vertical-up';
}) {
  const theme = useHostTheme();
  const isHoriz = position === 'horizontal';
  const arrow = isHoriz ? '→' : position === 'vertical-down' ? '↓' : '↑';

  return (
    <div style={{
      display: 'flex',
      flexDirection: isHoriz ? 'column' : 'column',
      alignItems: 'center', gap: 4, minWidth: isHoriz ? 110 : 80,
    }}>
      {state === 'available' && (
        <Button variant={isDanger ? 'primary' : 'secondary'}>
          {action}
        </Button>
      )}
      {state === 'in-progress' && (
        <Stack gap={2} style={{ alignItems: 'center' }}>
          <Pill active tone="info" size="sm">{action}</Pill>
          <Text size="small" style={{ color: theme.accent }}>In progress...</Text>
        </Stack>
      )}
      {state === 'idle' && (
        <Text size="small" tone="tertiary">{action}</Text>
      )}
      <Text size="small" tone="tertiary">{arrow}</Text>
    </div>
  );
}

function StateMachineDiagram({ currentPhase }: { currentPhase: DemoPhase }) {
  const inTransit = isTransient(currentPhase);
  const transitInfo = inTransit ? getTransientInfo(currentPhase) : null;

  const nodeState = (id: RestPhase): { isActive: boolean; isTransitioning: boolean } => {
    if (inTransit) {
      return {
        isActive: id === transitInfo!.from,
        isTransitioning: id === transitInfo!.to,
      };
    }
    return { isActive: id === currentPhase, isTransitioning: false };
  };

  const edgeState = (from: RestPhase): 'idle' | 'available' | 'in-progress' => {
    if (inTransit) {
      return transitInfo!.from === from ? 'in-progress' : 'idle';
    }
    return from === currentPhase ? 'available' : 'idle';
  };

  return (
    <Stack gap={16}>
      {/* Top row: SteadyState → FailedOver */}
      <Row gap={0} align="stretch" justify="center" wrap>
        <PhaseNode phase={REST_PHASES[0]} {...nodeState('SteadyState')} />
        <div style={{ display: 'flex', alignItems: 'center', padding: '0 12px' }}>
          <TransitionEdge action="Failover" isDanger state={edgeState('SteadyState')} position="horizontal" />
        </div>
        <PhaseNode phase={REST_PHASES[1]} {...nodeState('FailedOver')} />
      </Row>

      {/* Middle: vertical connectors */}
      <Row justify="space-between" style={{ padding: '0 60px' }}>
        <TransitionEdge action="Restore" isDanger={false} state={edgeState('FailedBack')} position="vertical-up" />
        <TransitionEdge action="Reprotect" isDanger={false} state={edgeState('FailedOver')} position="vertical-down" />
      </Row>

      {/* Bottom row: FailedBack ← DRedSteadyState */}
      <Row gap={0} align="stretch" justify="center" wrap>
        <PhaseNode phase={REST_PHASES[3]} {...nodeState('FailedBack')} />
        <div style={{ display: 'flex', alignItems: 'center', padding: '0 12px' }}>
          <TransitionEdge action="Failback" isDanger={false} state={edgeState('DRedSteadyState')} position="horizontal" />
        </div>
        <PhaseNode phase={REST_PHASES[2]} {...nodeState('DRedSteadyState')} />
      </Row>
    </Stack>
  );
}

/* ─── Screen 1: DR Dashboard ─── */
function DashboardWireframe() {
  const theme = useHostTheme();

  const dashboardRows: React.ReactNode[][] = [
    [
      <Text weight="semibold" style={{ color: theme.accent }}>erp-full-stack</Text>,
      <StatusBadge label="SteadyState" tone="success" />,
      'dc1-prod',
      <Row gap={4} align="center"><StatusBadge label="Healthy" tone="success" /><Text size="small" tone="secondary">RPO 12s</Text></Row>,
      <Row gap={4} align="center"><Text size="small">Apr 20</Text><StatusBadge label="Succeeded" tone="success" /></Row>,
      <Text size="small" tone="tertiary">⋮</Text>,
    ],
    [
      <Text weight="semibold" style={{ color: theme.accent }}>crm-app</Text>,
      <StatusBadge label="FailedOver" tone="info" />,
      'dc2-prod',
      <Row gap={4} align="center"><StatusBadge label="Degraded" tone="warning" /><Text size="small" tone="secondary">RPO 45s</Text></Row>,
      <Row gap={4} align="center"><Text size="small">Apr 18</Text><StatusBadge label="PartiallySucceeded" tone="warning" /></Row>,
      <Text size="small" tone="tertiary">⋮</Text>,
    ],
    [
      <Text weight="semibold" style={{ color: theme.accent }}>analytics-pipeline</Text>,
      <StatusBadge label="FailingOver" tone="info" />,
      'dc1-prod',
      <Row gap={4} align="center"><StatusBadge label="Unknown" tone="neutral" /><Text size="small" tone="secondary">--</Text></Row>,
      <Row gap={4} align="center"><Text size="small">Apr 15</Text><StatusBadge label="Succeeded" tone="success" /></Row>,
      <Text size="small" tone="tertiary">⋮</Text>,
    ],
    [
      <Text weight="semibold" style={{ color: theme.accent }}>payment-gateway</Text>,
      <StatusBadge label="SteadyState" tone="success" />,
      'dc1-prod',
      <Row gap={4} align="center"><StatusBadge label="Error" tone="danger" /><Text size="small" tone="secondary">RPO unknown</Text></Row>,
      <Row gap={4} align="center"><Text size="small">Mar 10</Text><StatusBadge label="Succeeded" tone="success" /></Row>,
      <Text size="small" tone="tertiary">⋮</Text>,
    ],
    [
      <Text weight="semibold" style={{ color: theme.accent }}>hr-portal</Text>,
      <StatusBadge label="SteadyState" tone="success" />,
      'dc1-prod',
      <Row gap={4} align="center"><StatusBadge label="Healthy" tone="success" /><Text size="small" tone="secondary">RPO 8s</Text></Row>,
      <Row gap={4} align="center"><Text size="small">Apr 22</Text><StatusBadge label="Succeeded" tone="success" /></Row>,
      <Text size="small" tone="tertiary">⋮</Text>,
    ],
  ];

  return (
    <Stack gap={14}>
      <H2>Screen 1 — DR Dashboard</H2>
      <SectionLabel>
        6 columns: Name, Phase, Active On, Protected (health + RPO), Last Execution, Actions. Clean scan at 500-plan scale.
      </SectionLabel>

      <Row gap={6} align="center">
        <Text size="small" tone="secondary">Home</Text>
        <Text size="small" tone="tertiary">/</Text>
        <Text size="small" weight="semibold">Disaster Recovery</Text>
      </Row>

      <H3>Alert Banners (persistent, not dismissible)</H3>
      <Callout tone="danger" title="1 DR Plan running UNPROTECTED — replication broken">
        <Text size="small">payment-gateway has replication errors. Click to filter affected plans.</Text>
      </Callout>
      <Callout tone="warning" title="1 plan with degraded replication">
        <Text size="small">crm-app replication is degraded (RPO 45s). Investigate storage connectivity.</Text>
      </Callout>
      <Callout tone="warning" title="1 plan not tested in 30+ days">
        <Text size="small">payment-gateway last tested Mar 10 (46 days ago). Schedule a planned migration.</Text>
      </Callout>

      <H3>Toolbar</H3>
      <Row gap={8} align="center" wrap>
        <WireframeBox label="Search: plan name" style={{ minWidth: 180 }} />
        <WireframeBox label="Phase ▾" />
        <WireframeBox label="Active On ▾" />
        <WireframeBox label="Protected ▾" />
        <WireframeBox label="Last Execution ▾" />
        <Spacer />
        <Text size="small" tone="secondary">Showing 5 of 247 plans</Text>
      </Row>

      <H3>Plan Table (default sort: problems first)</H3>
      <Table
        headers={['Name', 'Phase', 'Active On', 'Protected', 'Last Execution', '']}
        rows={dashboardRows}
        rowTone={[undefined, 'warning', undefined, 'danger', undefined]}
        striped
        stickyHeader
      />
      <Text size="small" tone="tertiary">
        Row click navigates to Plan Detail. Kebab menu (⋮) shows only valid actions for current phase.
      </Text>
    </Stack>
  );
}

/* ─── Screen 2: Plan Detail with State Machine Overview ─── */
function PlanDetailWireframe() {
  const theme = useHostTheme();
  const DEMO_ORDER: DemoPhase[] = [
    'SteadyState', 'FailingOver', 'FailedOver', 'Reprotecting',
    'DRedSteadyState', 'FailingBack', 'FailedBack', 'Restoring',
  ];
  const [demoIdx, setDemoIdx] = useState(0);
  const demoPhase = DEMO_ORDER[demoIdx];

  const cyclePhase = () => setDemoIdx((demoIdx + 1) % DEMO_ORDER.length);

  const inTransit = isTransient(demoPhase);
  const transitInfo = inTransit ? getTransientInfo(demoPhase) : null;

  return (
    <Stack gap={14}>
      <H2>Screen 2 — Plan Detail Page</H2>
      <SectionLabel>
        Overview tab shows the 4-phase DR lifecycle as a visual cycle. Only the current phase is highlighted.
        During transitions, a progress banner replaces the action button and all other buttons are disabled.
        Confirmation is handled by a popup modal (pre-flight dialog), not inline text.
      </SectionLabel>

      {/* Breadcrumb */}
      <Row gap={6} align="center">
        <Text size="small" style={{ color: theme.accent }}>DR Dashboard</Text>
        <Text size="small" tone="tertiary">/</Text>
        <Text size="small" weight="semibold">erp-full-stack</Text>
      </Row>

      {/* Tab bar */}
      <Row gap={4}>
        <Pill active>Overview</Pill>
        <Pill>Waves</Pill>
        <Pill>History</Pill>
        <Pill>Configuration</Pill>
      </Row>

      <Divider />

      <H3>Overview Tab — DR Lifecycle</H3>

      {/* Interactive demo */}
      <Row gap={8} align="center">
        <Text size="small" tone="secondary">Demo — cycles through rest + transient states:</Text>
        <Button variant="ghost" onClick={cyclePhase}>Next state</Button>
        {inTransit ? (
          <Pill tone="info" size="sm">{demoPhase}</Pill>
        ) : (
          <StatusBadge label={demoPhase} tone={
            demoPhase === 'SteadyState' || demoPhase === 'DRedSteadyState' ? 'success' : 'info'
          } />
        )}
      </Row>

      {/* Plan header */}
      <Row gap={16} align="center" wrap>
        <Stack gap={2}>
          <Text weight="bold" style={{ fontSize: 16 }}>erp-full-stack</Text>
          <Text size="small" tone="secondary">12 VMs across 3 waves</Text>
        </Stack>
        <Spacer />
        <Row gap={4} align="center">
          <Text size="small" tone="secondary">Active on:</Text>
          <Text weight="semibold" size="small">
            {(demoPhase === 'SteadyState' || demoPhase === 'FailedBack' || demoPhase === 'Restoring' || demoPhase === 'FailingOver')
              ? 'dc1-prod' : 'dc2-prod'}
          </Text>
        </Row>
      </Row>

      {/* Transition progress banner */}
      {inTransit && (
        <Callout tone="info" title={`${transitInfo!.action} in progress`}>
          <Stack gap={4}>
            <Text size="small">
              Transitioning from {transitInfo!.from} to {transitInfo!.to}. Wave 2 of 3 executing — 7 of 12 VMs processed.
            </Text>
            <Row gap={8} align="center">
              <WireframeBox label="████████████░░░░░░░░ 58%" style={{ minWidth: 200, fontFamily: 'monospace', fontSize: 12 }} />
              <Text size="small" tone="secondary">Elapsed: 8m 14s · Est. remaining: ~6m</Text>
            </Row>
            <Text size="small" tone="secondary">View live execution details in the History tab.</Text>
          </Stack>
        </Callout>
      )}

      <Divider />

      {/* State machine diagram */}
      <StateMachineDiagram currentPhase={demoPhase} />

      <Divider />

      {/* Design notes */}
      <H3>Interaction Design Notes</H3>
      <Table
        headers={['Behavior', 'Description']}
        rows={[
          ['Single highlight', 'Only the current rest phase is filled with accent color. All other phases are faded to 35% opacity.'],
          ['Action buttons', 'Only the outgoing transition from the current rest phase shows an enabled button. Clicking opens a pre-flight confirmation popup.'],
          ['Confirmation popup', 'Modal dialog with pre-flight summary (VM count, RPO, duration estimate) and typed keyword confirmation. Not shown inline.'],
          ['Transition in progress', 'During FailingOver / Reprotecting / FailingBack / Restoring: the arrow shows "In progress" with a progress banner above. All action buttons disappear.'],
          ['Transition target', 'The destination phase node gets a dashed accent border during transition — visual "arriving here" indication.'],
          ['Failover button style', 'Danger (red) variant reserved exclusively for Failover. All other transitions use secondary (neutral) variant.'],
        ]}
        framed={false}
      />

      <Divider />

      {/* History tab unchanged */}
      <H3>History Tab (unchanged)</H3>
      <Table
        headers={['Date', 'Mode', 'Result', 'Duration', 'RPO', 'Triggered By']}
        rows={[
          ['Apr 20, 14:32', 'Planned Migration', <StatusBadge label="Succeeded" tone="success" />, '17m 22s', '0s', 'maya@corp'],
          ['Apr 5, 09:15', 'Planned Migration', <StatusBadge label="Succeeded" tone="success" />, '18m 05s', '0s', 'maya@corp'],
          ['Mar 18, 03:14', 'Disaster', <StatusBadge label="PartiallySucceeded" tone="warning" />, '22m 41s', '47s', 'carlos@corp'],
          ['Mar 2, 10:00', 'Planned Migration', <StatusBadge label="Succeeded" tone="success" />, '16m 58s', '0s', 'maya@corp'],
        ]}
        striped
      />

      <Divider />

      <H3>Configuration Tab (expanded)</H3>
      <SectionLabel>
        Plan config, replication health details, and YAML view all live here — keeping the Overview tab focused on lifecycle state.
      </SectionLabel>
      <Grid columns={2} gap={16}>
        <Stack gap={8}>
          <Text weight="semibold" size="small" tone="secondary">Plan Configuration</Text>
          <Table
            headers={['', '']}
            rows={[
              [<Text size="small" tone="secondary">Label Selector</Text>, <Code>app.kubernetes.io/part-of=erp-system</Code>],
              [<Text size="small" tone="secondary">Wave Label</Text>, <Code>soteria.io/wave</Code>],
              [<Text size="small" tone="secondary">Max Concurrent</Text>, '4'],
              [<Text size="small" tone="secondary">Created</Text>, 'Apr 2, 2026'],
            ]}
            framed={false}
          />
        </Stack>
        <Stack gap={8}>
          <Text weight="semibold" size="small" tone="secondary">Replication Health</Text>
          <Table
            headers={['Volume Group', 'Health', 'RPO', 'Last Checked']}
            rows={[
              ['erp-db VG', <StatusBadge label="Healthy" tone="success" />, '8s', '30s ago'],
              ['erp-app VG', <StatusBadge label="Healthy" tone="success" />, '12s', '28s ago'],
              ['erp-web VG', <StatusBadge label="Healthy" tone="success" />, '10s', '32s ago'],
            ]}
            framed={false}
          />
        </Stack>
      </Grid>
      <WireframeBox label="PatternFly CodeBlock — read-only YAML of DRPlan CRD spec + labels/annotations" style={{ padding: 16 }} />
    </Stack>
  );
}

/* ─── Screen 3: Wave Composition Tree ─── */
function WaveTreeWireframe() {
  return (
    <Stack gap={14}>
      <H2>Screen 3 — Wave Composition Tree</H2>
      <SectionLabel>
        Plan Detail "Waves" tab. TreeView of auto-formed wave hierarchy. Collapsed by default; expand to see DRGroup chunks and per-VM detail.
      </SectionLabel>

      <Card collapsible defaultOpen>
        <CardHeader trailing={<Row gap={6}><Text size="small">3 VMs</Text><StatusBadge label="1 Degraded" tone="warning" /></Row>}>
          Wave 1 — Databases
        </CardHeader>
        <CardBody>
          <Stack gap={6}>
            <Text size="small" tone="secondary" weight="semibold">DRGroup chunk 1 (maxConcurrent: 4)</Text>
            <Table
              headers={['VM', 'Storage', 'Consistency', 'Health', 'RPO']}
              rows={[
                [<Text size="small" weight="semibold">erp-db-1</Text>, <Pill size="sm">odf-storage</Pill>, 'VM-level', <StatusBadge label="Healthy" tone="success" />, '8s'],
                [<Text size="small" weight="semibold">erp-db-2</Text>, <Pill size="sm">odf-storage</Pill>, 'VM-level', <StatusBadge label="Healthy" tone="success" />, '8s'],
                [<Text size="small" weight="semibold">erp-db-3</Text>, <Pill size="sm">dell-storage</Pill>, 'VM-level', <StatusBadge label="Degraded" tone="warning" />, '45s'],
              ]}
              rowTone={[undefined, undefined, 'warning']}
              framed={false}
            />
          </Stack>
        </CardBody>
      </Card>

      <Card collapsible defaultOpen>
        <CardHeader trailing={<Row gap={6}><Text size="small">5 VMs</Text><StatusBadge label="All Healthy" tone="success" /></Row>}>
          Wave 2 — App Servers
        </CardHeader>
        <CardBody>
          <Stack gap={10}>
            <Stack gap={4}>
              <Text size="small" tone="secondary" weight="semibold">DRGroup chunk 1 (maxConcurrent: 4)</Text>
              <Table
                headers={['VM', 'Storage', 'Consistency', 'Health', 'RPO']}
                rows={[
                  [<Text size="small" weight="semibold">erp-app-1</Text>, <Pill size="sm">odf-storage</Pill>, <Pill size="sm" tone="info">NS: erp-apps</Pill>, <StatusBadge label="Healthy" tone="success" />, '8s'],
                  [<Text size="small" weight="semibold">erp-app-2</Text>, <Pill size="sm">odf-storage</Pill>, <Pill size="sm" tone="info">NS: erp-apps</Pill>, <StatusBadge label="Healthy" tone="success" />, '8s'],
                  [<Text size="small" weight="semibold">erp-app-3</Text>, <Pill size="sm">odf-storage</Pill>, <Pill size="sm" tone="info">NS: erp-apps</Pill>, <StatusBadge label="Healthy" tone="success" />, '8s'],
                  [<Text size="small" weight="semibold">erp-app-4</Text>, <Pill size="sm">dell-storage</Pill>, 'VM-level', <StatusBadge label="Healthy" tone="success" />, '12s'],
                ]}
                framed={false}
              />
            </Stack>
            <Divider />
            <Stack gap={4}>
              <Text size="small" tone="secondary" weight="semibold">DRGroup chunk 2 (overflow from maxConcurrent)</Text>
              <Table
                headers={['VM', 'Storage', 'Consistency', 'Health', 'RPO']}
                rows={[
                  [<Text size="small" weight="semibold">erp-app-5</Text>, <Pill size="sm">dell-storage</Pill>, 'VM-level', <StatusBadge label="Healthy" tone="success" />, '12s'],
                ]}
                framed={false}
              />
            </Stack>
          </Stack>
        </CardBody>
      </Card>

      <Card collapsible defaultOpen={false}>
        <CardHeader trailing={<Row gap={6}><Text size="small">4 VMs</Text><StatusBadge label="All Healthy" tone="success" /></Row>}>
          Wave 3 — Web Frontends
        </CardHeader>
        <CardBody>
          <Text tone="secondary" size="small">Expand to see DRGroup chunks and per-VM detail</Text>
        </CardBody>
      </Card>

      <Divider />
      <H3>Tree Interaction Notes</H3>
      <Text size="small" tone="secondary">
        Built on PatternFly TreeView. Arrow keys expand/collapse. Namespace-consistent VMs share a "NS:" badge (blue info pill).
        DRGroup chunks reflect maxConcurrentFailovers throttling — chunk 2 holds the overflow VMs that execute after chunk 1 completes.
        Aggregate health on collapsed wave header lets Maya spot problems without expanding.
      </Text>
    </Stack>
  );
}

/* ─── Screen 4: Status Badges & Empty States ─── */
function BadgesAndEmptyStates() {
  return (
    <Stack gap={14}>
      <H2>Screen 4 — Status Badges & Empty States</H2>

      <H3>Phase Badges</H3>
      <SectionLabel>Solid for rest states, outlined+spinner for transient states.</SectionLabel>
      <Row gap={8} wrap>
        <Pill active tone="success" size="md">SteadyState</Pill>
        <Pill active tone="success" size="md">DRedSteadyState</Pill>
        <Pill active tone="info" size="md">FailedOver</Pill>
        <Pill active tone="info" size="md">FailedBack</Pill>
      </Row>
      <Row gap={8} wrap>
        <Pill tone="info" size="md">FailingOver</Pill>
        <Pill tone="info" size="md">Reprotecting</Pill>
        <Pill tone="info" size="md">FailingBack</Pill>
        <Pill tone="info" size="md">Restoring</Pill>
      </Row>
      <Text size="small" tone="tertiary">
        Solid fill = rest state. Outlined = transient (in PatternFly: outlined Label with spinner icon).
      </Text>

      <Divider />

      <H3>Execution Result Badges</H3>
      <Row gap={8} wrap>
        <Pill active tone="success" size="md">Succeeded</Pill>
        <Pill active tone="warning" size="md">PartiallySucceeded</Pill>
        <Pill active tone="deleted" size="md">Failed</Pill>
      </Row>

      <Divider />

      <H3>Replication Health Indicators</H3>
      <SectionLabel>Icon + text + color. Never color alone (UX-DR16).</SectionLabel>
      <Stack gap={4}>
        <Row gap={4} align="center"><StatusBadge label="Healthy" tone="success" /><Text size="small" tone="secondary">RPO 12s · checked 30s ago</Text></Row>
        <Row gap={4} align="center"><StatusBadge label="Degraded" tone="warning" /><Text size="small" tone="secondary">RPO 45s · checked 28s ago</Text></Row>
        <Row gap={4} align="center"><StatusBadge label="Error" tone="danger" /><Text size="small" tone="secondary">RPO unknown · check failed</Text></Row>
        <Row gap={4} align="center"><StatusBadge label="Unknown" tone="neutral" /><Text size="small" tone="secondary">-- · unable to check</Text></Row>
      </Stack>

      <Divider />

      <H3>Empty States</H3>
      <SectionLabel>PatternFly EmptyState — always includes guidance, never a blank page.</SectionLabel>

      <Card>
        <CardHeader>Dashboard — No Plans</CardHeader>
        <CardBody style={{ textAlign: 'center', padding: 32 }}>
          <Stack gap={10} style={{ alignItems: 'center' }}>
            <Text size="small" tone="tertiary" style={{ fontSize: 28 }}>DR</Text>
            <Text weight="semibold">No DR Plans configured</Text>
            <Text tone="secondary" size="small">
              Create your first DR plan by labeling VMs with <Code>app.kubernetes.io/part-of</Code> and <Code>soteria.io/wave</Code>
            </Text>
            <Pill active tone="info">View Documentation</Pill>
          </Stack>
        </CardBody>
      </Card>

      <Card>
        <CardHeader>History Tab — No Executions</CardHeader>
        <CardBody style={{ textAlign: 'center', padding: 24 }}>
          <Stack gap={8} style={{ alignItems: 'center' }}>
            <Text weight="semibold">No executions yet</Text>
            <Text tone="secondary" size="small">Trigger a planned migration to validate your DR plan</Text>
            <Pill active tone="info">Start Planned Migration</Pill>
          </Stack>
        </CardBody>
      </Card>

      <Card>
        <CardHeader>Dashboard — Loading</CardHeader>
        <CardBody>
          <Stack gap={6}>
            <WireframeBox label="Skeleton row — matches table column layout" />
            <WireframeBox label="Skeleton row" />
            <WireframeBox label="Skeleton row" />
          </Stack>
        </CardBody>
      </Card>
    </Stack>
  );
}

/* ─── Screen 5: Reference Tables ─── */
function ReferenceSection() {
  return (
    <Stack gap={14}>
      <H2>Screen 5 — Reference</H2>

      <H3>Alert Banner Logic</H3>
      <Table
        headers={['Condition', 'Banner', 'Persistence', 'User Action']}
        rows={[
          ['Error replication', <Pill active tone="deleted" size="sm">Danger</Pill>, 'Until resolved', 'Filter affected plans'],
          ['Degraded replication', <Pill active tone="warning" size="sm">Warning</Pill>, 'Until resolved', 'Investigate storage'],
          ['Untested 30+ days', <Pill active tone="warning" size="sm">Warning</Pill>, 'Until tested', 'Schedule migration'],
          ['No issues', 'No banner', '--', 'Absence = healthy'],
        ]}
        striped
      />

      <Divider />

      <H3>Feedback Patterns</H3>
      <Table
        headers={['Event', 'Type', 'Duration', 'Content']}
        rows={[
          ['Execution started', 'Toast (info)', '8s', '"Failover started for erp-full-stack"'],
          ['Execution succeeded', 'Toast (success)', '15s', '"12 VMs recovered in 17 min"'],
          ['Execution partial', 'Toast (warning)', 'Persistent', '"1 DRGroup failed — View Details"'],
          ['Replication degraded', 'Banner', 'Until resolved', '"N plans with degraded replication"'],
          ['Replication broken', 'Banner', 'Until resolved', '"N plans running UNPROTECTED"'],
        ]}
        striped
      />
    </Stack>
  );
}

/* ─── Main Canvas ─── */
export default function Epic6Wireframes() {
  return (
    <Stack gap={28} style={{ maxWidth: 960 }}>
      <Stack gap={4}>
        <H1>Epic 6 — OCP Console: Dashboard & Plan Management</H1>
        <Text tone="secondary">
          PatternFly 5 wireframes for Soteria. Maya's 5-second health check. Carlos's 3 AM disaster response.
        </Text>
        <Row gap={12} wrap>
          <Stat value="6" label="Stories" />
          <Stat value="4" label="Custom Components" />
          <Stat value="500+" label="Plans at Scale" />
          <Stat value="5s" label="Status Check" />
        </Row>
      </Stack>

      <Divider />
      <DashboardWireframe />
      <Divider />
      <PlanDetailWireframe />
      <Divider />
      <WaveTreeWireframe />
      <Divider />
      <BadgesAndEmptyStates />
      <Divider />
      <ReferenceSection />

      <Divider />
      <Callout tone="info" title="PatternFly Alignment">
        All wireframes use standard PatternFly 5 components. Custom components (ExecutionGanttChart, ReplicationHealthIndicator, WaveCompositionTree) use PF design tokens exclusively.
      </Callout>
      <Text size="small" tone="tertiary">Epic 6: Stories 6.1–6.6. Epic 7 adds the Execution Monitor, pre-flight confirmation dialogs, and DR operations.</Text>
    </Stack>
  );
}
