---
marp: true
theme: default
paginate: true
html: true
backgroundColor: #1a1a2e
color: #e6e6e6
style: |
  section {
    font-family: 'Inter', 'Helvetica Neue', Arial, sans-serif;
  }
  h1 {
    color: #00d2ff;
    font-size: 2.2em;
    border-bottom: 2px solid #00d2ff;
    padding-bottom: 0.3em;
  }
  h2 {
    color: #00d2ff;
    font-size: 1.6em;
  }
  h3 {
    color: #7fdbff;
    font-size: 1.2em;
  }
  strong {
    color: #00d2ff;
  }
  em {
    color: #a0a0c0;
  }
  code {
    background-color: #2a2a4a;
    color: #7fdbff;
    padding: 0.15em 0.4em;
    border-radius: 4px;
    font-size: 0.9em;
  }
  pre {
    background-color: #12122a;
    border: 1px solid #333366;
    border-radius: 8px;
    padding: 1em;
  }
  pre code {
    background-color: transparent;
    padding: 0;
  }
  table {
    font-size: 0.78em;
    border-collapse: collapse;
    width: 100%;
  }
  th {
    background-color: #2a2a4a;
    color: #00d2ff;
    padding: 0.5em 0.8em;
    border-bottom: 2px solid #00d2ff;
  }
  td {
    padding: 0.4em 0.8em;
    border-bottom: 1px solid #333366;
  }
  a {
    color: #00d2ff;
  }
  blockquote {
    border-left: 4px solid #00d2ff;
    background: #2a2a4a;
    padding: 0.5em 1em;
    border-radius: 0 8px 8px 0;
    font-size: 0.9em;
    margin: 0.8em 0;
  }
  .columns {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1.5em;
  }
  footer {
    color: #555580;
    font-size: 0.6em;
  }
  .diagram-container {
    display: flex;
    justify-content: center;
    margin: 0.5em 0;
  }
---

<!-- _class: lead -->
<!-- _paginate: false -->
<!-- _backgroundColor: #0e0e1a -->

# Soteria

## Kubernetes-Native Disaster Recovery for OpenShift Virtualization

<br>

*Storage-agnostic DR orchestration — from planned migrations to 3 AM disasters.*

<br><br>

**Open Source** · **Storage-Agnostic** · **Human-Triggered** · **Kubernetes-Native**

`soteria.io/v1alpha1` — Apache 2.0

---

# The Problem

### OpenShift Virtualization has no unified disaster recovery orchestration layer.

<div class="columns">
<div>

### What exists today

- Each storage vendor ships **its own** replication tooling
- No vendor-neutral orchestration across ODF, Dell, Pure, NetApp
- No ordered, wave-based VM failover
- No cross-site shared state without a third cluster
- No immutable audit trail for compliance

</div>
<div>

### What organizations need

- **One workflow engine** regardless of storage backend
- Ordered, throttled failover of hundreds of VMs
- Shared DR state visible from **both** datacenters
- Immutable execution records for SOX / ISO 22301
- A single action when a datacenter goes dark

</div>
</div>

<br>

> **DR is the gating blocker** for production adoption of OpenShift Virtualization. The replication layer exists — the orchestration layer does not.

---

# What is Soteria?

A Kubernetes operator that **orchestrates disaster recovery** for KubeVirt virtual machines across two OpenShift clusters.

<div class="columns">
<div>

### Design Principles

- **Human-triggered only** — no auto-failover, no split-brain
- **Storage-agnostic** — pluggable 7-method driver interface
- **Label-driven** — two labels = VM is DR-protected
- **Fail-forward** — partial success is a first-class result
- **Kubernetes-native** — CRDs, RBAC, kubectl, Console

</div>
<div>

### How it works (30 seconds)

1. **DRPlan** selects VMs by label, organizes into waves
2. **Operator triggers** failover (planned or disaster)
3. **Engine executes** wave-by-wave: stop VMs, flip volumes, start VMs
4. **DRExecution** captures immutable audit trail
5. **Reprotect** reverses replication for failback readiness

Everything flows through the **standard Kubernetes API** — `kubectl`, Console, RBAC, audit logging all work natively.

</div>
</div>

---

# The DR Lifecycle

### 4 rest states, 3 execution modes — a symmetric cycle.

