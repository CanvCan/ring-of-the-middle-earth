'use strict';

const G = {
  playerID: '', side: '', turn: 0,
  activeSide: 'FREE_PEOPLES', // whose turn it is — synced from server
  units: {}, regions: {}, paths: {},
  selectedUnit: null, pendingOrders: [],
  sse: null,
  routeBuilding: false, routeOrderType: null,
  builtPaths: [], builtNodes: [], lastNode: null,
  pickingTarget: null,
  _highlightedNodes: [], // nodes highlighted for target-picking
  _clearHighlightedNodes() {
    this._highlightedNodes.forEach(nid => unhighlightNode(nid));
    this._highlightedNodes = [];
  },
  // Returns true if the given unit already has a pending order this turn
  isUnitOrdered(uid) { return this.pendingOrders.some(o => o.unitId === uid); },
};

// ── SVG node coordinates (matches original SVG viewBox 0 0 1400 1000, translate(0,50)) ──
const NODES = {
  'the-shire':    {x:100, y:250, label:'The Shire',    terrain:'plains',    startCtrl:'FREE_PEOPLES'},
  'bree':         {x:250, y:250, label:'Bree',          terrain:'plains',    startCtrl:'NEUTRAL'},
  'tharbad':      {x:250, y:400, label:'Tharbad',       terrain:'swamp',     startCtrl:'NEUTRAL'},
  'weathertop':   {x:400, y:150, label:'Weathertop',    terrain:'mountains', startCtrl:'NEUTRAL'},
  'rivendell':    {x:550, y:150, label:'Rivendell',     terrain:'mountains', startCtrl:'FREE_PEOPLES'},
  'fangorn':      {x:500, y:450, label:'Fangorn',       terrain:'forest',    startCtrl:'FREE_PEOPLES'},
  'fords-of-isen':{x:350, y:550, label:'Fords of Isen', terrain:'plains',    startCtrl:'NEUTRAL'},
  'rohan-plains': {x:650, y:500, label:'Rohan Plains',  terrain:'plains',    startCtrl:'FREE_PEOPLES'},
  'moria':        {x:600, y:300, label:'Moria',         terrain:'mountains', startCtrl:'NEUTRAL'},
  'helms-deep':   {x:500, y:700, label:"Helm's Deep",   terrain:'fortress',  startCtrl:'FREE_PEOPLES'},
  'isengard':     {x:350, y:650, label:'Isengard',      terrain:'fortress',  startCtrl:'SHADOW'},
  'edoras':       {x:650, y:650, label:'Edoras',        terrain:'plains',    startCtrl:'FREE_PEOPLES'},
  'lothlorien':   {x:700, y:300, label:'Lothlórien',    terrain:'forest',    startCtrl:'FREE_PEOPLES'},
  'dead-marshes': {x:1000,y:250, label:'Dead Marshes',  terrain:'swamp',     startCtrl:'NEUTRAL'},
  'emyn-muil':    {x:850, y:350, label:'Emyn Muil',     terrain:'mountains', startCtrl:'NEUTRAL'},
  'minas-tirith': {x:850, y:650, label:'Minas Tirith',  terrain:'fortress',  startCtrl:'FREE_PEOPLES'},
  'ithilien':     {x:1000,y:450, label:'Ithilien',      terrain:'forest',    startCtrl:'NEUTRAL'},
  'osgiliath':    {x:1000,y:650, label:'Osgiliath',     terrain:'plains',    startCtrl:'NEUTRAL'},
  'minas-morgul': {x:1150,y:650, label:'Minas Morgul',  terrain:'fortress',  startCtrl:'SHADOW'},
  'cirith-ungol': {x:1150,y:450, label:'Cirith Ungol',  terrain:'mountains', startCtrl:'SHADOW'},
  'mordor':       {x:1150,y:250, label:'Mordor',        terrain:'volcanic',  startCtrl:'SHADOW'},
  'mount-doom':   {x:1300,y:350, label:'Mount Doom',    terrain:'volcanic',  startCtrl:'SHADOW'},
};

const EDGES = [
  {id:'shire-to-bree',              from:'the-shire',    to:'bree',          cost:1},
  {id:'bree-to-weathertop',         from:'bree',         to:'weathertop',    cost:1},
  {id:'bree-to-rivendell',          from:'bree',         to:'rivendell',     cost:2},
  {id:'bree-to-tharbad',            from:'bree',         to:'tharbad',       cost:1},
  {id:'shire-to-tharbad',           from:'the-shire',    to:'tharbad',       cost:2},
  {id:'weathertop-to-rivendell',    from:'weathertop',   to:'rivendell',     cost:1},
  {id:'rivendell-to-moria',         from:'rivendell',    to:'moria',         cost:2},
  {id:'rivendell-to-lothlorien',    from:'rivendell',    to:'lothlorien',    cost:2},
  {id:'moria-to-lothlorien',        from:'moria',        to:'lothlorien',    cost:1},
  {id:'lothlorien-to-emyn-muil',    from:'lothlorien',   to:'emyn-muil',     cost:1},
  {id:'lothlorien-to-rohan-plains', from:'lothlorien',   to:'rohan-plains',  cost:1},
  {id:'rohan-plains-to-fangorn',    from:'rohan-plains', to:'fangorn',       cost:1},
  {id:'rohan-plains-to-edoras',     from:'rohan-plains', to:'edoras',        cost:1},
  {id:'rohan-plains-to-minas-tirith',from:'rohan-plains',to:'minas-tirith',  cost:2},
  {id:'fangorn-to-isengard',        from:'fangorn',      to:'isengard',      cost:1},
  {id:'isengard-to-rohan-plains',   from:'isengard',     to:'rohan-plains',  cost:1},
  {id:'tharbad-to-fords-of-isen',   from:'tharbad',      to:'fords-of-isen', cost:2},
  {id:'fords-of-isen-to-isengard',  from:'fords-of-isen',to:'isengard',      cost:1},
  {id:'fords-of-isen-to-helms-deep',from:'fords-of-isen',to:'helms-deep',    cost:1},
  {id:'fords-of-isen-to-edoras',    from:'fords-of-isen',to:'edoras',        cost:1},
  {id:'edoras-to-helms-deep',       from:'edoras',       to:'helms-deep',    cost:1},
  {id:'helms-deep-to-isengard',     from:'helms-deep',   to:'isengard',      cost:1},
  {id:'edoras-to-minas-tirith',     from:'edoras',       to:'minas-tirith',  cost:2},
  {id:'emyn-muil-to-dead-marshes',  from:'emyn-muil',    to:'dead-marshes',  cost:1},
  {id:'emyn-muil-to-ithilien',      from:'emyn-muil',    to:'ithilien',      cost:2},
  {id:'dead-marshes-to-ithilien',   from:'dead-marshes', to:'ithilien',      cost:1},
  {id:'dead-marshes-to-mordor',     from:'dead-marshes', to:'mordor',        cost:2},
  {id:'ithilien-to-minas-tirith',   from:'ithilien',     to:'minas-tirith',  cost:1},
  {id:'ithilien-to-osgiliath',      from:'ithilien',     to:'osgiliath',     cost:1},
  {id:'ithilien-to-cirith-ungol',   from:'ithilien',     to:'cirith-ungol',  cost:2},
  {id:'minas-tirith-to-osgiliath',  from:'minas-tirith', to:'osgiliath',     cost:1},
  {id:'osgiliath-to-minas-morgul',  from:'osgiliath',    to:'minas-morgul',  cost:1},
  {id:'minas-morgul-to-cirith-ungol',from:'minas-morgul',to:'cirith-ungol',  cost:1},
  {id:'minas-morgul-to-mordor',     from:'minas-morgul', to:'mordor',        cost:1},
  {id:'cirith-ungol-to-mordor',     from:'cirith-ungol', to:'mordor',        cost:1},
  {id:'cirith-ungol-to-mount-doom', from:'cirith-ungol', to:'mount-doom',    cost:2},
  {id:'mordor-to-mount-doom',       from:'mordor',       to:'mount-doom',    cost:1},
];

const CTRL_COLOR = {FREE_PEOPLES:'#2d5a8e', SHADOW:'#8b1a1a', NEUTRAL:'#7a6a50'};
const ADJ = {};
EDGES.forEach(e => {
  (ADJ[e.from]=ADJ[e.from]||[]).push({id:e.id,nb:e.to,cost:e.cost});
  (ADJ[e.to]  =ADJ[e.to]  ||[]).push({id:e.id,nb:e.from,cost:e.cost});
});
const NS = 'http://www.w3.org/2000/svg';

