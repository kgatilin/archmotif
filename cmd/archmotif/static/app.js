(function () {
  const fallbackLayoutOptions = [
    { id: "dot", label: "Hierarchy", placement: "server", description: "Graphviz dot layered layout" },
    { id: "structure", label: "Structure", placement: "browser", description: "ArchMotif semantic layout" },
    { id: "force", label: "Force", placement: "browser", description: "Interactive force layout" },
    { id: "radial", label: "Radial", placement: "browser", description: "Radial focus layout" },
  ];
  const initial = new URLSearchParams(window.location.search);
  const sourceGraphID = document.body.dataset.graphId || "";
  const state = {
    graphID: initial.get("graph_id") || sourceGraphID,
    view: "packages",
    id: "",
    detail: "public",
    depth: 1,
    external: false,
    selected: "",
    layout: "dot",
    diffFrom: initial.get("diff_from") || "",
  };
  let layoutPinned = initial.has("layout");
  let layoutOptions = fallbackLayoutOptions.slice();
  let layoutDefaults = { packages: "dot", "diff-packages": "structure", structure: "structure", package: "structure", neighborhood: "dot" };
  if (initial.get("view")) state.view = initial.get("view");
  if (initial.get("id")) state.id = initial.get("id");
  if (initial.get("detail")) state.detail = initial.get("detail");
  if (initial.get("depth")) state.depth = Number(initial.get("depth")) || 1;
  if (initial.get("external")) state.external = initial.get("external") === "true";
  if (initial.get("layout")) state.layout = normalizeLayout(initial.get("layout"));
  const svg = document.getElementById("graph");
  const viewport = document.getElementById("viewport");
  const edgeLayer = document.getElementById("edges");
  const nodeLayer = document.getElementById("nodes");
  const title = document.getElementById("title");
  const subtitle = document.getElementById("subtitle");
  const warn = document.getElementById("warn");
  const selection = document.getElementById("selection");
  const results = document.getElementById("searchResults");
  const targetList = document.getElementById("targetList");
  const diffFromInput = document.getElementById("diffFromInput");
  const diffSummary = document.getElementById("diffSummary");
  let current = { nodes: [], edges: [] };
  let targets = [];
  let positions = new Map();
  let transform = { x: 0, y: 0, k: 1 };
  let draggingNode = null;
  let panning = null;
  let frame = 0;
  let manualLayout = false;
  let cameraDirty = true;

  async function loadLayoutOptions() {
    try {
      const res = await fetch("/api/layouts");
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      if (Array.isArray(data.layouts) && data.layouts.length > 0) {
        layoutOptions = data.layouts;
      }
      if (data.defaults) {
        layoutDefaults = data.defaults;
      }
      if (!layoutPinned) {
        state.layout = preferredLayoutForView(state.view);
      }
    } catch (_) {
      layoutOptions = fallbackLayoutOptions.slice();
    }
    state.layout = normalizeLayout(state.layout);
    renderLayoutControls();
  }

  function renderLayoutControls() {
    const el = document.getElementById("layoutControls");
    el.innerHTML = "";
    layoutOptions.forEach(layout => {
      const b = document.createElement("button");
      b.dataset.layout = layout.id;
      b.className = "layout";
      b.textContent = layout.label || layout.id;
      b.title = layout.description || layout.engine || layout.id;
      b.onclick = () => {
        state.layout = normalizeLayout(layout.id);
        layoutPinned = true;
        positions = new Map();
        cameraDirty = true;
        updateButtons();
        if (isServerLayout(state.layout)) load(); else { renderSVG(); syncURL(); }
      };
      el.appendChild(b);
    });
  }

  function layoutOption(id) {
    return layoutOptions.find(layout => layout.id === id);
  }

  function isServerLayout(id) {
    const layout = layoutOption(id);
    return layout && layout.placement === "server";
  }

  function preferredLayoutForView(view) {
    return layoutDefaults[view] || layoutDefaults.default || (layoutOptions[0] ? layoutOptions[0].id : "dot");
  }

  function setView(view, id) {
    state.view = view;
    state.id = id || "";
    if (!layoutPinned) {
      state.layout = normalizeLayout(preferredLayoutForView(view));
    }
  }

  function qs() {
    const p = new URLSearchParams();
    if (state.graphID) p.set("graph_id", state.graphID);
    p.set("view", state.view);
    p.set("detail", state.detail);
    p.set("depth", String(state.depth));
    p.set("external", String(state.external));
    p.set("layout", state.layout);
    if (state.id) p.set("id", state.id);
    if (state.diffFrom) p.set("diff_from", state.diffFrom);
    return p.toString();
  }
  async function load() {
    try {
      const res = await fetch("/api/graph?" + qs());
      if (!res.ok) throw new Error(await res.text());
      current = await res.json();
      state.selected = current.selected || "";
      positions = new Map();
      cameraDirty = true;
      render();
      renderTargets();
      syncURL();
    } catch (err) {
      warn.hidden = false;
      warn.textContent = (err && err.message ? err.message : String(err || "Graph request failed")).trim();
    }
  }
  function syncURL() {
    history.replaceState(null, "", "/?" + qs());
  }
  function render() {
    title.textContent = current.title || "Graph";
    subtitle.textContent = current.subtitle || "";
    const layoutWarning = current.layout && current.layout.warning ? current.layout.warning : "";
    warn.hidden = !current.truncated && !layoutWarning;
    warn.textContent = current.truncated ? "View was capped for readability; focus a smaller area." : layoutWarning;
    document.getElementById("statNodes").textContent = current.stats.viewNodes;
    document.getElementById("statEdges").textContent = current.stats.viewEdges;
    document.getElementById("statPackages").textContent = current.stats.packages;
    document.getElementById("statContracts").textContent = current.stats.contracts;
    renderCrumbs();
    renderDiff();
    renderSVG();
    selectNode(current.selected);
  }
  function renderCrumbs() {
    const el = document.getElementById("crumbs");
    el.innerHTML = "";
    (current.context || []).forEach(a => {
      const b = document.createElement("button");
      b.textContent = a.label;
      b.onclick = () => { setView(a.view, a.id || ""); load(); };
      el.appendChild(b);
    });
  }
  async function loadTargets() {
    if (!targetList) return;
    try {
      const p = new URLSearchParams();
      if (sourceGraphID) p.set("graph_id", sourceGraphID);
      const res = await fetch("/api/targets" + (p.toString() ? "?" + p.toString() : ""));
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      targets = Array.isArray(data.targets) ? data.targets : [];
      renderTargets();
    } catch (err) {
      targetList.innerHTML = '<div class="empty">' + esc(err.message || "Target list unavailable") + '</div>';
    }
  }
  function renderTargets() {
    if (!targetList) return;
    targetList.innerHTML = "";
    const activeGraphID = state.graphID || sourceGraphID;
    targetList.appendChild(targetButton({
      title: "Actual",
      meta: sourceGraphID || "default graph",
      graphID: sourceGraphID,
      active: !activeGraphID || activeGraphID === sourceGraphID,
    }));
    targets.forEach(t => {
      targetList.appendChild(targetButton({
        title: t.target_id || t.graph_id,
        meta: (t.nodes || 0) + " nodes · " + (t.edges || 0) + " edges",
        graphID: t.graph_id,
        active: activeGraphID === t.graph_id,
      }));
    });
    if (targets.length === 0) {
      const empty = document.createElement("div");
      empty.className = "empty";
      empty.textContent = "No target graphs yet.";
      targetList.appendChild(empty);
    }
  }
  function targetButton(item) {
    const b = document.createElement("button");
    b.className = "target-item" + (item.active ? " active" : "");
    b.innerHTML = "<strong>" + esc(item.title) + "</strong><span>" + esc(item.meta) + "</span>";
    b.onclick = () => switchGraph(item.graphID || sourceGraphID);
    return b;
  }
  function switchGraph(graphID) {
    const next = graphID || sourceGraphID;
    if ((state.graphID || sourceGraphID) === next) return;
    state.graphID = next;
    state.selected = "";
    positions = new Map();
    cameraDirty = true;
    setView("packages", "");
    renderTargets();
    load();
  }
  function nodeRadius(n) {
    let base = n.kind === "package" ? 14 : n.kind === "type" ? 11 : n.kind === "method" ? 8 : 8;
    base += Math.min(18, Math.log2(Math.max(1, n.degree) + 1) * 3.8);
    if (n.contract) base += 4;
    return base;
  }
  function renderSVG() {
    const rect = svg.getBoundingClientRect();
    const width = Math.max(640, rect.width);
    const height = Math.max(420, rect.height);
    const nodeByID = new Map(current.nodes.map(n => [n.id, n]));
    manualLayout = false;
    current.edges.forEach(e => { e._route = null; });
    ensureLayout(width, height);
    current.nodes.forEach(n => {
      let p = positions.get(n.id);
      n._p = p;
      n._r = nodeRadius(n);
    });
    if (cameraDirty) {
      frameCurrentLayout(width, height);
      cameraDirty = false;
    }
    edgeLayer.innerHTML = "";
    current.edges.forEach(e => {
      if (!nodeByID.has(e.from) || !nodeByID.has(e.to)) return;
      const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
      path.setAttribute("class", "edge " + e.kind + (e.diff ? " diff-" + e.diff : ""));
      const t = document.createElementNS("http://www.w3.org/2000/svg", "title");
      t.textContent = e.kind + (e.diff ? " · " + e.diff : "");
      path.appendChild(t);
      e._el = path;
      edgeLayer.appendChild(path);
    });
    nodeLayer.innerHTML = "";
    current.nodes.forEach(n => {
      const g = document.createElementNS("http://www.w3.org/2000/svg", "g");
      g.setAttribute("class", nodeClass(n));
      g.dataset.id = n.id;
      const c = document.createElementNS("http://www.w3.org/2000/svg", "circle");
      c.setAttribute("r", n._r);
      const t = document.createElementNS("http://www.w3.org/2000/svg", "text");
      t.setAttribute("text-anchor", "middle");
      t.setAttribute("dy", n._r + 13);
      t.textContent = n.label;
      const tip = document.createElementNS("http://www.w3.org/2000/svg", "title");
      tip.textContent = n.qname || n.id;
      g.appendChild(c);
      g.appendChild(t);
      g.appendChild(tip);
      g.addEventListener("click", ev => { ev.stopPropagation(); selectNode(n.id); });
      g.addEventListener("dblclick", ev => { ev.stopPropagation(); openNode(n); });
      g.addEventListener("pointerdown", ev => {
        ev.stopPropagation();
        cancelAnimationFrame(frame);
        n._p.vx = 0;
        n._p.vy = 0;
        draggingNode = { node: n, sx: ev.clientX, sy: ev.clientY, ox: n._p.x, oy: n._p.y };
        g.setPointerCapture(ev.pointerId);
      });
      n._el = g;
      nodeLayer.appendChild(g);
    });
    updateScene(nodeByID);
    if (state.layout === "force") tick(width, height, 220);
  }
  function ensureLayout(width, height) {
    const missing = current.nodes.some(n => !positions.has(n.id));
    if (!missing) return;
    positions = new Map();
    if (isServerLayout(state.layout) && hasServerLayout()) return layoutServer(width, height);
    if (state.layout === "structure") return layoutStructure(width, height);
    if (state.layout === "radial") return layoutRadial(width, height);
    return layoutFlow(width, height);
  }
  function hasServerLayout() {
    return current.layout && current.nodes.some(n => Number.isFinite(n.x) && Number.isFinite(n.y));
  }
  function layoutServer(width, height) {
    const points = [];
    current.nodes.forEach(n => {
      if (Number.isFinite(n.x) && Number.isFinite(n.y)) points.push({ x: n.x, y: n.y });
    });
    current.edges.forEach(e => (e.points || []).forEach(p => points.push(p)));
    const box = graphBounds(points);
    const margin = 120;
    const scale = 92;
    const graphWidth = (box.maxX - box.minX) * scale;
    const graphHeight = (box.maxY - box.minY) * scale;
    const offsetX = graphWidth < width - margin * 2 ? (width - graphWidth) / 2 : margin;
    const offsetY = graphHeight < height - margin * 2 ? (height - graphHeight) / 2 : margin;
    const project = p => ({
      x: offsetX + (p.x - box.minX) * scale,
      y: offsetY + (box.maxY - p.y) * scale,
    });
    current.nodes.forEach(n => {
      const p = project(n);
      positions.set(n.id, { x: p.x, y: p.y, fx: p.x, fy: p.y, vx: 0, vy: 0 });
    });
    current.edges.forEach(e => {
      if (e.points && e.points.length > 1) e._route = e.points.map(project);
    });
  }
  function frameCurrentLayout(width, height) {
    const bounds = nodeBounds();
    if (!bounds) {
      transform = { x: 0, y: 0, k: 1 };
      applyTransform();
      return;
    }
    const pad = 96;
    const bw = Math.max(1, bounds.maxX - bounds.minX);
    const bh = Math.max(1, bounds.maxY - bounds.minY);
    const fitW = (width - pad * 2) / bw;
    const fitH = (height - pad * 2) / bh;
    const k = isServerLayout(state.layout)
      ? clamp(Math.min(fitW, fitH, 1), 0.32, 1)
      : clamp(Math.min(fitW, fitH, 1), 0.58, 1);
    const centerX = (bounds.minX + bounds.maxX) / 2;
    const x = width / 2 - centerX * k;
    const y = height / 2 - ((bounds.minY + bounds.maxY) / 2) * k;
    transform = { x, y, k };
    applyTransform();
  }
  function nodeBounds() {
    const points = [];
    current.nodes.forEach(n => {
      if (!n._p) return;
      const r = n._r || 0;
      points.push({ x: n._p.x - r, y: n._p.y - r });
      points.push({ x: n._p.x + r, y: n._p.y + r + 24 });
    });
    if (points.length === 0) return null;
    return graphBounds(points);
  }
  function graphBounds(points) {
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    points.forEach(p => {
      minX = Math.min(minX, p.x); maxX = Math.max(maxX, p.x);
      minY = Math.min(minY, p.y); maxY = Math.max(maxY, p.y);
    });
    if (!Number.isFinite(minX)) return { minX: 0, minY: 0, maxX: 1, maxY: 1 };
    if (minX === maxX) { minX -= 0.5; maxX += 0.5; }
    if (minY === maxY) { minY -= 0.5; maxY += 0.5; }
    return { minX, minY, maxX, maxY };
  }
  function layoutStructure(width, height) {
    const nodes = orderedNodes();
    if (nodes.length === 0) return;
    if (nodes.every(n => n.kind === "package")) return layoutPackageRanks(width, height, nodes);

    const containsParent = new Map();
    const children = new Map();
    current.edges.forEach(e => {
      if (e.kind !== "contains") return;
      if (!children.has(e.from)) children.set(e.from, []);
      children.get(e.from).push(e.to);
      containsParent.set(e.to, e.from);
    });
    const nodeByID = new Map(nodes.map(n => [n.id, n]));
    const packages = nodes.filter(n => n.kind === "package");
    const selectedPackage = packages.find(n => n.id === current.selected) || packages.find(n => current.nodes.some(x => x.package === n.id && x.id === current.selected));
    const sortedPackages = packages.slice().sort((a, b) => {
      if (selectedPackage && a.id === selectedPackage.id) return -1;
      if (selectedPackage && b.id === selectedPackage.id) return 1;
      return (b.degree - a.degree) || String(a.label).localeCompare(String(b.label));
    });
    const laneCount = Math.max(1, sortedPackages.length);
    const laneWidth = Math.max(170, (width - 130) / laneCount);
    const packageX = new Map();
    sortedPackages.forEach((pkg, i) => {
      const x = width / 2 + (i - (laneCount - 1) / 2) * laneWidth;
      packageX.set(pkg.id, clamp(x, 75, width - 75));
      positions.set(pkg.id, { x: packageX.get(pkg.id), y: 82, fx: packageX.get(pkg.id), fy: 82, vx: 0, vy: 0 });
    });
    const fallbackX = id => {
      const pkg = nodeByID.get(id);
      if (pkg && packageX.has(pkg.package)) return packageX.get(pkg.package);
      return width / 2;
    };
    sortedPackages.forEach(pkg => {
      const laneX = packageX.get(pkg.id) || width / 2;
      const laneNodes = nodes.filter(n => n.package === pkg.id && n.id !== pkg.id);
      const types = laneNodes.filter(n => n.kind === "type");
      placeRow(types, laneX, 210, Math.min(laneWidth * 0.9, width - 120), 96, 80);
      const typeX = new Map(types.map(n => [n.id, positions.get(n.id).x]));
      const callable = laneNodes.filter(n => n.kind === "method" || n.kind === "function" || n.kind === "field");
      const byOwner = new Map();
      callable.forEach(n => {
        const parent = nearestStructuralParent(n.id, containsParent, nodeByID);
        const key = parent && typeX.has(parent.id) ? parent.id : pkg.id;
        if (!byOwner.has(key)) byOwner.set(key, []);
        byOwner.get(key).push(n);
      });
      const ownerKeys = Array.from(byOwner.keys()).sort((a, b) => String(a).localeCompare(String(b)));
      const ownerStep = ownerKeys.length > 1 ? Math.max(58, Math.min(90, (height - 430) / (ownerKeys.length - 1))) : 72;
      ownerKeys.forEach((ownerID, ownerIndex) => {
        const ownerItems = byOwner.get(ownerID).sort((a, b) => structuralOrder(a, b));
        const center = typeX.get(ownerID) || laneX;
        const y = 335 + ownerIndex * ownerStep;
        placeRow(ownerItems, center, y, Math.min(300, laneWidth * 0.9), 74, 58);
      });
      const placed = new Set([pkg.id, ...types.map(n => n.id), ...callable.map(n => n.id)]);
      const rest = laneNodes.filter(n => !placed.has(n.id));
      placeRow(rest, laneX, Math.min(height - 70, 520), Math.min(laneWidth * 0.9, width - 120), 72, 64);
    });
    nodes.forEach(n => {
      if (positions.has(n.id)) return;
      const rank = structuralRank(n);
      const y = 82 + rank * 126;
      positions.set(n.id, { x: fallbackX(n.id), y: clamp(y, 70, height - 60), fx: fallbackX(n.id), fy: clamp(y, 70, height - 60), vx: 0, vy: 0 });
    });
  }
  function layoutPackageRanks(width, height, nodes) {
    const nodeByID = new Map(nodes.map(n => [n.id, n]));
    const outgoing = new Map(nodes.map(n => [n.id, []]));
    const incoming = new Map(nodes.map(n => [n.id, []]));
    current.edges.forEach(e => {
      if (!nodeByID.has(e.from) || !nodeByID.has(e.to)) return;
      outgoing.get(e.from).push(e.to);
      incoming.get(e.to).push(e.from);
    });
    const rank = dependencyRanks(nodes, outgoing, incoming);
    const buckets = new Map();
    nodes.forEach(n => {
      const r = rank.get(n.id) || 0;
      if (!buckets.has(r)) buckets.set(r, []);
      buckets.get(r).push(n);
    });
    const maxRank = Math.max(0, ...Array.from(buckets.keys()));
    Array.from(buckets.keys()).sort((a, b) => a - b).forEach(r => {
      const bucket = buckets.get(r).sort((a, b) => (b.degree - a.degree) || String(a.label).localeCompare(String(b.label)));
      const y = 82 + (height - 170) * (maxRank === 0 ? 0.5 : r / maxRank);
      placeRow(bucket, width / 2, y, width - 150, 105, 74);
    });
  }
  function dependencyRanks(nodes, outgoing, incoming) {
    const rank = new Map(nodes.map(n => [n.id, 0]));
    const roots = nodes.filter(n => (incoming.get(n.id) || []).length === 0);
    const queue = (roots.length ? roots : nodes.slice().sort((a, b) => b.degree - a.degree).slice(0, 1)).map(n => n.id);
    const seen = new Set(queue);
    for (let qi = 0; qi < queue.length; qi++) {
      const id = queue[qi];
      (outgoing.get(id) || []).forEach(to => {
        const nextRank = Math.min(8, (rank.get(id) || 0) + 1);
        if (nextRank > (rank.get(to) || 0)) rank.set(to, nextRank);
        if (!seen.has(to)) { seen.add(to); queue.push(to); }
      });
    }
    return rank;
  }
  function nearestStructuralParent(id, containsParent, nodeByID) {
    let parent = containsParent.get(id);
    while (parent) {
      const n = nodeByID.get(parent);
      if (!n) return null;
      if (n.kind === "type" || n.kind === "package") return n;
      parent = containsParent.get(parent);
    }
    return null;
  }
  function placeRow(items, centerX, y, maxWidth, gap, rowGap) {
    if (!items.length) return;
    const cols = Math.max(1, Math.min(items.length, Math.floor(maxWidth / gap) || 1));
    const rows = Math.ceil(items.length / cols);
    items.forEach((n, i) => {
      const row = Math.floor(i / cols);
      const col = i % cols;
      const rowCount = row === rows - 1 ? items.length - row * cols : cols;
      const x = centerX + (col - (rowCount - 1) / 2) * gap;
      const yy = y + (row - (rows - 1) / 2) * rowGap;
      positions.set(n.id, { x, y: yy, fx: x, fy: yy, vx: 0, vy: 0 });
    });
  }
  function layoutFlow(width, height) {
    const nodes = orderedNodes();
    const nodeByID = new Map(nodes.map(n => [n.id, n]));
    const outgoing = new Map(nodes.map(n => [n.id, []]));
    const incoming = new Map(nodes.map(n => [n.id, []]));
    current.edges.forEach(e => {
      if (!nodeByID.has(e.from) || !nodeByID.has(e.to)) return;
      outgoing.get(e.from).push(e.to);
      incoming.get(e.to).push(e.from);
    });
    const rank = new Map();
    const seed = current.nodes.find(n => n.id === current.selected);
    const roots = seed ? [seed] : nodes.filter(n => (incoming.get(n.id) || []).length === 0);
    const start = roots.length > 0 ? roots : nodes.slice(0, 1);
    const queue = [];
    start.forEach(n => { rank.set(n.id, 0); queue.push(n.id); });
    for (let qi = 0; qi < queue.length; qi++) {
      const id = queue[qi];
      const nextRank = Math.min(7, (rank.get(id) || 0) + 1);
      (outgoing.get(id) || []).forEach(to => {
        if (!rank.has(to) || nextRank < rank.get(to)) {
          rank.set(to, nextRank);
          queue.push(to);
        }
      });
    }
    nodes.forEach(n => {
      if (!rank.has(n.id)) {
        rank.set(n.id, Math.min(7, kindOrder(n.kind)));
        return;
      }
      if (rank.get(n.id) === 0) {
        return;
      }
      const incomingCount = (incoming.get(n.id) || []).length;
      const bump = Math.min(3, Math.floor(Math.log2(incomingCount + 1)));
      rank.set(n.id, Math.min(7, rank.get(n.id) + bump));
    });
    const maxRank = Math.max(1, ...Array.from(rank.values()));
    const buckets = new Map();
    nodes.forEach(n => {
      const r = rank.get(n.id);
      if (!buckets.has(r)) buckets.set(r, []);
      buckets.get(r).push(n);
    });
    Array.from(buckets.keys()).sort((a, b) => a - b).forEach(r => {
      const bucket = buckets.get(r).sort((a, b) => (b.degree - a.degree) || String(a.label).localeCompare(String(b.label)));
      const y = 95 + (height - 185) * (r / maxRank);
      const slots = centerOutSlots(bucket.length);
      const usable = Math.max(220, width - 220);
      const step = usable / Math.max(1, bucket.length);
      bucket.forEach((n, i) => {
        const x = width / 2 + slots[i] * step;
        positions.set(n.id, { x, y, fx: x, fy: y, vx: 0, vy: 0 });
      });
    });
  }
  function centerOutSlots(count) {
    const out = [];
    for (let i = 0; i < count; i++) {
      if (i === 0) {
        out.push(0);
      } else {
        const k = Math.ceil(i / 2);
        out.push(i % 2 === 1 ? -k : k);
      }
    }
    return out;
  }
  function layoutRadial(width, height) {
    const nodes = orderedNodes();
    const center = current.nodes.find(n => n.id === current.selected) || nodes.find(n => n.kind === "package") || nodes[0];
    if (!center) return;
    positions.set(center.id, { x: width / 2, y: height / 2, vx: 0, vy: 0 });
    const rings = [
      nodes.filter(n => n.id !== center.id && n.kind === "package"),
      nodes.filter(n => n.id !== center.id && n.kind === "type"),
      nodes.filter(n => n.id !== center.id && (n.kind === "function" || n.kind === "method")),
      nodes.filter(n => n.id !== center.id && n.kind !== "package" && n.kind !== "type" && n.kind !== "function" && n.kind !== "method"),
    ].filter(r => r.length > 0);
    rings.forEach((ring, ri) => {
      const radius = Math.min(width, height) * (0.16 + ri * 0.1);
      ring.forEach((n, i) => {
        const angle = (Math.PI * 2 * i) / Math.max(1, ring.length) - Math.PI / 2;
        positions.set(n.id, { x: width / 2 + Math.cos(angle) * radius, y: height / 2 + Math.sin(angle) * radius, vx: 0, vy: 0 });
      });
    });
  }
  function orderedNodes() {
    return current.nodes.slice().sort((a, b) => {
      const ak = structuralRank(a), bk = structuralRank(b);
      if (ak !== bk) return ak - bk;
      return String(a.label).localeCompare(String(b.label));
    });
  }
  function structuralOrder(a, b) {
    const ak = structuralRank(a), bk = structuralRank(b);
    if (ak !== bk) return ak - bk;
    return String(a.label).localeCompare(String(b.label));
  }
  function structuralRank(n) {
    return ({ package: 0, type: 1, function: 2, method: 2, field: 2 })[n.kind] ?? 3;
  }
  function kindOrder(kind) {
    return ({ package: 0, type: 1, function: 2, method: 3, field: 4 })[kind] ?? 5;
  }
  function nodeClass(n) {
    const parts = ["node", n.kind];
    if (n.foreign) parts.push("foreign");
    if (n.contract) parts.push("contract");
    if (n.diff) parts.push("diff-" + n.diff);
    if (current.nodes.length > 45 && n.kind !== "package" && n.kind !== "type" && !n.contract && n.id !== state.selected) parts.push("quiet-label");
    if (n.id === state.selected) parts.push("selected");
    return parts.join(" ");
  }
  function renderDiff() {
    if (!diffSummary) return;
    if (diffFromInput && document.activeElement !== diffFromInput) diffFromInput.value = state.diffFrom || "";
    const diff = current.diff;
    if (!diff) {
      diffSummary.className = "empty";
      diffSummary.textContent = "No diff overlay.";
      return;
    }
    const s = diff.summary || {};
    const v = diff.visible || {};
    diffSummary.className = "diff-summary";
    diffSummary.innerHTML =
      '<div class="diff-route"><strong>' + esc(diff.from) + '</strong><span>&rarr;</span><strong>' + esc(diff.to) + '</strong></div>' +
      '<div class="diff-grid">' +
      diffMetric("Added", v.nodesAdded || 0, s.nodes_added || 0, "added") +
      diffMetric("Removed", v.nodesRemoved || 0, s.nodes_removed || 0, "removed") +
      diffMetric("Changed", v.nodesChanged || 0, s.nodes_changed || 0, "changed") +
      diffMetric("New edges", v.edgesAdded || 0, s.edges_added || 0, "added") +
      diffMetric("Old edges", v.edgesRemoved || 0, s.edges_removed || 0, "removed") +
      '</div>' +
      (v.removedHidden ? '<div class="diff-note">' + esc(v.removedHidden) + ' removed items are outside this view.</div>' : '');
  }
  function diffMetric(label, visible, total, kind) {
    return '<div class="diff-metric ' + kind + '"><span>' + esc(label) + '</span><strong>' + esc(visible) + '</strong><small>of ' + esc(total) + '</small></div>';
  }
  function openNode(n) {
    if (state.view === "diff-packages" && n.kind === "package") {
      setView("diff-packages", n.id);
      load();
      return;
    }
    if (n.kind === "package") {
      setView("package", n.id);
    } else {
      setView("neighborhood", n.id);
    }
    load();
  }
  function selectNode(id) {
    state.selected = id || "";
    current.nodes.forEach(n => {
      if (n._el) n._el.setAttribute("class", nodeClass(n));
    });
    const n = current.nodes.find(x => x.id === state.selected);
    if (!n) {
      selection.className = "empty";
      selection.textContent = "Click a node. Double-click package nodes to drill into a package; double-click other nodes to focus their neighborhood.";
      return;
    }
    selection.className = "";
    const loc = n.file ? n.file + (n.line ? ":" + n.line : "") : "";
    const changedAttrs = attrsDiffText(n.attrsDiff);
    selection.innerHTML =
      '<div class="kind"><span class="dot" style="background:' + colorFor(n.kind) + '"></span>' + esc(n.kind) + (n.contract ? " · contract" : "") + (n.foreign ? " · external" : "") + (n.diff ? " · " + esc(n.diff) : "") + '</div>' +
      '<h3>' + esc(n.label) + '</h3>' +
      '<div class="kv">' +
      (n.qname ? '<div><span>QName</span><code>' + esc(n.qname) + '</code></div>' : '') +
      (loc ? '<div><span>Source</span><code>' + esc(loc) + '</code></div>' : '') +
      (n.typeKind ? '<div><span>Type kind</span><code>' + esc(n.typeKind) + '</code></div>' : '') +
      (n.role ? '<div><span>Role</span><code>' + esc(n.role) + '</code></div>' : '') +
      (n.diff ? '<div><span>Diff</span><code>' + esc(n.diff) + '</code></div>' : '') +
      (changedAttrs ? '<div><span>Changed attrs</span><code>' + esc(changedAttrs) + '</code></div>' : '') +
      '<div><span>ID</span><code>' + esc(n.id) + '</code></div>' +
      '</div><div class="actions">' +
      '<button class="primary" id="focusBtn">Focus</button>' +
      (state.view === "diff-packages" && n.kind === "package" ? '<button id="publicDiffBtn">Public diff</button>' : '') +
      (n.kind === "package" ? '<button id="openPkgBtn">Open package</button>' : '') +
      '</div>';
    document.getElementById("focusBtn").onclick = () => { setView("neighborhood", n.id); load(); };
    const publicDiff = document.getElementById("publicDiffBtn");
    if (publicDiff) publicDiff.onclick = () => { setView("diff-packages", n.id); load(); };
    const openPkg = document.getElementById("openPkgBtn");
    if (openPkg) openPkg.onclick = () => { setView("package", n.id); load(); };
  }
  function attrsDiffText(diff) {
    if (!diff) return "";
    return Object.keys(diff).sort().slice(0, 8).map(k => {
      const pair = diff[k] || [];
      return k + ": " + valueText(pair[0]) + " -> " + valueText(pair[1]);
    }).join("\n");
  }
  function valueText(v) {
    if (v === null || typeof v === "undefined") return "(none)";
    if (typeof v === "object") return JSON.stringify(v);
    return String(v);
  }
  function tick(width, height, steps) {
    cancelAnimationFrame(frame);
    const nodeByID = new Map(current.nodes.map(n => [n.id, n]));
    let remaining = steps;
    function step() {
      const nodes = current.nodes;
      for (let i = 0; i < nodes.length; i++) {
        const a = nodes[i];
        for (let j = i + 1; j < nodes.length; j++) {
          const b = nodes[j];
          let dx = b._p.x - a._p.x, dy = b._p.y - a._p.y;
          let d2 = dx * dx + dy * dy + 0.01;
          let d = Math.sqrt(d2);
          let min = a._r + b._r + 28;
          let force = Math.min(2.8, 1800 / d2);
          if (d < min) force += (min - d) * 0.035;
          dx /= d; dy /= d;
          a._p.vx -= dx * force; a._p.vy -= dy * force;
          b._p.vx += dx * force; b._p.vy += dy * force;
        }
      }
      current.edges.forEach(e => {
        const a = nodeByID.get(e.from), b = nodeByID.get(e.to);
        if (!a || !b) return;
        let dx = b._p.x - a._p.x, dy = b._p.y - a._p.y;
        let d = Math.sqrt(dx * dx + dy * dy) || 1;
        const target = e.kind === "contains" ? 105 : e.kind === "dependsOn" ? 185 : 135;
        const f = (d - target) * 0.018 * (e.weight || 1);
        dx /= d; dy /= d;
        a._p.vx += dx * f; a._p.vy += dy * f;
        b._p.vx -= dx * f; b._p.vy -= dy * f;
      });
      nodes.forEach(n => {
        n._p.vx += (width / 2 - n._p.x) * 0.006;
        n._p.vy += (height / 2 - n._p.y) * 0.006;
        n._p.vx *= 0.82; n._p.vy *= 0.82;
        if (!draggingNode || draggingNode.node.id !== n.id) {
          n._p.x = clamp(n._p.x + n._p.vx, n._r + 20, width - n._r - 20);
          n._p.y = clamp(n._p.y + n._p.vy, n._r + 50, height - n._r - 30);
        }
      });
      updateScene(nodeByID);
      if (remaining-- > 0) frame = requestAnimationFrame(step);
    }
    step();
  }
  function tickFlow(width, height, steps) {
    cancelAnimationFrame(frame);
    const nodeByID = new Map(current.nodes.map(n => [n.id, n]));
    let remaining = steps;
    function step() {
      const nodes = current.nodes;
      for (let i = 0; i < nodes.length; i++) {
        const a = nodes[i];
        for (let j = i + 1; j < nodes.length; j++) {
          const b = nodes[j];
          let dx = b._p.x - a._p.x, dy = b._p.y - a._p.y;
          let d2 = dx * dx + dy * dy + 0.01;
          let d = Math.sqrt(d2);
          let min = a._r + b._r + 18;
          let force = Math.min(1.8, 1100 / d2);
          if (d < min) force += (min - d) * 0.04;
          dx /= d; dy /= d;
          a._p.vx -= dx * force; a._p.vy -= dy * force;
          b._p.vx += dx * force; b._p.vy += dy * force;
        }
      }
      current.edges.forEach(e => {
        const a = nodeByID.get(e.from), b = nodeByID.get(e.to);
        if (!a || !b) return;
        const dx = b._p.x - a._p.x;
        const pull = e.kind === "dependsOn" || e.kind === "contains" ? 0.004 : 0.002;
        a._p.vx += dx * pull;
        b._p.vx -= dx * pull;
      });
      nodes.forEach(n => {
        if (typeof n._p.fx === "number") {
          n._p.vx += (n._p.fx - n._p.x) * 0.035;
        }
        if (typeof n._p.fy === "number") {
          n._p.vy += (n._p.fy - n._p.y) * 0.08;
        }
        n._p.vx *= 0.72; n._p.vy *= 0.72;
        if (!draggingNode || draggingNode.node.id !== n.id) {
          n._p.x = clamp(n._p.x + n._p.vx, n._r + 20, width - n._r - 20);
          n._p.y = clamp(n._p.y + n._p.vy, n._r + 50, height - n._r - 30);
        }
      });
      updateScene(nodeByID);
      if (remaining-- > 0) frame = requestAnimationFrame(step);
    }
    step();
  }
  function updateScene(nodeByID) {
    current.edges.forEach(e => {
      const a = nodeByID.get(e.from), b = nodeByID.get(e.to);
      if (!a || !b || !e._el) return;
      if (!manualLayout && e._route && e._route.length > 1) {
        e._el.setAttribute("d", routePath(e._route));
      } else {
        e._el.setAttribute("d", "M " + a._p.x.toFixed(1) + " " + a._p.y.toFixed(1) + " L " + b._p.x.toFixed(1) + " " + b._p.y.toFixed(1));
      }
    });
    current.nodes.forEach(n => {
      if (n._el) n._el.setAttribute("transform", "translate(" + n._p.x.toFixed(1) + "," + n._p.y.toFixed(1) + ")");
    });
  }
  function redrawManualPositions() {
    const nodeByID = new Map(current.nodes.map(n => [n.id, n]));
    updateScene(nodeByID);
  }
  function routePath(points) {
    let d = "M " + points[0].x.toFixed(1) + " " + points[0].y.toFixed(1);
    let i = 1;
    for (; i + 2 < points.length; i += 3) {
      d += " C " + points[i].x.toFixed(1) + " " + points[i].y.toFixed(1) + " " +
        points[i + 1].x.toFixed(1) + " " + points[i + 1].y.toFixed(1) + " " +
        points[i + 2].x.toFixed(1) + " " + points[i + 2].y.toFixed(1);
    }
    for (; i < points.length; i++) {
      d += " L " + points[i].x.toFixed(1) + " " + points[i].y.toFixed(1);
    }
    return d;
  }
  svg.addEventListener("pointerdown", ev => {
    panning = { x: ev.clientX, y: ev.clientY, tx: transform.x, ty: transform.y };
    svg.classList.add("panning");
  });
  svg.addEventListener("pointermove", ev => {
    if (draggingNode) {
      const dx = (ev.clientX - draggingNode.sx) / transform.k;
      const dy = (ev.clientY - draggingNode.sy) / transform.k;
      draggingNode.node._p.x = draggingNode.ox + dx;
      draggingNode.node._p.y = draggingNode.oy + dy;
      draggingNode.node._p.vx = 0;
      draggingNode.node._p.vy = 0;
      manualLayout = true;
      redrawManualPositions();
    } else if (panning) {
      transform.x = panning.tx + ev.clientX - panning.x;
      transform.y = panning.ty + ev.clientY - panning.y;
      applyTransform();
    }
  });
  svg.addEventListener("pointerup", () => { draggingNode = null; panning = null; svg.classList.remove("panning"); });
  svg.addEventListener("pointerleave", () => { draggingNode = null; panning = null; svg.classList.remove("panning"); });
  svg.addEventListener("click", () => selectNode(""));
  svg.addEventListener("wheel", ev => {
    ev.preventDefault();
    const rect = svg.getBoundingClientRect();
    const mx = ev.clientX - rect.left;
    const my = ev.clientY - rect.top;
    const wx = (mx - transform.x) / transform.k;
    const wy = (my - transform.y) / transform.k;
    const delta = ev.deltaY > 0 ? 0.9 : 1.1;
    transform.k = clamp(transform.k * delta, 0.24, 3.2);
    transform.x = mx - wx * transform.k;
    transform.y = my - wy * transform.k;
    applyTransform();
  }, { passive: false });
  function applyTransform() {
    viewport.setAttribute("transform", "translate(" + transform.x + "," + transform.y + ") scale(" + transform.k + ")");
  }
  function clamp(v, min, max) { return Math.max(min, Math.min(max, v)); }
  function esc(s) {
    return String(s || "").replace(/[&<>"']/g, ch => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[ch]));
  }
  function colorFor(kind) {
    return ({ package: "#2563eb", type: "#0f766e", function: "#d97706", method: "#ea580c", field: "#64748b" })[kind] || "#7c3aed";
  }
  document.getElementById("overviewBtn").onclick = () => { setView("packages", ""); load(); };
  document.getElementById("diffPackagesBtn").onclick = () => {
    if (!state.diffFrom) {
      warn.hidden = false;
      warn.textContent = "Set a base graph id in Diff first.";
      if (diffFromInput) diffFromInput.focus();
      return;
    }
    setView("diff-packages", "");
    load();
  };
  document.getElementById("structureBtn").onclick = () => { setView("structure", ""); load(); };
  document.getElementById("publicBtn").onclick = () => { state.detail = "public"; updateButtons(); load(); };
  document.getElementById("allBtn").onclick = () => { state.detail = "all"; updateButtons(); load(); };
  document.getElementById("externalBox").onchange = ev => { state.external = ev.target.checked; load(); };
  document.querySelectorAll(".depth").forEach(b => b.onclick = () => { state.depth = Number(b.dataset.depth); updateButtons(); if (state.view === "neighborhood") load(); });
  document.getElementById("applyLayoutBtn").onclick = () => { positions = new Map(); cameraDirty = true; if (isServerLayout(state.layout)) load(); else renderSVG(); };
  document.getElementById("applyDiffBtn").onclick = applyDiffFromInput;
  document.getElementById("clearDiffBtn").onclick = () => {
    state.diffFrom = "";
    if (diffFromInput) diffFromInput.value = "";
    positions = new Map();
    cameraDirty = true;
    load();
  };
  if (diffFromInput) {
    diffFromInput.addEventListener("keydown", ev => {
      if (ev.key === "Enter") applyDiffFromInput();
    });
  }
  function applyDiffFromInput() {
    state.diffFrom = diffFromInput ? diffFromInput.value.trim() : "";
    positions = new Map();
    cameraDirty = true;
    load();
  }
  function updateButtons() {
    document.getElementById("publicBtn").classList.toggle("active", state.detail === "public");
    document.getElementById("allBtn").classList.toggle("active", state.detail === "all");
    document.getElementById("overviewBtn").classList.toggle("active", state.view === "packages");
    document.getElementById("diffPackagesBtn").classList.toggle("active", state.view === "diff-packages");
    document.getElementById("structureBtn").classList.toggle("active", state.view === "structure");
    document.querySelectorAll(".depth").forEach(b => b.classList.toggle("active", Number(b.dataset.depth) === state.depth));
    document.querySelectorAll(".layout").forEach(b => b.classList.toggle("active", b.dataset.layout === state.layout));
  }
  function normalizeLayout(layout) {
    if (layout === "flow") return "dot";
    return layoutOptions.some(item => item.id === layout) ? layout : (layoutOptions[0] ? layoutOptions[0].id : "dot");
  }
  let searchTimer = 0;
  document.getElementById("searchInput").addEventListener("input", ev => {
    clearTimeout(searchTimer);
    const q = ev.target.value.trim();
    if (!q) { results.classList.remove("open"); results.innerHTML = ""; return; }
    searchTimer = setTimeout(async () => {
      const p = new URLSearchParams();
      p.set("q", q);
      if (state.graphID) p.set("graph_id", state.graphID);
      const res = await fetch("/api/search?" + p.toString());
      const data = await res.json();
      results.innerHTML = "";
      data.results.forEach(item => {
        const div = document.createElement("div");
        div.className = "result";
        div.innerHTML = "<strong>" + esc(item.label) + "</strong><span>" + esc(item.kind + " · " + (item.qname || item.id)) + "</span>";
        div.onclick = () => {
          results.classList.remove("open");
          document.getElementById("searchInput").value = "";
          setView(item.view, item.id);
          load();
        };
        results.appendChild(div);
      });
      results.classList.toggle("open", data.results.length > 0);
    }, 120);
  });
  document.addEventListener("click", ev => {
    if (!ev.target.closest(".search")) results.classList.remove("open");
  });
  window.addEventListener("resize", () => renderSVG());
  async function bootstrap() {
    document.getElementById("externalBox").checked = state.external;
    await loadLayoutOptions();
    await loadTargets();
    updateButtons();
    load().catch(err => { title.textContent = "Failed to load graph"; subtitle.textContent = err.message; });
    setInterval(loadTargets, 3000);
  }
  bootstrap();
})();