<div class="diagram-container">
<svg width="780" height="380" viewBox="0 0 780 380" xmlns="http://www.w3.org/2000/svg">
  <!-- Background subtle grid -->
  <defs>
    <filter id="glow">
      <feGaussianBlur stdDeviation="3" result="coloredBlur"/>
      <feMerge><feMergeNode in="coloredBlur"/><feMergeNode in="SourceGraphic"/></feMerge>
    </filter>
    <marker id="arrow-cyan" markerWidth="10" markerHeight="7" refX="9" refY="3.5" orient="auto">
      <polygon points="0 0, 10 3.5, 0 7" fill="#00d2ff"/>
    </marker>
    <marker id="arrow-green" markerWidth="10" markerHeight="7" refX="9" refY="3.5" orient="auto">
      <polygon points="0 0, 10 3.5, 0 7" fill="#00cc88"/>
    </marker>
  </defs>

  <!-- State boxes -->
  <!-- SteadyState (top-left) -->
  <rect x="30" y="40" width="210" height="70" rx="10" fill="#1a3a2e" stroke="#00cc88" stroke-width="2"/>
  <text x="135" y="68" text-anchor="middle" fill="#00cc88" font-size="15" font-weight="bold" font-family="Inter, sans-serif">SteadyState</text>
  <text x="135" y="90" text-anchor="middle" fill="#a0c0b0" font-size="11" font-family="Inter, sans-serif">Site A active · A → B replication</text>

  <!-- FailedOver (top-right) -->
  <rect x="540" y="40" width="210" height="70" rx="10" fill="#3a1a1a" stroke="#ff6666" stroke-width="2"/>
  <text x="645" y="68" text-anchor="middle" fill="#ff6666" font-size="15" font-weight="bold" font-family="Inter, sans-serif">FailedOver</text>
  <text x="645" y="90" text-anchor="middle" fill="#c0a0a0" font-size="11" font-family="Inter, sans-serif">Site B active · No replication</text>

  <!-- DRedSteadyState (bottom-right) -->
  <rect x="540" y="260" width="210" height="70" rx="10" fill="#1a3a2e" stroke="#00cc88" stroke-width="2"/>
  <text x="645" y="288" text-anchor="middle" fill="#00cc88" font-size="15" font-weight="bold" font-family="Inter, sans-serif">DRedSteadyState</text>
  <text x="645" y="310" text-anchor="middle" fill="#a0c0b0" font-size="11" font-family="Inter, sans-serif">Site B active · B → A replication</text>

  <!-- FailedBack (bottom-left) -->
  <rect x="30" y="260" width="210" height="70" rx="10" fill="#3a1a1a" stroke="#ff6666" stroke-width="2"/>
  <text x="135" y="288" text-anchor="middle" fill="#ff6666" font-size="15" font-weight="bold" font-family="Inter, sans-serif">FailedBack</text>
  <text x="135" y="310" text-anchor="middle" fill="#c0a0a0" font-size="11" font-family="Inter, sans-serif">Site A active · No replication</text>

  <!-- Arrows -->
  <!-- SteadyState → FailedOver (top) -->
  <line x1="240" y1="65" x2="535" y2="65" stroke="#00d2ff" stroke-width="2.5" marker-end="url(#arrow-cyan)" filter="url(#glow)"/>
  <rect x="300" y="28" width="190" height="24" rx="4" fill="#1a1a2e" opacity="0.9"/>
  <text x="395" y="45" text-anchor="middle" fill="#00d2ff" font-size="12" font-weight="bold" font-family="Inter, sans-serif">FAILOVER</text>
  <text x="395" y="23" text-anchor="middle" fill="#7fdbff" font-size="9" font-family="Inter, sans-serif">planned_migration | disaster</text>

  <!-- FailedOver → DRedSteadyState (right) -->
  <line x1="660" y1="115" x2="660" y2="255" stroke="#00cc88" stroke-width="2.5" marker-end="url(#arrow-green)" filter="url(#glow)"/>
  <rect x="672" y="163" width="100" height="24" rx="4" fill="#1a1a2e" opacity="0.9"/>
  <text x="722" y="180" text-anchor="middle" fill="#00cc88" font-size="12" font-weight="bold" font-family="Inter, sans-serif">REPROTECT</text>

  <!-- DRedSteadyState → FailedBack (bottom) -->
  <line x1="535" y1="305" x2="245" y2="305" stroke="#00d2ff" stroke-width="2.5" marker-end="url(#arrow-cyan)" filter="url(#glow)"/>
  <rect x="300" y="316" width="190" height="24" rx="4" fill="#1a1a2e" opacity="0.9"/>
  <text x="395" y="333" text-anchor="middle" fill="#00d2ff" font-size="12" font-weight="bold" font-family="Inter, sans-serif">FAILBACK</text>
  <text x="395" y="352" text-anchor="middle" fill="#7fdbff" font-size="9" font-family="Inter, sans-serif">planned_migration | disaster</text>

  <!-- FailedBack → SteadyState (left) -->
  <line x1="120" y1="255" x2="120" y2="115" stroke="#00cc88" stroke-width="2.5" marker-end="url(#arrow-green)" filter="url(#glow)"/>
  <rect x="8" y="163" width="100" height="24" rx="4" fill="#1a1a2e" opacity="0.9"/>
  <text x="58" y="180" text-anchor="middle" fill="#00cc88" font-size="12" font-weight="bold" font-family="Inter, sans-serif">RESTORE</text>

  <!-- Legend -->
  <rect x="295" y="145" width="190" height="80" rx="8" fill="#12122a" stroke="#333366" stroke-width="1"/>
  <text x="390" y="165" text-anchor="middle" fill="#a0a0c0" font-size="10" font-weight="bold" font-family="Inter, sans-serif">EXECUTION MODES</text>
  <line x1="325" y1="175" x2="375" y2="175" stroke="#00d2ff" stroke-width="2"/>
  <text x="382" y="179" fill="#e6e6e6" font-size="10" font-family="Inter, sans-serif">planned_migration | disaster</text>
  <line x1="325" y1="195" x2="375" y2="195" stroke="#00cc88" stroke-width="2"/>
  <text x="382" y="199" fill="#e6e6e6" font-size="10" font-family="Inter, sans-serif">reprotect</text>
  <rect x="310" y="206" width="10" height="10" rx="2" fill="#1a3a2e" stroke="#00cc88" stroke-width="1"/>
  <text x="328" y="215" fill="#e6e6e6" font-size="10" font-family="Inter, sans-serif">Protected</text>
  <rect x="389" y="206" width="10" height="10" rx="2" fill="#3a1a1a" stroke="#ff6666" stroke-width="1"/>
  <text x="407" y="215" fill="#e6e6e6" font-size="10" font-family="Inter, sans-serif">Unprotected</text>