// ── Join ───────────────────────────────────────────────────────────────────
function joinGame() {
  const pid   = document.getElementById('pid').value.trim();
  const pside = document.getElementById('pside').value;
  if (!pid) { alert('Please enter a Player ID'); return; }
  G.playerID = pid; G.side = pside;
  document.getElementById('login').style.display = 'none';
  document.getElementById('game').classList.add('on');
  const sLabel = pside==='FREE_PEOPLES' ? '⚪ Light Side' : '⚫ Dark Side';
  const el = document.getElementById('hdr-player');
  el.textContent = pid + ' — ' + sLabel;
  el.style.color = pside==='FREE_PEOPLES' ? '#3867d6' : '#eb3b5a';
  if (pside==='FREE_PEOPLES') document.getElementById('btn-routes').style.display='inline-block';
  else document.getElementById('btn-intercept').style.display='inline-block';
  G.activeSide = 'FREE_PEOPLES'; // light always goes first
  drawStaticMap();
  connectSSE();
  fetchState();
  updateTurnIndicator();
  setInterval(fetchState, 4000);
}

// ── SSE ────────────────────────────────────────────────────────────────────
function connectSSE() {
  G.sse = new EventSource('/api/events?playerId='+G.playerID+'&side='+G.side);
  G.sse.onopen  = ()=>setConn('● Connected','#26de81');
  G.sse.onerror = ()=>setConn('● Reconnecting…','#f9ca24');
  G.sse.onmessage = e => { try { handleEvent(JSON.parse(e.data)); } catch(_){} };
}
function setConn(t,c){const e=document.getElementById('hdr-conn');e.textContent=t;e.style.color=c;}

function handleEvent(ev) {
  switch(ev.type||ev.eventType||'') {
    case 'WorldStateSnapshot': applySnap(ev); break;

    case 'PhaseChanged':
      G.activeSide = ev.activeSide;
      updateTurnIndicator();
      if (ev.activeSide === G.side) {
        logEntry('phase', '⚔', 'Your phase begins — give orders and Dispatch', 'hi');
        startTurnTimer(0);
      } else {
        const opp = ev.activeSide==='FREE_PEOPLES'?'⚪ Light Side':'⚫ Dark Side';
        logEntry('phase', '⌛', opp + '\'s phase — waiting for opponent');
      }
      break;

    case 'TurnStarted':
      logEntry('turn', '🔔', 'Turn ' + (ev.turn||G.turn) + ' has begun');
      startTurnTimer(0);
      break;

    case 'UnitMoved': {
      // Immediately apply new position so the map updates without waiting for an
      // async REST round-trip. This is the primary fix for the stale-position bug.
      const u = G.units[ev.unitId];
      if (u) {
        u.currentRegion = ev.to;
        renderMapUnits();
        renderUnitList();
      }
      const fromLabel = (NODES[ev.from]||{label:ev.from||'?'}).label;
      const toLabel   = (NODES[ev.to]  ||{label:ev.to  ||'?'}).label;
      const cls = ((u&&u.class)||'Unit') + ((u&&u.side)==='SHADOW'?' [Dark]':' [Light]');
      logEntry('move',
        (u&&u.side)==='SHADOW'?'🦅':'🧝',
        ev.unitId + ' (' + cls + '): ' + fromLabel + ' → ' + toLabel);
      // Debounced full-state refresh for strength/status/cooldown updates
      fetchStateDebounced();
      break;
    }

    case 'RingBearerMoved': {
      // Light Side only: immediately show new Ring Bearer position on the map.
      if (G.side === 'FREE_PEOPLES' && ev.trueRegion) {
        const rb = G.units['ring-bearer'] || Object.values(G.units).find(u => u.class === 'RingBearer');
        if (rb) {
          rb.currentRegion = ev.trueRegion;
          renderMapUnits();
          renderUnitList();
        }
      }
      const toLabel = (NODES[ev.trueRegion]||{label:ev.trueRegion||'?'}).label;
      logEntry('rb', '💍', 'Ring Bearer advanced to ' + toLabel, 'hi');
      // Debounced full refresh to sync remaining state (exposure, turn counter, etc.)
      fetchStateDebounced();
      break;
    }

    case 'PathStatusChanged': {
      const pathLabel = ev.pathId.replace(/-/g,' ');
      const icons = {BLOCKED:'🚫', TEMPORARILY_OPEN:'✨', THREATENED:'⚠', OPEN:'✓'};
      const cls   = {BLOCKED:'danger', TEMPORARILY_OPEN:'hi', THREATENED:'', OPEN:''}[ev.newStatus] || '';
      const icon  = icons[ev.newStatus] || '•';
      let detail  = 'Road [' + pathLabel + '] → ' + ev.newStatus;
      if (ev.surveillanceLevel > 0) detail += ' · Surveillance: ' + ev.surveillanceLevel;
      if (ev.tempOpenTurns   > 0) detail += ' · Open for ' + ev.tempOpenTurns + ' turns';
      logEntry('path', icon, detail, cls);
      fetchStateDebounced();
      break;
    }

    case 'PathCorrupted': {
      const pathLabel = ev.pathId.replace(/-/g,' ');
      logEntry('path', '💀', 'Road [' + pathLabel + '] CORRUPTED permanently — Surveillance: 3', 'danger');
      fetchStateDebounced();
      break;
    }

    case 'RingBearerDetected': {
      const rLabel = (NODES[ev.regionId]||{label:ev.regionId}).label;
      logEntry('detect', '👁', 'RING BEARER detected near ' + rLabel + '! (Nazgûl senses the One Ring)', 'hi');
      break;
    }

    case 'RingBearerSpotted': {
      const pathLabel = ev.pathId.replace(/-/g,' ');
      logEntry('detect', '👁', 'Ring Bearer spotted crossing [' + pathLabel + '] — surveillance triggered', 'hi');
      break;
    }

    case 'BattleResolved': {
      const rLabel = (NODES[ev.regionId]||{label:ev.regionId}).label;
      if (ev.attackerWon) {
        logEntry('battle', '⚔', 'BATTLE at ' + rLabel + ' — ATTACKER WON, defenders pushed back', 'hi');
      } else {
        logEntry('battle', '🛡', 'BATTLE at ' + rLabel + ' — DEFENDER HELD, attackers take losses', 'danger');
      }
      fetchStateDebounced();
      break;
    }

    case 'RouteBlocked': {
      const pathLabel = ev.pathId.replace(/-/g,' ');
      logEntry('block', '🚫', ev.unitId + '\'s route blocked — path [' + pathLabel + '] is sealed', 'danger');
      break;
    }

    case 'RouteCompromised': {
      logEntry('block', '⚠', ev.unitId + '\'s route is compromised — replan needed', 'danger');
      break;
    }

    case 'RegionControlChanged': {
      const rLabel = (NODES[ev.regionId]||{label:ev.regionId}).label;
      const side = ev.newControl==='FREE_PEOPLES'?'⚪ Free Peoples':ev.newControl==='SHADOW'?'⚫ Shadow':'Neutral';
      logEntry('region', '🏴', rLabel + ' now under ' + side + ' control', 'hi');
      fetchStateDebounced();
      break;
    }

    case 'MaiaAbilityUsed': {
      const pathLabel = (ev.pathId||'').replace(/-/g,' ');
      const evType = ev.eventType||ev.result||'';
      if (evType === 'PATH_OPENED') {
        logEntry('maia', '✨', ev.unitId + ' opened road [' + pathLabel + '] — TEMPORARILY_OPEN for 2 turns', 'hi');
      } else if (evType === 'PATH_CORRUPTED') {
        logEntry('maia', '💀', ev.unitId + ' corrupted road [' + pathLabel + '] — permanent Surveillance 3', 'danger');
      } else {
        logEntry('maia', '✨', ev.unitId + ' used Maia Ability on [' + pathLabel + ']');
      }
      fetchStateDebounced();
      break;
    }

    case 'GameOver': showOver(ev); break;
  }
}

// ── State ──────────────────────────────────────────────────────────────────
async function fetchState() {
  try {
    const r = await fetch('/api/game/state?playerId='+G.playerID+'&side='+G.side);
    if (!r.ok) { document.getElementById('hdr-status').textContent='API '+r.status; return; }
    document.getElementById('hdr-status').textContent='';
    applySnap(await r.json());
  } catch(e) { document.getElementById('hdr-status').textContent='Unreachable'; }
}