</svg>
</div>

Rest states persist on the DRPlan. Transition states (`FailingOver`, `Reprotecting`, `FailingBack`, `ReprotectingBack`) are derived at runtime — never stale if the controller restarts mid-operation.

---

# Architecture

### Aggregated API Server + ScyllaDB = cross-site shared state without a third cluster.

<div class="diagram-container">
<svg width="800" height="290" viewBox="0 0 800 290" xmlns="http://www.w3.org/2000/svg">
  <defs>
    <marker id="arr-w" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto">
      <polygon points="0 0, 8 3, 0 6" fill="#e6e6e6"/>
    </marker>
    <marker id="arr-c" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto">
      <polygon points="0 0, 8 3, 0 6" fill="#00d2ff"/>
    </marker>
    <linearGradient id="dc-grad" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="#222244"/>
      <stop offset="100%" stop-color="#1a1a2e"/>
    </linearGradient>
  </defs>

  <!-- DC1 -->
  <rect x="20" y="10" width="340" height="270" rx="12" fill="url(#dc-grad)" stroke="#444477" stroke-width="1.5"/>
  <text x="190" y="36" text-anchor="middle" fill="#7fdbff" font-size="14" font-weight="bold" font-family="Inter, sans-serif">DC1 — Primary Site</text>

  <!-- DC2 -->
  <rect x="440" y="10" width="340" height="270" rx="12" fill="url(#dc-grad)" stroke="#444477" stroke-width="1.5"/>
  <text x="610" y="36" text-anchor="middle" fill="#7fdbff" font-size="14" font-weight="bold" font-family="Inter, sans-serif">DC2 — DR Site</text>

  <!-- DC1: kube-apiserver -->
  <rect x="50" y="52" width="190" height="40" rx="6" fill="#2a2a4a" stroke="#666699" stroke-width="1"/>
  <text x="145" y="77" text-anchor="middle" fill="#e6e6e6" font-size="11" font-family="Inter, sans-serif">kube-apiserver</text>

  <!-- DC1: Soteria -->
  <rect x="50" y="106" width="190" height="48" rx="6" fill="#1a3a4a" stroke="#00d2ff" stroke-width="1.5"/>
  <text x="145" y="126" text-anchor="middle" fill="#00d2ff" font-size="12" font-weight="bold" font-family="Inter, sans-serif">Soteria</text>
  <text x="145" y="143" text-anchor="middle" fill="#a0c0d0" font-size="9" font-family="Inter, sans-serif">API Server + Controller (single binary)</text>

  <!-- DC1: ScyllaDB -->
  <rect x="50" y="176" width="190" height="44" rx="6" fill="#2a3a2a" stroke="#00cc88" stroke-width="1.5"/>
  <text x="145" y="197" text-anchor="middle" fill="#00cc88" font-size="12" font-weight="bold" font-family="Inter, sans-serif">ScyllaDB</text>
  <text x="145" y="212" text-anchor="middle" fill="#a0c0b0" font-size="9" font-family="Inter, sans-serif">RF=2 · LOCAL_ONE · CDC Watch</text>

  <!-- DC1: Storage -->
  <rect x="260" y="106" width="80" height="48" rx="6" fill="#3a2a1a" stroke="#ffaa44" stroke-width="1"/>
  <text x="300" y="127" text-anchor="middle" fill="#ffaa44" font-size="10" font-weight="bold" font-family="Inter, sans-serif">Storage</text>
  <text x="300" y="143" text-anchor="middle" fill="#c0b090" font-size="8" font-family="Inter, sans-serif">ODF / Dell / ...</text>

  <!-- DC2: kube-apiserver -->
  <rect x="560" y="52" width="190" height="40" rx="6" fill="#2a2a4a" stroke="#666699" stroke-width="1"/>
  <text x="655" y="77" text-anchor="middle" fill="#e6e6e6" font-size="11" font-family="Inter, sans-serif">kube-apiserver</text>

  <!-- DC2: Soteria -->
  <rect x="560" y="106" width="190" height="48" rx="6" fill="#1a3a4a" stroke="#00d2ff" stroke-width="1.5"/>
  <text x="655" y="126" text-anchor="middle" fill="#00d2ff" font-size="12" font-weight="bold" font-family="Inter, sans-serif">Soteria</text>
  <text x="655" y="143" text-anchor="middle" fill="#a0c0d0" font-size="9" font-family="Inter, sans-serif">API Server + Controller (single binary)</text>

  <!-- DC2: ScyllaDB -->
  <rect x="560" y="176" width="190" height="44" rx="6" fill="#2a3a2a" stroke="#00cc88" stroke-width="1.5"/>
  <text x="655" y="197" text-anchor="middle" fill="#00cc88" font-size="12" font-weight="bold" font-family="Inter, sans-serif">ScyllaDB</text>
  <text x="655" y="212" text-anchor="middle" fill="#a0c0b0" font-size="9" font-family="Inter, sans-serif">RF=2 · LOCAL_ONE · CDC Watch</text>

  <!-- DC2: Storage -->
  <rect x="460" y="106" width="80" height="48" rx="6" fill="#3a2a1a" stroke="#ffaa44" stroke-width="1"/>
  <text x="500" y="127" text-anchor="middle" fill="#ffaa44" font-size="10" font-weight="bold" font-family="Inter, sans-serif">Storage</text>
  <text x="500" y="143" text-anchor="middle" fill="#c0b090" font-size="8" font-family="Inter, sans-serif">ODF / Dell / ...</text>

  <!-- Arrows: kube-apiserver ↔ Soteria -->
  <line x1="145" y1="92" x2="145" y2="106" stroke="#666699" stroke-width="1.5" marker-end="url(#arr-w)"/>
  <text x="170" y="102" fill="#a0a0c0" font-size="8" font-family="Inter, sans-serif">aggregation</text>
  <line x1="655" y1="92" x2="655" y2="106" stroke="#666699" stroke-width="1.5" marker-end="url(#arr-w)"/>
  <text x="680" y="102" fill="#a0a0c0" font-size="8" font-family="Inter, sans-serif">aggregation</text>

  <!-- Arrows: Soteria → ScyllaDB -->
  <line x1="145" y1="154" x2="145" y2="176" stroke="#00d2ff" stroke-width="1.5" marker-end="url(#arr-c)"/>
  <line x1="655" y1="154" x2="655" y2="176" stroke="#00d2ff" stroke-width="1.5" marker-end="url(#arr-c)"/>

  <!-- Arrows: Soteria → Storage -->
  <line x1="240" y1="130" x2="260" y2="130" stroke="#ffaa44" stroke-width="1" marker-end="url(#arr-w)" stroke-dasharray="4,3"/>
  <line x1="560" y1="130" x2="540" y2="130" stroke="#ffaa44" stroke-width="1" marker-end="url(#arr-w)" stroke-dasharray="4,3"/>

  <!-- Cross-site ScyllaDB replication -->
  <line x1="240" y1="198" x2="560" y2="198" stroke="#00cc88" stroke-width="2.5" stroke-dasharray="8,4"/>
  <rect x="345" y="184" width="110" height="28" rx="5" fill="#1a1a2e" stroke="#00cc88" stroke-width="1"/>
  <text x="400" y="203" text-anchor="middle" fill="#00cc88" font-size="10" font-weight="bold" font-family="Inter, sans-serif">Async Replication</text>

  <!-- Cross-site Storage replication -->
  <line x1="340" y1="130" x2="460" y2="130" stroke="#ffaa44" stroke-width="1.5" stroke-dasharray="6,3"/>
  <text x="400" y="124" text-anchor="middle" fill="#ffaa44" font-size="9" font-family="Inter, sans-serif">Volume Replication</text>

  <!-- Labels bar -->
  <rect x="50" y="238" width="290" height="30" rx="5" fill="#12122a" stroke="#333366" stroke-width="1"/>
  <text x="60" y="257" fill="#a0a0c0" font-size="9" font-family="Inter, sans-serif">
    API: active/active all replicas · Controller: active/passive via Lease
  </text>

  <rect x="460" y="238" width="290" height="30" rx="5" fill="#12122a" stroke="#333366" stroke-width="1"/>
  <text x="470" y="257" fill="#a0a0c0" font-size="9" font-family="Inter, sans-serif">
    LWT for critical transitions · CDC for Kubernetes Watch API
  </text>