// Debounced fetchState — coalesces rapid concurrent calls into one per 300 ms window.
// Prevents multiple in-flight GET /api/game/state requests racing each other after a
// turn-end burst of SSE events. (B9 — no goroutine leaks analogue in JS: no extra
// promises left dangling.)
let _fetchDebounceTimer = null;
function fetchStateDebounced() {
  if (_fetchDebounceTimer !== null) return; // already scheduled
  _fetchDebounceTimer = setTimeout(() => {
    _fetchDebounceTimer = null;
    fetchState();
  }, 300);
}

function applySnap(d) {
  if (d.turn!=null) {
    const turnChanged = d.turn !== G.turn && d.turn > 0;
    if (turnChanged) {
      // New round — clear pending orders (turn ended)
      G.pendingOrders=[];
      updateOrderCount();
    }
    G.turn=d.turn;
    document.getElementById('hdr-turn').textContent='Turn '+G.turn;
    if (d.turnStartedAt) {
      const elapsed = Math.floor((Date.now() - d.turnStartedAt) / 1000);
      startTurnTimer(Math.max(0, Math.min(elapsed, 59)));
    } else if (turnChanged) {
      startTurnTimer(0);
    }
  }
  if (d.activeSide != null && d.activeSide !== G.activeSide) {
    G.activeSide = d.activeSide;
    updateTurnIndicator();
  } else if (d.activeSide != null) {
    G.activeSide = d.activeSide;
    // Refresh button states without showing banner again
    const myTurn = G.activeSide === G.side;
    document.getElementById('sbtn').disabled = (!myTurn || G.pendingOrders.length === 0);
    const hdrStatus = document.getElementById('hdr-status');
    if (myTurn) { hdrStatus.textContent='⚔ Your Turn'; hdrStatus.style.color='#26de81'; }
    else { const opp=G.activeSide==='FREE_PEOPLES'?'⚪ Light Side':'⚫ Dark Side'; hdrStatus.textContent='⌛ '+opp+'\'s Turn'; hdrStatus.style.color='#c9921a'; }
  }
  if (d.units) {
    G.units={};
    d.units.forEach(u=>{G.units[u.id]=u;});
    renderUnitList();
    renderMapUnits();
  }
  if (d.regions) { G.regions={}; d.regions.forEach(r=>{G.regions[r.id]=r;}); updateNodeColors(); }
  if (d.paths)   { G.paths={};   d.paths.forEach(p=>{G.paths[p.id]=p;});     updateEdges(); }
}

// ── Draw static map ────────────────────────────────────────────────────────
function drawStaticMap() {
  const eg = document.getElementById('edges-g');
  const ng = document.getElementById('nodes-g');

  EDGES.forEach(e => {
    const n1=NODES[e.from], n2=NODES[e.to];
    const el = mksvg('line',{id:'e-'+e.id,x1:n1.x,y1:n1.y,x2:n2.x,y2:n2.y,'stroke-linecap':'round'});
    el.setAttribute('stroke', e.cost===2?'#555555':'#888888');
    el.setAttribute('stroke-width', e.cost===2?'4':'3');
    if (e.cost===2) el.setAttribute('stroke-dasharray','8,8');
    eg.appendChild(el);
  });

  Object.entries(NODES).forEach(([id,n]) => {
    const g = mksvg('g',{id:'n-'+id, transform:`translate(${n.x},${n.y})`, cursor:'pointer'});
    // Label
    const lw = Math.max(n.label.length*9+28, 96);
    g.appendChild(mksvg('rect',{x:-lw/2,y:-62,width:lw,height:32,rx:8,fill:'#f0e6cc',stroke:'#c9921a','stroke-width':1.5,'filter':'url(#nshadow)'}));
    const lbl = mksvg('text',{x:0,y:-40,'font-size':15,'font-weight':'bold','text-anchor':'middle',fill:'#1a1209','pointer-events':'none','font-family':'Cinzel,Georgia,serif'});
    lbl.textContent = n.label;
    g.appendChild(lbl);
    // Node circle — r=26
    const c = mksvg('circle',{r:26,fill:CTRL_COLOR[n.startCtrl]||'#7a6a50',stroke:'rgba(201,146,26,0.55)','stroke-width':2.5,'filter':'url(#nshadow)'});
    c.id = 'nc-'+id;
    g.appendChild(c);
    // Terrain icon
    g.appendChild(mksvg('use',{href:'#icon-'+n.terrain}));
    // RB ring indicator
    const rb = mksvg('text',{x:22,y:-22,'font-size':18,'pointer-events':'none',id:'rbr-'+id});
    rb.textContent='💍'; rb.style.display='none';
    g.appendChild(rb);

    g.addEventListener('click',      ()  => onNodeClick(id));
    g.addEventListener('mouseenter', ev  => showTip(ev,id));
    g.addEventListener('mouseleave', ()  => hideTip());
    ng.appendChild(g);
  });
}

function mksvg(tag,attrs) {
  const el=document.createElementNS(NS,tag);
  Object.entries(attrs).forEach(([k,v])=>el.setAttribute(k,v));
  return el;
}

// ── Map updates ────────────────────────────────────────────────────────────
function updateNodeColors() {
  Object.entries(G.regions).forEach(([id,r]) => {
    const c=document.getElementById('nc-'+id); if (!c) return;
    c.setAttribute('fill', r.control==='FREE_PEOPLES'?'#2d5a8e':r.control==='SHADOW'?'#8b1a1a':'#7a6a50');
    c.setAttribute('stroke-width', r.fortified?5:3);
    c.setAttribute('stroke', r.fortified?'#f9ca24':'#ffffff');
  });
}

function updateEdges() {
  EDGES.forEach(e => {
    const el=document.getElementById('e-'+e.id); if (!el) return;
    const p=G.paths[e.id];
    if (!p) return;
    el.classList.remove('path-blocked','path-temp-open');
    if (p.status==='BLOCKED') {
      el.setAttribute('stroke','#8b1a1a'); el.setAttribute('stroke-dasharray','6,3'); el.setAttribute('stroke-width','4');
      el.classList.add('path-blocked');
    } else if (p.status==='THREATENED') {
      el.setAttribute('stroke','#c9921a'); el.setAttribute('stroke-dasharray','5,3'); el.setAttribute('stroke-width','3');
    } else if (p.status==='TEMPORARILY_OPEN') {
      el.setAttribute('stroke','#26de81'); el.setAttribute('stroke-dasharray','8,4'); el.setAttribute('stroke-width','4');
      el.classList.add('path-temp-open');
    } else {
      el.setAttribute('stroke',e.cost===2?'#6a5535':'#8a7050');
      el.setAttribute('stroke-width',e.cost===2?'4':'3');
      if (e.cost===2) el.setAttribute('stroke-dasharray','8,8'); else el.removeAttribute('stroke-dasharray');
    }
  });
}

function renderMapUnits() {
  const ug=document.getElementById('units-g');
  while(ug.firstChild) ug.removeChild(ug.firstChild);
  Object.keys(NODES).forEach(id=>{const e=document.getElementById('rbr-'+id);if(e)e.style.display='none';});

  // 2-char abbreviation: initials of dash-separated words
  function abbrev(id) {
    const parts=id.split('-');
    if(parts.length>=2) return (parts[0][0]+parts[1][0]).toUpperCase();
    return id.slice(0,2).toUpperCase();
  }

  const byRegion={};
  Object.values(G.units).forEach(u=>{
    if(!u.currentRegion) return;
    (byRegion[u.currentRegion]=byRegion[u.currentRegion]||[]).push(u);
  });

  const DOT_R=19, SPACING=42;

  Object.entries(byRegion).forEach(([rid,us])=>{
    const n=NODES[rid]; if(!n) return;
    const rb=us.find(u=>u.class==='RingBearer');
    if(rb&&rb.currentRegion){ const e=document.getElementById('rbr-'+rid); if(e) e.style.display='block'; }
    const nonRB=us.filter(u=>u.class!=='RingBearer');
    if(nonRB.length===0) return;

    // Horizontal row below the node circle (r=26, so start at +42)
    const totalW=(nonRB.length-1)*SPACING;
    const startX=n.x-totalW/2;
    const baseY=n.y+44;

    nonRB.forEach((u,i)=>{
      const cx=startX+i*SPACING;
      const cy=baseY;
      const isShadow=u.side==='SHADOW';
      const isOwn=u.side===G.side;
      // Outer glow ring for own units
      if(isOwn){
        const glow=mksvg('circle',{cx,cy,r:DOT_R+4,fill:'none',stroke:isShadow?'rgba(192,32,32,0.35)':'rgba(75,123,236,0.35)','stroke-width':3,'pointer-events':'none'});
        ug.appendChild(glow);
      }
      const col=isShadow?'#8b1a1a':'#2d5a8e';
      const dot=mksvg('circle',{cx,cy,r:DOT_R,
        fill:col,
        stroke:isOwn?'rgba(232,184,75,0.8)':'rgba(255,255,255,0.25)',
        'stroke-width': isOwn?'2.5':'1.5',
        cursor:isOwn?'pointer':'default'});
      if(isOwn){
        dot.addEventListener('click',ev=>{ev.stopPropagation();onUnitDotClick(rid,u.id);});
      }
      dot.addEventListener('mouseenter',ev=>{
        const t=document.getElementById('tip');
        t.innerHTML=`<b>${u.id}</b><div class="tr">${u.class} · ${isShadow?'Dark':'Light'}</div><div class="tr">STR ${u.strength} · ${u.status}${u.cooldown>0?' · CD:'+u.cooldown:''}</div>`;
        t.style.left=(ev.clientX+12)+'px'; t.style.top=(ev.clientY-8)+'px'; t.style.display='block';
      });
      dot.addEventListener('mouseleave',hideTip);
      const lbl=mksvg('text',{x:cx,y:cy+5,'text-anchor':'middle','font-size':13,'font-weight':800,fill:'#fff','pointer-events':'none','font-family':'Cinzel,serif'});
      lbl.textContent=abbrev(u.id);
      ug.appendChild(dot); ug.appendChild(lbl);
    });
  });
}

// ── Node click dispatcher ──────────────────────────────────────────────────
function onNodeClick(nid) {
  // 1. Route building mode
  if (G.routeBuilding) { addRouteNode(nid); return; }

  // 2. Target picking mode (for ATTACK, DEPLOY, etc.)
  if (G.pickingTarget) {
    G.pickingTarget.resolve(nid); // resolve handles validation + clearing G.pickingTarget
    return;
  }

  // 3. Show units at this node — popup to pick one
  const unitsHere = Object.values(G.units).filter(u=>u.currentRegion===nid && u.side===G.side && u.status!=='DESTROYED');
  if (unitsHere.length === 0) return;
  if (unitsHere.length === 1) { selectUnit(unitsHere[0].id); return; }
  showUnitPicker(nid, unitsHere);
}

// When clicking a dot directly
function onUnitDotClick(nid, uid) {
  if (G.routeBuilding || G.pickingTarget) { onNodeClick(nid); return; }
  selectUnit(uid);
}

// ── Unit picker popup (multiple units at same node) ────────────────────────
function showUnitPicker(nid, units) {
  // Remove existing picker
  const old=document.getElementById('unit-picker'); if(old) old.remove();
  const n=NODES[nid];
  // Convert SVG coords to screen coords
  const svg=document.getElementById('game-map');
  const pt=svg.createSVGPoint();
  pt.x=n.x; pt.y=n.y+50;
  const screen=pt.matrixTransform(svg.getScreenCTM());

  const div=document.createElement('div');
  div.id='unit-picker';
  div.style.cssText=`position:fixed;left:${screen.x-90}px;top:${screen.y+8}px;
    background:#1e1409;border:1px solid rgba(201,146,26,0.45);border-radius:4px;padding:10px;
    z-index:600;box-shadow:0 8px 32px rgba(0,0,0,0.8),0 0 0 1px rgba(0,0,0,0.5);min-width:180px`;
  div.innerHTML='<div style="font-family:Cinzel,serif;font-size:0.6rem;font-weight:700;color:#7a5a10;text-transform:uppercase;letter-spacing:.2em;margin-bottom:8px">Select Unit</div>';
  units.forEach(u=>{
    const b=document.createElement('button');
    b.style.cssText=`display:block;width:100%;text-align:left;padding:8px 11px;margin-bottom:4px;
      background:rgba(0,0,0,0.3);border:1px solid rgba(201,146,26,0.2);border-radius:3px;cursor:pointer;
      font-family:'Crimson Text',serif;font-size:14px;font-weight:600;color:#f0e6cc;transition:all .12s`;
    const nameColor=u.side==='SHADOW'?'#d96060':'#7aabff';
    b.innerHTML=`<span style="color:${nameColor}">${u.id}</span><span style="color:#7a5a10;font-size:12px"> · ⚔${u.strength} · ${u.status}</span>`;
    b.onmouseenter=()=>{b.style.borderColor='rgba(201,146,26,0.6)';b.style.background='rgba(201,146,26,0.08)';};
    b.onmouseleave=()=>{b.style.borderColor='rgba(201,146,26,0.2)';b.style.background='rgba(0,0,0,0.3)';};
    b.onclick=()=>{ div.remove(); selectUnit(u.id); };
    div.appendChild(b);
  });
  const close=document.createElement('button');
  close.textContent='✕ Cancel';
  close.style.cssText=`width:100%;padding:5px;border:none;background:none;color:rgba(192,32,32,0.6);
    font-family:Cinzel,serif;font-size:0.6rem;font-weight:700;letter-spacing:.1em;text-transform:uppercase;cursor:pointer;margin-top:2px`;
  close.onmouseenter=()=>close.style.color='#c02020';
  close.onmouseleave=()=>close.style.color='rgba(192,32,32,0.6)';
  close.onclick=()=>div.remove();
  div.appendChild(close);
  document.body.appendChild(div);
  // Close on outside click
  setTimeout(()=>{ document.addEventListener('click',function h(e){ if(!div.contains(e.target)){div.remove();document.removeEventListener('click',h);}}); },50);
}

// ── Tooltip ────────────────────────────────────────────────────────────────
function showTip(ev,nid) {
  const n=NODES[nid], r=G.regions[nid];
  const us=Object.values(G.units).filter(u=>u.currentRegion===nid);
  let html=`<b>${n.label}</b><div class="tr">${n.terrain.charAt(0).toUpperCase()+n.terrain.slice(1)}`;
  if(r) html+=` · ${r.control==='FREE_PEOPLES'?'Free':r.control==='SHADOW'?'Shadow':'Neutral'} · Threat ${r.threatLevel}${r.fortified?' 🛡':''}`;
  html+='</div>';
  if(us.length) html+=`<div class="tu">${us.map(u=>u.id+(u.class==='RingBearer'?' 💍':'')).join(', ')}</div>`;
  const tip=document.getElementById('tip');
  tip.innerHTML=html;
  tip.style.left=(ev.clientX+14)+'px'; tip.style.top=(ev.clientY-8)+'px'; tip.style.display='block';
}
function hideTip(){document.getElementById('tip').style.display='none';}

// ── Unit list ──────────────────────────────────────────────────────────────
function renderUnitList() {
  const c=document.getElementById('unit-scroll');
  c.innerHTML='';
  const sorted=Object.values(G.units).sort((a,b)=>{
    const ao=a.side===G.side?0:1, bo=b.side===G.side?0:1;
    if(ao!==bo) return ao-bo;
    if(a.class==='RingBearer') return -1;
    if(b.class==='RingBearer') return 1;
    return 0;
  });
  sorted.forEach(u=>{
    const isOwn=u.side===G.side, isRB=u.class==='RingBearer', isDead=u.status==='DESTROYED';
    const div=document.createElement('div');
    div.className='ucard'+(isDead?' dead':'')+(G.selectedUnit===u.id?' active':'')+(isOwn?'':' enemy');
    // Own units always use gold/warm badge so both sides look the same; enemies keep faction color
    const badgeCls='u-badge '+(isRB?'rb':isOwn?'own':u.side==='FREE_PEOPLES'?'free':'shadow');
    const nameCls='u-name '+(isOwn?'own-name':u.side==='SHADOW'?'shadow-name':'free-name');
    const region=isRB&&G.side==='SHADOW'?'(hidden)':(u.currentRegion||'—');
    div.innerHTML=`<div class="${badgeCls}">${isRB?'💍':u.id.charAt(0).toUpperCase()}</div>
      <div class="u-body">
        <div class="${nameCls}">${u.id}${isRB?' <small style="color:#a5b1c2">(Ring Bearer)</small>':''}</div>
        <div class="u-detail">${region} · ${u.status}${u.cooldown>0?' · CD:'+u.cooldown:''}</div>
      </div>
      <div class="u-str">⚔${u.strength}</div>`;
    if(isOwn&&!isDead) div.addEventListener('click',()=>selectUnit(u.id));
    c.appendChild(div);
  });
}