</svg>
</div>

**Why ScyllaDB?** Two DCs cannot form a Raft/Paxos quorum — etcd, CockroachDB, and FoundationDB all need a third site. ScyllaDB's eventual consistency with `LOCAL_ONE` + Lightweight Transactions for critical state changes solves this natively.

---

# Key Capabilities

<div class="columns">
<div>

### Pluggable Storage Drivers

7-method `StorageProvider` interface with role-based replication (`NonReplicated` / `Source` / `Target`). Drivers register at compile time, selected implicitly from PVC storage classes.

- **No-op driver** for dev/test/CI from Day 1
- **Conformance suite** validates any implementation
- ODF, Dell, Pure, NetApp — same interface

### Wave Execution & Throttling

VMs organize into **waves** via labels (DB first, then App, then Web). Within each wave, **DRGroups** are chunked to respect `maxConcurrentFailovers`. Waves run sequentially; chunks run concurrently.

</div>
<div>

### Preflight & Safety

Continuous validation before execution: VM discovery, volume group resolution, storage class mapping, wave conflict detection, capacity checks. `Ready` condition on every DRPlan.

### Fail-Forward Error Model

Rollback is impossible when a DC is down. Failed DRGroups are recorded; the engine continues. Operators retry specific groups — no full re-execution needed.

### OCP Console Plugin

DR Dashboard, plan detail with context-aware actions, pre-flight confirmation dialog, live Gantt-chart execution monitor. Built with PatternFly 5.

</div>
</div>

---

<!-- _class: lead -->
<!-- _backgroundColor: #0e0e1a -->

# Get Involved

<br>

<div class="columns">
<div>

### Use Soteria

- Install via **OLM** from OperatorHub
- Define DRPlans with **two labels per VM**
- Run planned migrations to **build confidence**
- Full DR lifecycle: failover, reprotect, failback, restore

### Contribute a Storage Driver

- Implement the **7-method Go interface**
- Run the **conformance test suite**
- Submit a PR with failure-mode documentation

</div>
<div>

### Project Links

- **License:** Apache 2.0
- **API:** `soteria.io/v1alpha1`
- **Stack:** Go · controller-runtime · ScyllaDB · PatternFly 5
- **Tested with:** envtest · Ginkgo/Gomega · testcontainers

### Scale Targets

| Metric | Target |
|---|---|
| Total VMs | 5,000 |
| DRPlans | 100 |
| VMs per plan (avg) | 50 |
| API response | < 2s |
| Live updates | < 5s |

</div>
</div>