// ── Select unit ────────────────────────────────────────────────────────────
async function selectUnit(uid) {
  const u=G.units[uid];
  if(!u||u.side!==G.side) return; // security: only own units

  // Full cleanup of any previous interactive state
  if(G.routeBuilding || G._highlightedNodes) {
    clearAdjacent(); updateNodeColors(); updateEdges();
  }
  G._clearHighlightedNodes();
  G.selectedUnit=uid;
  G.routeBuilding=false; G.builtPaths=[]; G.builtNodes=[]; G.lastNode=null;
  G.pickingTarget=null;
  document.getElementById('hint-bar').textContent='';
  document.getElementById('map-area').style.cursor='default';
  const pp=document.getElementById('path-picker'); if(pp) pp.remove();
  const up=document.getElementById('unit-picker'); if(up) up.remove();

  renderUnitList();
  document.getElementById('sel-name').textContent=uid;
  document.getElementById('order-panel').style.display='block';
  document.getElementById('route-builder').style.display='none';
  document.getElementById('order-btns').innerHTML='<div style="font-size:11px;color:#a5b1c2">Loading…</div>';
  try {
    const r=await fetch('/api/orders/available?unitId='+uid+'&playerId='+G.playerID);
    if(!r.ok){document.getElementById('order-btns').innerHTML='<div style="color:#eb3b5a;font-size:11px">Error loading orders</div>';return;}
    buildOrderButtons((await r.json()).orders||[]);
  } catch(e){ document.getElementById('order-btns').innerHTML='<div style="color:#eb3b5a;font-size:11px">Unreachable</div>'; }
}

const ORDER_LABELS = {
  'ASSIGN_ROUTE':     '🗺 Assign Route — set initial march path',
  'REDIRECT_UNIT':    '↩ Redirect Unit — override current route',
  'DESTROY_RING':     '💥 Destroy Ring — cast it into the fire',
  'MAIA_ABILITY':     '✨ Maia Ability — invoke magical power',
  'BLOCK_PATH':       '🚫 Block Path — seal a road against passage',
  'SEARCH_PATH':      '🔍 Search Path — survey a road for the Ring',
  'ATTACK_REGION':    '⚔ Attack Region — assault an adjacent region',
  'REINFORCE_REGION': '🛡 Reinforce Region — strengthen an adjacent region',
  'FORTIFY_REGION':   '🏰 Fortify Region — build defences here',
  'DEPLOY_NAZGUL':    '🦅 Deploy Nazgûl — send wraith to adjacent region',
};

function buildOrderButtons(orders) {
  const c=document.getElementById('order-btns');
  c.innerHTML='';

  // Check: is it even our turn?
  const myTurn = !G.activeSide || G.activeSide === G.side;
  if(!myTurn) {
    const opp=G.activeSide==='FREE_PEOPLES'?'Light Side':'Dark Side';
    c.innerHTML='<div style="font-size:12px;color:#c9921a;padding:6px 0;font-style:italic">⌛ Waiting for '+opp+'…</div>';
    return;
  }

  // Check: unit already has a pending order this turn?
  if(G.isUnitOrdered(G.selectedUnit)) {
    c.innerHTML='<div style="font-size:12px;color:#26de81;padding:6px 0">✓ Order already queued — remove it from the list below to change</div>';
    return;
  }

  if(orders.length===0){
    c.innerHTML='<div style="font-size:11px;color:#a5b1c2;padding:4px 0">No orders available this turn</div>';
    return;
  }
  orders.forEach(ot=>{
    const b=document.createElement('button');
    b.innerHTML=ORDER_LABELS[ot]||ot.replace(/_/g,' ');
    b.title=ot;
    b.addEventListener('click',()=>handleOrder(ot));
    c.appendChild(b);
  });
}

// ── Handle order ───────────────────────────────────────────────────────────
// Field names per spec Section 5.3:
//   ASSIGN_ROUTE    → pathIds[]
//   REDIRECT_UNIT   → newPathIds[]
//   MAIA_ABILITY    → targetPathId
//   BLOCK_PATH      → pathId
//   SEARCH_PATH     → pathId
//   ATTACK_REGION   → targetRegionId
//   REINFORCE_REGION→ targetRegionId
//   DEPLOY_NAZGUL   → targetRegionId
//   DESTROY_RING / FORTIFY_REGION → no extra fields
async function handleOrder(ot) {
  const order={orderType:ot,playerId:G.playerID,side:G.side,unitId:G.selectedUnit,turn:G.turn};
  const u=G.units[G.selectedUnit];

  // ── Always reset previous interactive state before starting new order ──────
  if(G.routeBuilding && ot!=='ASSIGN_ROUTE' && ot!=='REDIRECT_UNIT') {
    G.routeBuilding=false;
    G.builtPaths=[]; G.builtNodes=[]; G.lastNode=null;
    clearAdjacent(); updateNodeColors(); updateEdges();
    document.getElementById('route-builder').style.display='none';
    document.getElementById('hint-bar').textContent='';
  }
  G.pickingTarget=null;
  G._clearHighlightedNodes();

  // ── Route orders ──────────────────────────────────────────
  if(ot==='ASSIGN_ROUTE'||ot==='REDIRECT_UNIT') {
    const startRegion=u&&u.currentRegion;
    G.routeBuilding=true; G.routeOrderType=ot;
    G.builtPaths=[]; G.builtNodes=startRegion?[startRegion]:[];
    G.lastNode=startRegion||null;
    if(startRegion) { highlightNode(startRegion,'#e8b84b'); showAdjacent(startRegion); }
    document.getElementById('route-builder').style.display='block';
    const startLabel=startRegion?(NODES[startRegion]||{label:startRegion}).label:'?';
    const modeLabel=ot==='ASSIGN_ROUTE'?'Initial Route':'Redirect';
    document.getElementById('rprev').textContent=startRegion
      ? '['+modeLabel+'] '+startLabel+' → (click cyan-outlined nodes)'
      : 'Click your unit\'s node first…';
    document.getElementById('hint-bar').textContent=
      ot==='ASSIGN_ROUTE'
        ? '🗺 Set march path — click adjacent nodes on map, then Confirm'
        : '↩ Override route — click new destination nodes, then Confirm';
    return;
  }

  // ── No-payload orders ─────────────────────────────────────
  if(ot==='DESTROY_RING')   { enqueue(order,'DESTROY_RING ⚡'); return; }
  if(ot==='FORTIFY_REGION') { enqueue(order,'FORTIFY_REGION'); return; }

  // ── Maia ability — targets an adjacent path ───────────────
  if(ot==='MAIA_ABILITY') {
    const adjPaths=(ADJ[u&&u.currentRegion]||[]).map(e=>e.id);
    const pid=await pickPath(adjPaths,'Select path for Maia ability');
    if(!pid) return;
    order.targetPathId=pid;          // spec: "tar..." = targetPathId
    enqueue(order,'MAIA_ABILITY on '+pid);
    return;
  }

  // ── Path orders — unit must be at an endpoint ─────────────
  if(ot==='BLOCK_PATH'||ot==='SEARCH_PATH') {
    const adjPaths=(ADJ[u&&u.currentRegion]||[]).map(e=>e.id);
    const hint=ot==='BLOCK_PATH'?'Select path to block':'Select path to search';
    const pid=await pickPath(adjPaths,hint);
    if(!pid) return;
    order.pathId=pid;                // spec: "pathI..." = pathId
    enqueue(order,ot+' on '+pid);
    return;
  }

  // ── Region orders — unit must be in an adjacent region (spec §ErrUnitNotAdjacent)
  if(ot==='ATTACK_REGION') {
    // Highlight only adjacent nodes to make valid targets obvious
    const adjNbs=(ADJ[u&&u.currentRegion]||[]).map(e=>e.nb);
    adjNbs.forEach(nid=>{
      const c=document.getElementById('nc-'+nid);
      if(c){c.setAttribute('fill','#8b1a1a');c.setAttribute('stroke','#c02020');c.setAttribute('stroke-width','3.5');}
      G._highlightedNodes.push(nid);
    });
    const rid=await pickAdjacentRegion(adjNbs,'👆 Click adjacent region to attack');
    G._clearHighlightedNodes();
    updateNodeColors(); updateEdges();
    if(!rid) return;
    order.targetRegionId=rid;
    enqueue(order,'ATTACK → '+(NODES[rid]||{label:rid}).label);
    return;
  }

  if(ot==='REINFORCE_REGION') {
    const adjNbs=(ADJ[u&&u.currentRegion]||[]).map(e=>e.nb);
    adjNbs.forEach(nid=>{
      const c=document.getElementById('nc-'+nid);
      if(c){c.setAttribute('fill','#1a5a2e');c.setAttribute('stroke','#26de81');c.setAttribute('stroke-width','3.5');}
      G._highlightedNodes.push(nid);
    });
    const rid=await pickAdjacentRegion(adjNbs,'👆 Click adjacent region to reinforce');
    G._clearHighlightedNodes();
    updateNodeColors(); updateEdges();
    if(!rid) return;
    order.targetRegionId=rid;
    enqueue(order,'REINFORCE → '+(NODES[rid]||{label:rid}).label);
    return;
  }

  if(ot==='DEPLOY_NAZGUL') {
    const adjNbs=(ADJ[u&&u.currentRegion]||[]).map(e=>e.nb);
    adjNbs.forEach(nid=>{
      const c=document.getElementById('nc-'+nid);
      if(c){c.setAttribute('fill','#4a0e5a');c.setAttribute('stroke','#9b59b6');c.setAttribute('stroke-width','3.5');}
      G._highlightedNodes.push(nid);
    });
    const rid=await pickAdjacentRegion(adjNbs,'👆 Click adjacent region to deploy Nazgûl');
    G._clearHighlightedNodes();
    updateNodeColors(); updateEdges();
    if(!rid) return;
    order.targetRegionId=rid;
    enqueue(order,'DEPLOY_NAZGUL → '+(NODES[rid]||{label:rid}).label);
    return;
  }

  // ── Fallback: enqueue as-is ───────────────────────────────
  enqueue(order, ot.replace(/_/g,' '));
}

// ── Pick region from map (restricted to validNbs list if provided) ─────────
function pickAdjacentRegion(validNbs, hint) {
  return new Promise(resolve=>{
    document.getElementById('hint-bar').textContent=hint+' ('+validNbs.length+' adjacent)';
    document.getElementById('map-area').style.cursor='crosshair';
    G.pickingTarget={
      type:'region',
      validNbs,
      resolve: (nid)=>{
        if(validNbs.length>0 && !validNbs.includes(nid)){
          log('Unit must be adjacent to target region','danger');
          return;
        }
        G.pickingTarget=null;
        document.getElementById('hint-bar').textContent='';
        document.getElementById('map-area').style.cursor='default';
        resolve(nid);
      }
    };
  });
}

// ── Pick path from adjacent nodes ─────────────────────────────────────────
function pickPath(adjPaths, hint) {
  // For paths, we show a small inline picker since paths are edges not nodes
  return new Promise(resolve=>{
    if(adjPaths.length===0){resolve(null);return;}
    const old=document.getElementById('path-picker'); if(old) old.remove();
    const div=document.createElement('div');
    div.id='path-picker';
    div.style.cssText=`position:fixed;right:310px;top:50%;transform:translateY(-50%);
      background:#1e1409;border:1px solid rgba(201,146,26,0.45);border-radius:4px;padding:12px;
      z-index:600;box-shadow:0 8px 32px rgba(0,0,0,0.8);min-width:240px;max-height:320px;overflow-y:auto`;
    div.innerHTML=`<div style="font-family:Cinzel,serif;font-size:0.6rem;font-weight:700;color:#7a5a10;text-transform:uppercase;letter-spacing:.2em;margin-bottom:10px">${hint}</div>`;
    adjPaths.forEach(pid=>{
      const p=G.paths[pid];
      const status=p?p.status:'OPEN';
      const statusColor=status==='BLOCKED'?'#c02020':status==='THREATENED'?'#c9921a':status==='TEMPORARILY_OPEN'?'#26de81':'#4a7a4a';
      const b=document.createElement('button');
      b.style.cssText=`display:block;width:100%;text-align:left;padding:8px 11px;margin-bottom:5px;
        background:rgba(0,0,0,0.3);border:1px solid rgba(201,146,26,0.2);border-radius:3px;cursor:pointer;
        font-family:'Crimson Text',serif;font-size:13px;font-weight:600;color:#f0e6cc;transition:all .12s`;
      b.innerHTML=`${pid.replace(/-/g,' ')} <span style="color:${statusColor};font-size:11px;font-style:italic">[${status}]</span>`;
      b.onmouseenter=()=>{b.style.borderColor='rgba(201,146,26,0.6)';b.style.background='rgba(201,146,26,0.08)';};
      b.onmouseleave=()=>{b.style.borderColor='rgba(201,146,26,0.2)';b.style.background='rgba(0,0,0,0.3)';};
      b.onclick=()=>{div.remove();resolve(pid);};
      div.appendChild(b);
    });
    const cancel=document.createElement('button');
    cancel.textContent='✕ Cancel';
    cancel.style.cssText=`width:100%;padding:5px;border:none;background:none;color:rgba(192,32,32,0.6);
      font-family:Cinzel,serif;font-size:0.6rem;font-weight:700;letter-spacing:.1em;text-transform:uppercase;cursor:pointer;margin-top:4px`;
    cancel.onmouseenter=()=>cancel.style.color='#c02020';
    cancel.onmouseleave=()=>cancel.style.color='rgba(192,32,32,0.6)';
    cancel.onclick=()=>{div.remove();resolve(null);};
    div.appendChild(cancel);
    document.body.appendChild(div);
  });
}

function enqueue(order, label) {
  G.pendingOrders.push(order);
  updateOrderCount();
  logEntry('order', '📜', 'Queued — ' + label);
  clearSel();
}

// ── Route building ─────────────────────────────────────────────────────────
// Tracks which nodes are currently dim-highlighted as "reachable next hop"
const _adjHighlighted = [];

// Tracks edge elements highlighted as reachable next hop
const _adjEdgeHighlighted = [];

function showAdjacent(fromNode) {
  // Clear previous adjacent highlights (nodes + edges)
  _adjHighlighted.forEach(n => unhighlightNode(n));
  _adjHighlighted.length = 0;
  _adjEdgeHighlighted.forEach(eid => {
    const el = document.getElementById('e-'+eid);
    if(el) el.classList.remove('path-adj-hint');
  });
  _adjEdgeHighlighted.length = 0;

  // Highlight adjacent nodes (cyan stroke) and their connecting edges (pulse)
  (ADJ[fromNode]||[]).forEach(e => {
    if(G.builtNodes.includes(e.nb)) return; // skip already-visited
    const c = document.getElementById('nc-'+e.nb);
    if(c) {
      c.setAttribute('stroke','#00cec9');
      c.setAttribute('stroke-width','3.5');
      _adjHighlighted.push(e.nb);
    }
    const el = document.getElementById('e-'+e.id);
    if(el) {
      el.classList.add('path-adj-hint');
      _adjEdgeHighlighted.push(e.id);
    }
  });
}

function clearAdjacent() {
  _adjHighlighted.forEach(n => unhighlightNode(n));
  _adjHighlighted.length = 0;
  _adjEdgeHighlighted.forEach(eid => {
    const el = document.getElementById('e-'+eid);
    if(el) el.classList.remove('path-adj-hint');
  });
  _adjEdgeHighlighted.length = 0;
}

function addRouteNode(nid) {
  // If no start set yet (edge case), treat first click as start
  if(G.lastNode===null) {
    G.lastNode=nid;
    G.builtNodes=[nid];
    highlightNode(nid,'#e8b84b');
    const lbl=(NODES[nid]||{label:nid}).label;
    document.getElementById('rprev').textContent=lbl+' → (click destination)';
    showAdjacent(nid);
    return;
  }
  // Ignore clicking the same node twice
  if(nid===G.lastNode) return;
  // Find connecting edge (bidirectional)
  const edge=(ADJ[G.lastNode]||[]).find(e=>e.nb===nid);
  if(!edge){
    const fromLbl=(NODES[G.lastNode]||{label:G.lastNode}).label;
    const toLbl=(NODES[nid]||{label:nid}).label;
    document.getElementById('rprev').textContent=
      '⚠ '+toLbl+' is not adjacent to '+fromLbl+' — click a cyan-outlined node';
    return;
  }
  G.builtPaths.push(edge.id);
  G.builtNodes.push(nid);
  G.lastNode=nid;
  highlightNode(nid,'#e8b84b');
  highlightEdge(edge.id);
  showAdjacent(nid);
  // Preview: show full path using builtNodes (correct direction always)
  const labels=G.builtNodes.map(n=>(NODES[n]||{label:n}).label);
  document.getElementById('rprev').textContent=labels.join(' → ');
}

function highlightNode(nid,color) {
  const c=document.getElementById('nc-'+nid);
  if(c){
    c.setAttribute('fill',color);
    c.setAttribute('stroke','#e8b84b');
    c.setAttribute('stroke-width','4');
  }
}
function unhighlightNode(nid) {
  const c=document.getElementById('nc-'+nid);
  if(!c) return;
  const r=G.regions[nid];
  const ctrl=r?r.control:(NODES[nid]&&NODES[nid].startCtrl)||'NEUTRAL';
  c.setAttribute('fill',CTRL_COLOR[ctrl]||'#7a6a50');
  c.setAttribute('stroke','rgba(201,146,26,0.55)');
  c.setAttribute('stroke-width','2.5');
}
function highlightEdge(eid) {
  const el=document.getElementById('e-'+eid);
  if(el){el.setAttribute('stroke','#f9ca24');el.setAttribute('stroke-width','5');el.removeAttribute('stroke-dasharray');}
}
function clearRoute() {
  G.builtPaths=[];
  const u=G.selectedUnit&&G.units[G.selectedUnit];
  const startRegion=u&&u.currentRegion||null;
  G.lastNode=startRegion;
  G.builtNodes=startRegion?[startRegion]:[];
  clearAdjacent();
  updateNodeColors(); updateEdges();
  if(startRegion) { highlightNode(startRegion,'#e8b84b'); showAdjacent(startRegion); }
  const startLabel=startRegion?(NODES[startRegion]||{label:startRegion}).label:'?';
  document.getElementById('rprev').textContent=startRegion
    ? startLabel+' → (click cyan-outlined nodes)'
    : 'Click start node…';
}
function confirmRoute() {
  if(G.builtPaths.length===0){
    document.getElementById('rprev').textContent='⚠ Click at least one destination node first';
    return;
  }
  const order={orderType:G.routeOrderType,playerId:G.playerID,unitId:G.selectedUnit,turn:G.turn};
  if(G.routeOrderType==='ASSIGN_ROUTE') order.pathIds=[...G.builtPaths];
  else order.newPathIds=[...G.builtPaths];
  const label=G.routeOrderType+' ('+G.builtPaths.length+' steps): '+
    G.builtNodes.map(n=>(NODES[n]||{label:n}).label).join(' → ');
  G.routeBuilding=false;
  G.builtPaths=[]; G.builtNodes=[];
  clearAdjacent();
  updateNodeColors(); updateEdges();
  document.getElementById('route-builder').style.display='none';
  document.getElementById('hint-bar').textContent='';
  G.pendingOrders.push(order);
  updateOrderCount();
  logEntry('order', '📜', 'Queued — ' + label);
  clearSel();
}

function clearSel() {
  if(G.routeBuilding || G._highlightedNodes.length) { clearAdjacent(); updateNodeColors(); updateEdges(); }
  G._clearHighlightedNodes();
  G.selectedUnit=null; G.routeBuilding=false;
  G.builtPaths=[]; G.builtNodes=[]; G.lastNode=null;
  G.pickingTarget=null;
  document.getElementById('order-panel').style.display='none';
  document.getElementById('route-builder').style.display='none';
  document.getElementById('hint-bar').textContent='';
  document.getElementById('map-area').style.cursor='default';
  const pp=document.getElementById('path-picker'); if(pp) pp.remove();
  const up=document.getElementById('unit-picker'); if(up) up.remove();
  renderUnitList();
}

// ── Turn indicator ─────────────────────────────────────────────────────────
function updateTurnIndicator() {
  const myTurn = !G.activeSide || G.activeSide === G.side;
  const sbtn = document.getElementById('sbtn');
  const hdrStatus = document.getElementById('hdr-status');
  const banner = document.getElementById('turn-banner');

  if (myTurn) {
    sbtn.disabled = G.pendingOrders.length === 0;
    hdrStatus.textContent = '⚔ Your Turn';
    hdrStatus.style.color = '#26de81';
    if (banner) {
      banner.textContent = '⚔ Your Turn — Give orders, then Dispatch';
      banner.className = 'turn-banner my-turn';
      banner.style.display = 'block';
      setTimeout(()=>{ if(banner) banner.style.display='none'; }, 4000);
    }
  } else {
    sbtn.disabled = true;
    const opp = G.activeSide==='FREE_PEOPLES'?'⚪ Light Side':'⚫ Dark Side';
    hdrStatus.textContent = '⌛ '+opp+'\'s Turn';
    hdrStatus.style.color = '#c9921a';
    if (banner) {
      banner.textContent = '⌛ '+opp+'\'s Turn — Waiting…';
      banner.className = 'turn-banner opp-turn';
      banner.style.display = 'block';
      setTimeout(()=>{ if(banner) banner.style.display='none'; }, 4000);
    }
  }
}

// ── Submit ─────────────────────────────────────────────────────────────────
async function submitOrders() {
  if (G.activeSide && G.activeSide !== G.side) {
    log('⚠ Not your turn!','danger'); return;
  }
  const count=G.pendingOrders.length;
  let ok=0, fail=0;

  // 1. Post each order individually
  for(const o of G.pendingOrders) {
    try {
      const r=await fetch('/api/order',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(o)});
      if(r.ok) ok++;
      else { fail++; log('⚠ Rejected: '+o.orderType+' ('+r.status+')','danger'); }
    } catch(e){ fail++; log('⚠ Network error: '+o.orderType,'danger'); }
  }

  // 2. Signal dispatch — triggers phase advance (or turn execution if Dark)
  try {
    const dr=await fetch('/api/orders/dispatch',{
      method:'POST',
      headers:{'Content-Type':'application/json'},
      body:JSON.stringify({playerId:G.playerID, side:G.side})
    });
    if(!dr.ok) { log('⚠ Dispatch failed ('+dr.status+')','danger'); return; }
  } catch(e){ log('⚠ Dispatch network error','danger'); return; }

  G.pendingOrders=[]; updateOrderCount();
  document.getElementById('sbtn').disabled=true;

  if(count===0) {
    logEntry('phase','✓','Dispatched with no orders — passing turn','hi');
  } else if(fail===0) {
    logEntry('phase','✓', count+' order'+(count>1?'s':'')+' dispatched successfully','hi');
  } else {
    logEntry('phase','⚠', ok+' dispatched, '+fail+' rejected by server','danger');
  }

  // If we're Light, we're now waiting for Dark
  if(G.side==='FREE_PEOPLES') {
    document.getElementById('hdr-status').textContent='⌛ Dark Side\'s Turn';
    document.getElementById('hdr-status').style.color='#c9921a';
  } else {
    // Dark dispatched — turn is processing
    document.getElementById('hdr-status').textContent='⚙ Processing turn…';
    document.getElementById('hdr-status').style.color='#7a5a10';
    setTimeout(fetchState, 1200);
  }
}
function updateOrderCount() {
  const n=G.pendingOrders.length;
  // Submit button is enabled only when it's our turn AND we have orders
  const myTurn = !G.activeSide || G.activeSide === G.side;
  document.getElementById('sbtn').disabled=(n===0 || !myTurn);
  const list=document.getElementById('pending-list');
  if(!list) return;
  list.innerHTML='';
  if(n===0){ list.style.display='none'; return; }
  list.style.display='flex';
  G.pendingOrders.forEach((o,i)=>{
    const row=document.createElement('div');
    row.style.cssText='display:flex;align-items:center;gap:6px;padding:4px 6px;font-size:11px;';
    const label=document.createElement('span');
    label.style.cssText='flex:1;color:#e8b84b;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;font-family:Crimson Text,serif;font-size:13px;';
    label.textContent=o.orderType.replace(/_/g,' ')+' — '+o.unitId;
    const del=document.createElement('button');
    del.textContent='✕';
    del.title='Remove this order';
    del.style.cssText='border:none;background:none;color:rgba(192,32,32,0.7);font-weight:800;font-size:13px;cursor:pointer;padding:0 2px;line-height:1;flex-shrink:0;';
    del.onclick=()=>{ G.pendingOrders.splice(i,1); updateOrderCount(); };
    row.appendChild(label); row.appendChild(del);
    list.appendChild(row);
  });
}

// ── Analysis (Counsel of the Wise) ─────────────────────────────────────────
//
// Route Risk Assessment (Light Side — Pipeline 1):
//   4 worker goroutines score 4 candidate routes for the Ring Bearer.
//   Formula: surveillanceLevel×3 + path status bonus (BLOCKED +5, THREATENED +2)
//            + sum of region threatLevels + Nazgul proximity count×2
//   Lower score = safer road. "RECOMMENDED" = lowest score route.
//
// Interception Plan (Dark Side — Pipeline 2):
//   4 worker goroutines compute, for every (Nazgul × route waypoint) pair,
//   how many turns the Nazgul needs vs how many turns the Ring Bearer needs.
//   Score = 1 − (turnsToIntercept / routeLength)  →  0..1, higher = better.
//   Output: best interception target per Nazgul, ranked by descending score.
//
// NOTE: Server returns Go PascalCase JSON (Routes, ByUnit, RiskScore, UnitID…).
//       This function normalises both PascalCase and camelCase variants.
async function fetchAnalysis(type) {
  const out = document.getElementById('aout');
  out.textContent = '⟳ Running pipeline analysis…';
  try {
    const r = await fetch('/api/analysis/'+type+'?playerId='+G.playerID);
    if (!r.ok) { out.textContent = '⚠ Server error '+r.status; return; }
    const d = await r.json();

    // ── Routes ──────────────────────────────────────────────────────────────
    if (type === 'routes') {
      const routes  = d.Routes  || d.routes  || [];
      const recID   = d.Recommended || d.recommended || '';
      const partial = (d.Partial || d.partial) ? ' ⚠ PARTIAL (timed out)' : '';

      let lines = [
        '═══ ROUTE RISK ASSESSMENT' + partial + ' ═══',
        'Formula: surveillance×3 + path bonus + threat levels + Nazgûl proximity×2',
        'Lower score = safer road for the Ring Bearer',
        '',
      ];

      if (routes.length === 0) {
        lines.push('No route data — start the game and wait for the first turn.');
      } else {
        routes.forEach((rt, i) => {
          const id    = rt.RouteID    || rt.routeId    || '?';
          const score = rt.RiskScore  ?? rt.riskScore  ?? 0;
          const warn  = rt.Warnings   || rt.warnings   || [];
          const rec   = (id === recID);
          const filled = Math.min(10, Math.round(score / 2));
          const bar    = '▓'.repeat(filled) + '░'.repeat(10 - filled);
          const icon   = score > 15 ? '🔴' : score > 8 ? '🟡' : '🟢';
          lines.push(icon + ' [' + (i+1) + '] ' + id + (rec ? '  ★ RECOMMENDED' : ''));
          lines.push('   Risk score: ' + score + '  [' + bar + ']');
          if (warn.length) {
            lines.push('   ⚠ Hazards: ' + warn.map(w => w.replace(/:/g, ': ')).join('  |  '));
          }
          lines.push('');
        });
      }
      out.textContent = lines.join('\n');

    // ── Intercept ────────────────────────────────────────────────────────────
    } else if (type === 'intercept') {
      const units   = d.ByUnit  || d.byUnit  || [];
      const partial = (d.Partial || d.partial) ? ' ⚠ PARTIAL (timed out)' : '';

      let lines = [
        '═══ INTERCEPTION PLAN' + partial + ' ═══',
        'Score = 1 − (Nazgûl travel turns ÷ route length)',
        'Higher score = better chance to intercept the Ring Bearer',
        '',
      ];

      if (units.length === 0) {
        lines.push('No active Nazgûl or Ring Bearer has no assigned route yet.');
        lines.push('Deploy Nazgûl and wait for the Ring Bearer to move.');
      } else {
        const sorted = [...units].sort((a,b) =>
          (b.Score ?? b.score ?? 0) - (a.Score ?? a.score ?? 0));
        sorted.forEach(p => {
          const uid    = p.UnitID       || p.unitId       || '?';
          const region = p.TargetRegion || p.targetRegion || '?';
          const score  = p.Score        ?? p.score        ?? 0;
          const rLabel = (NODES[region] || {label: region}).label;
          const icon   = score > 0.7 ? '🔴 HIGH'
                       : score > 0.4 ? '🟡 MED'
                       : score > 0   ? '🟢 LOW'
                       :               '⚫ NONE';
          lines.push(icon + '  ' + uid);
          lines.push('   Best intercept: ' + rLabel + '   Score: ' + score.toFixed(3));
          lines.push('');
        });
      }
      out.textContent = lines.join('\n');
    }
  } catch(e) { out.textContent = '⚠ Error: ' + e.message; }
}

// ── Game over ──────────────────────────────────────────────────────────────
function showOver(ev) {
  document.getElementById('game').classList.remove('on');
  document.getElementById('over').classList.add('on');
  document.getElementById('ov-title').textContent=ev.winner==='FREE_PEOPLES'?'⚪ Light Side Wins!':ev.winner==='SHADOW'?'⚫ Dark Side Wins!':'Draw!';
  document.getElementById('ov-cause').textContent=ev.cause||'';
  if(G.sse) G.sse.close();
}

// ── Chronicle of Events ────────────────────────────────────────────────────
// logEntry: structured log with turn badge, icon, category label, message
function logEntry(category, icon, msg, cls) {
  const el = document.getElementById('elog-scroll');
  const d  = document.createElement('div');
  d.className = 'log-item' + (cls ? ' ' + cls : '');

  const now = new Date();
  const ts  = now.toLocaleTimeString('en-GB',{hour:'2-digit',minute:'2-digit',second:'2-digit'});

  const catLabels = {
    phase:'PHASE', turn:'TURN', move:'MOVE', rb:'RING', path:'ROAD',
    detect:'EYE', battle:'BATTLE', block:'BLOCK', region:'REGION',
    maia:'MAIA', order:'ORDER',
  };
  const catLabel = catLabels[category] || category.toUpperCase();

  d.innerHTML =
    '<span class="log-badge" style="' +
      'display:inline-block;font-family:Cinzel,serif;font-size:9px;font-weight:700;' +
      'letter-spacing:.12em;padding:1px 5px;border-radius:2px;margin-right:5px;' +
      'background:rgba(201,146,26,0.15);color:#c9921a;border:1px solid rgba(201,146,26,0.3);' +
    '">' + catLabel + '</span>' +
    '<span class="log-turn-num" style="color:#7a5a10;font-size:10px;margin-right:4px">T' + G.turn + '</span>' +
    '<span class="log-icon" style="margin-right:4px">' + icon + '</span>' +
    '<span class="log-text" style="font-family:Crimson Text,serif;font-size:13px">' + escLog(msg) + '</span>' +
    '<span class="log-ts" style="float:right;color:#4a3a20;font-size:10px;padding-left:4px">' + ts + '</span>';

  el.prepend(d);
  if (el.children.length > 120) el.removeChild(el.lastChild);
}

// Escape unsafe chars — unit/region IDs are server-controlled but sanitise anyway
function escLog(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

// Legacy shim so existing call-sites (enqueue, submitOrders, etc.) still work
function log(msg, cls) {
  logEntry('info', '•', msg, cls);
}

// ── Visual enhancements ─────────────────────────────────────────────────────
let _timerInterval=null;

// elapsedSeconds: how many seconds have already passed in this turn (0 = fresh start)
function startTurnTimer(elapsedSeconds=0) {
  if(_timerInterval) clearInterval(_timerInterval);
  const bar=document.getElementById('turn-timer-bar');
  if(!bar) return;
  const DURATION=60;
  let elapsed=Math.min(elapsedSeconds, DURATION-1);

  function applyBar() {
    const pct=Math.max(0,100-(elapsed/DURATION*100));
    bar.style.width=pct+'%';
    if(pct<20)      bar.style.background='linear-gradient(90deg,#5a0e0e,#c02020)';
    else if(pct<45) bar.style.background='linear-gradient(90deg,#7a5a10,#c9921a)';
    else            bar.style.background='linear-gradient(90deg,#7a5a10,#e8b84b,#f9ca24)';
  }

  // Snap bar to current position without animation, then re-enable
  bar.style.transition='none';
  applyBar();
  bar.offsetWidth; // force reflow
  bar.style.transition='width 1s linear';

  _timerInterval=setInterval(()=>{
    elapsed++;
    applyBar();
    if(elapsed>=DURATION){ clearInterval(_timerInterval); _timerInterval=null; }
  },1000);
}
