// ── State ──────────────────────────────────────────────────────────────────
const S = { path: null, crumbs: [], pendingDelete: null, pendingDeleteIsDir: false, renameInfo: null };
const CLIP = { src: null, name: null };

function copyToClip(path, name) {
  CLIP.src = path; CLIP.name = name;
  $('pb-label').textContent = name;
  $('paste-bar').classList.add('visible');
  toast('Copied "'+name+'" — navigate to destination and tap Paste', 'info');
}
function clearClip() {
  CLIP.src = null; CLIP.name = null;
  $('paste-bar').classList.remove('visible');
}
async function doPaste() {
  if (!CLIP.src || !S.path) return;
  const src = CLIP.src, name = CLIP.name;
  const dst = S.path + '/' + name;
  clearClip();
  try {
    await api('POST', '/api/copy', JSON.stringify({src, dst}));
    toast('Copied "'+name+'" here', 'success');
    loadDir(S.path, false);
  } catch(e) { toast('Copy failed: '+e.message, 'error'); }
}
const SORT = { key: 'name', dir: 1 }; // key: name|date|size, dir: 1=asc -1=desc

// ── Util ───────────────────────────────────────────────────────────────────
const $ = id => document.getElementById(id);
function esc(s) {
  return String(s??'').replace(/&/g,'&amp;').replace(/</g,'&lt;')
    .replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}
const enc = encodeURIComponent;

const AUDIO   = new Set(['.wav','.aif','.aiff','.flac','.mp3','.ogg','.m4a','.opus','.aac']);
const ABLETON = new Set(['.als','.alc','.adg','.adv','.agr','.ams','.asd']);

function extBadge(ext) {
  if (!ext) return '';
  const cls = AUDIO.has(ext)||ABLETON.has(ext) ? ' audio' : '';
  return `<span class="ext-badge${cls}">${esc(ext.slice(1))}</span>`;
}
function fmtSize(b) {
  if (!b) return '';
  if (b<1024) return b+' B';
  if (b<1048576) return (b/1024).toFixed(1)+' KB';
  if (b<1073741824) return (b/1048576).toFixed(1)+' MB';
  return (b/1073741824).toFixed(2)+' GB';
}
function fmtDate(iso) {
  if (!iso) return '';
  const d=new Date(iso), now=new Date(), diff=now-d;
  if (diff<60000) return 'just now';
  if (diff<3600000) return Math.floor(diff/60000)+'m ago';
  if (diff<172800000) return Math.floor(diff/3600000)+'h ago';
  if (d.getFullYear()===now.getFullYear())
    return d.toLocaleDateString(undefined,{month:'short',day:'numeric'});
  return d.toLocaleDateString(undefined,{year:'2-digit',month:'short',day:'numeric'});
}

// ── Icons ──────────────────────────────────────────────────────────────────
const BASE = '/api/assets/Browser/';
function entryIcon(entry) {
  if (entry.is_dir) {
    const n = entry.name.toLowerCase();
    if (n.endsWith(' project') || n.endsWith(' project files') || n.endsWith('.als project'))
      return BASE+'Sidebar_CurrentProject.png';
    return BASE+'Sidebar_Folder.png';
  }
  const ext = (entry.extension||'').toLowerCase();
  if (ext === '.als') return BASE+'Set.png';
  if (ext === '.asd') return BASE+'DefaultSet.png';
  if (AUDIO.has(ext)) return BASE+'Audio.png';
  return null;
}
function iconHTML(entry) {
  const src = entryIcon(entry);
  if (!src) return '<div class="entry-icon"></div>';
  return `<div class="entry-icon"><img src="${src}" loading="lazy" alt=""></div>`;
}

// ── Sort ───────────────────────────────────────────────────────────────────
function sortEntries(entries) {
  return [...entries].sort((a, b) => {
    if (SORT.key === 'date') {
      return SORT.dir * (new Date(a.mod_time) - new Date(b.mod_time));
    }
    if (SORT.key === 'size') {
      return SORT.dir * ((a.size||0) - (b.size||0));
    }
    if (SORT.key === 'type') {
      if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
      return SORT.dir * (a.extension||'').localeCompare(b.extension||'');
    }
    // name: dirs first, then alpha
    if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
    return SORT.dir * a.name.toLowerCase().localeCompare(b.name.toLowerCase());
  });
}
function updateSortBar() {
  document.querySelectorAll('.sort-btn').forEach(btn => {
    const k = btn.dataset.sort;
    const active = k === SORT.key;
    btn.classList.toggle('active', active);
    const arrow = btn.querySelector('.sort-dir');
    if (active) {
      if (!arrow) btn.innerHTML += ' <span class="sort-dir">'+(SORT.dir===1?'↑':'↓')+'</span>';
      else arrow.textContent = SORT.dir===1?'↑':'↓';
    } else if (arrow) arrow.remove();
  });
}

// ── API ────────────────────────────────────────────────────────────────────
async function api(method, url, body) {
  const opts = {method};
  if (body instanceof FormData) { opts.body = body; }
  else if (body !== undefined && body !== null) {
    opts.body = typeof body === 'string' ? body : JSON.stringify(body);
    opts.headers = {'Content-Type':'application/json'};
  }
  const res = await fetch(url, opts);
  if (!res.ok) throw new Error(await res.text().catch(()=>`HTTP ${res.status}`));
  const ct = res.headers.get('content-type')||'';
  return ct.includes('json') ? res.json() : res;
}

// ── Toast ──────────────────────────────────────────────────────────────────
function toast(msg, type='info') {
  const icons = {success:'✓', error:'✕', info:'·'};
  const el = document.createElement('div');
  el.className = `toast ${type}`;
  el.innerHTML = `<span class="t-icon">${icons[type]||'·'}</span><span class="t-msg">${esc(msg)}</span>`;
  $('toast-wrap').appendChild(el);
  setTimeout(()=>{ el.classList.add('t-out'); setTimeout(()=>el.remove(),180); }, 3200);
}

// ── Sheet ──────────────────────────────────────────────────────────────────
function openSheet(path, name, isDir) {
  S.pendingDelete = path;
  S.pendingDeleteIsDir = !!isDir;
  const body = $('sh-body');
  if (isDir) {
    body.innerHTML = `Permanently delete <span class="sh-name">${esc(name)}</span> and <strong>all its contents</strong>`;
  } else {
    body.innerHTML = `Permanently delete <span class="sh-name">${esc(name)}</span>`;
  }
  $('overlay').classList.add('open');
}
function closeSheet(e) {
  if (e && e.target !== $('overlay')) return;
  $('overlay').classList.remove('open');
  S.pendingDelete = null;
}
async function doDelete() {
  const path = S.pendingDelete;
  $('overlay').classList.remove('open');
  S.pendingDelete = null;
  if (!path) return;
  try {
    const qs = S.pendingDeleteIsDir ? '&recursive=true' : '';
    await api('DELETE', `/api/delete?path=${enc(path)}${qs}`);
    toast('Deleted', 'success');
    await loadDir(S.path);
  } catch(e) { toast('Delete failed: '+e.message, 'error'); }
}

// ── Rename sheet ───────────────────────────────────────────────────────────
function openRenameSheet(path, name) {
  S.renameInfo = { path };
  $('rename-input').value = name;
  $('rename-overlay').classList.add('open');
  setTimeout(() => { $('rename-input').focus(); $('rename-input').select(); }, 150);
}
function closeRenameSheet(e) {
  if (e && e.target !== $('rename-overlay')) return;
  $('rename-overlay').classList.remove('open');
  S.renameInfo = null;
}
async function doRename() {
  const info = S.renameInfo;
  $('rename-overlay').classList.remove('open');
  S.renameInfo = null;
  if (!info) return;
  const newName = $('rename-input').value.trim();
  if (!newName || newName.includes('/')) { toast('Invalid name', 'error'); return; }
  const dir = info.path.substring(0, info.path.lastIndexOf('/'));
  const newPath = dir + '/' + newName;
  try {
    await api('POST', '/api/rename', JSON.stringify({old: info.path, new: newPath}));
    toast('Renamed to "'+newName+'"', 'success');
    await loadDir(S.path, false);
  } catch(e) { toast('Rename failed: '+e.message, 'error'); }
}

// ── Stats ──────────────────────────────────────────────────────────────────
let _statsTimer = null;

function showStats() {
  history.pushState({view:'stats'}, '');
  show('stats');
  $('btn-back').className = 'visible';
  $('header-sep').style.display = 'none';
  $('header-nav').style.display = 'none';
}
function startStatsPolling() {
  loadStats();
  _statsTimer = setInterval(loadStats, 3000);
}
function stopStatsPolling() {
  if (_statsTimer) { clearInterval(_statsTimer); _statsTimer = null; }
}
async function loadStats() {
  try {
    const s = await api('GET', '/api/stats');
    renderStats(s);
  } catch(e) {
    $('stats-grid').innerHTML = `<div class="empty-state"><strong>Error</strong>${esc(e.message)}</div>`;
  }
}
function fmtBytes(b) {
  if (!b) return '—';
  if (b < 1073741824) return (b/1048576).toFixed(1)+' MB';
  return (b/1073741824).toFixed(2)+' GB';
}
function fmtUptime(s) {
  const h = Math.floor(s/3600), m = Math.floor((s%3600)/60);
  return h > 24 ? Math.floor(h/24)+'d '+(h%24)+'h' : h+'h '+m+'m';
}
function mkBar(pct, warn=75, crit=90) {
  const cls = pct>=crit ? 'crit' : pct>=warn ? 'warn' : '';
  return `<div class="stat-bar-wrap"><div class="stat-bar ${cls}" style="width:${Math.min(pct,100)}%"></div></div>`;
}
function renderStats(s) {
  const rows = [];
  const cpu = typeof s.cpu_percent === 'number' ? s.cpu_percent : 0;
  const procRows = (s.top_procs||[]).map(p =>
    `<div class="stat-proc"><span class="stat-proc-name">${esc(p.name)}</span><span class="stat-proc-cpu">${p.cpu.toFixed(1)}%</span></div>`
  ).join('');
  rows.push(`<div class="stat-row"><div class="stat-label">CPU</div><div class="stat-body">
    <div class="stat-val">${cpu.toFixed(1)}%</div>${mkBar(cpu,60,85)}${procRows}</div></div>`);
  if (s.memory) {
    const pct = Math.round(s.memory.used/s.memory.total*100);
    rows.push(`<div class="stat-row"><div class="stat-label">Memory</div><div class="stat-body">
      <div class="stat-val">${fmtBytes(s.memory.used)} / ${fmtBytes(s.memory.total)} · ${pct}%</div>${mkBar(pct)}</div></div>`);
  }
  if (s.disk) {
    const pct = Math.round(s.disk.used/s.disk.total*100);
    rows.push(`<div class="stat-row"><div class="stat-label">Storage</div><div class="stat-body">
      <div class="stat-val">${fmtBytes(s.disk.used)} / ${fmtBytes(s.disk.total)} · ${pct}%</div>${mkBar(pct,80,92)}</div></div>`);
  }
  if (s.battery) {
    const pct = s.battery.percent;
    const warn = s.battery.status==='Charging' ? 101 : 20;
    rows.push(`<div class="stat-row"><div class="stat-label">Battery</div><div class="stat-body">
      <div class="stat-val">${pct}% · ${esc(s.battery.status)}</div>${mkBar(pct,warn,10)}</div></div>`);
  }
  if (s.ip_addresses && s.ip_addresses.length) {
    const pwLine = s.hotspot_password
      ? `<div class="stat-val stat-hotspot">Hotspot PW: <span class="hotspot-pw">${esc(s.hotspot_password)}</span></div>`
      : '';
    rows.push(`<div class="stat-row"><div class="stat-label">Network</div><div class="stat-body">
      ${s.ip_addresses.map(ip=>`<div class="stat-val">${esc(ip)}</div>`).join('')}${pwLine}</div></div>`);
  }
  if (s.uptime_seconds) {
    rows.push(`<div class="stat-row"><div class="stat-label">Uptime</div><div class="stat-body">
      <div class="stat-val">${fmtUptime(s.uptime_seconds)}</div></div></div>`);
  }
  $('stats-grid').innerHTML = rows.join('');
  const now = new Date().toLocaleTimeString(undefined,{hour:'2-digit',minute:'2-digit',second:'2-digit'});
  $('stats-refresh').textContent = 'Updated '+now+' · auto-refresh every 3s';
}

// ── Header ─────────────────────────────────────────────────────────────────
function renderHeader() {
  const inDir = S.crumbs.length > 0;
  $('header-sep').style.display = inDir ? '' : 'none';
  $('header-nav').style.display = inDir ? ''  : 'none';
  $('btn-back').className = inDir ? 'visible' : '';

  const nav = $('header-nav');
  nav.innerHTML = '';
  S.crumbs.forEach((c, i) => {
    if (i > 0) {
      const sep = document.createElement('span');
      sep.className = 'crumb-sep'; sep.textContent = '›';
      nav.appendChild(sep);
    }
    const span = document.createElement('span');
    span.className = 'crumb' + (i===S.crumbs.length-1 ? ' active' : '');
    span.textContent = c.name;
    if (i < S.crumbs.length-1) span.onclick = () => { S.crumbs=S.crumbs.slice(0,i+1); loadDir(c.path); };
    nav.appendChild(span);
  });
  nav.scrollLeft = nav.scrollWidth;
}

function goBack() {
  history.back(); // popstate handler does the actual UI change
}

// ── Views ──────────────────────────────────────────────────────────────────
function show(v) {
  $('view-home').style.display    = v==='home'    ? '' : 'none';
  $('view-dir').style.display     = v==='dir'     ? '' : 'none';
  $('view-stats').style.display   = v==='stats'   ? '' : 'none';
  $('view-display').style.display = v==='display' ? '' : 'none';
  $('view-midi').style.display    = v==='midi'    ? '' : 'none';
  $('view-browser').style.display = v==='browser' ? '' : 'none';
  $('btn-stats').classList.toggle('active', v==='stats');
  $('btn-display').classList.toggle('active', v==='display');
  $('btn-browser').classList.toggle('active', v==='browser');
  if (v === 'stats')   startStatsPolling();   else stopStatsPolling();
  if (v === 'display') startDisplayPolling(); else stopDisplayPolling();
  if (v === 'midi')    startMidiStream();      else stopMidiStream();
  if (v === 'browser') brInit();
}

// ── Root cards ─────────────────────────────────────────────────────────────
const ROOT_DESCS = {
  Music: 'Projects, samples and presets',
  Ableton: 'Ableton Live user library',
  Recordings: 'Audio recorded on Push',
};

function renderRoots(roots) {
  S.path=null; S.crumbs=[];
  renderHeader(); show('home');
  const grid = $('roots-grid');
  grid.innerHTML = '';
  roots.forEach(r => {
    const name = r.name || r.path.split('/').filter(Boolean).pop();
    const btn = document.createElement('button');
    btn.className = 'root-card' + (r.exists?'':' unavailable');
    const icon = r.removable
      ? `<img src="/api/assets/Browser/Sidebar_Computer.png" style="width:22px;opacity:.7;margin-right:10px;flex-shrink:0">`
      : `<img src="/api/assets/Browser/Sidebar_Folder.png" style="width:22px;opacity:.7;margin-right:10px;flex-shrink:0">`;
    btn.innerHTML = `
      ${icon}
      <div class="root-card-body">
        <span class="root-card-name">${esc(name)}</span>
        <span class="root-card-path">${esc(r.exists ? r.path : 'Not available')}</span>
      </div>
      ${r.removable ? `<button class="root-card-eject" title="Unmount">⏏</button>` : ''}
      <span class="root-card-arrow">›</span>`;
    if (r.exists) btn.onclick = () => navigate(r.path, name);
    if (r.removable) {
      btn.querySelector('.root-card-eject').onclick = async e => {
        e.stopPropagation();
        try {
          await api('POST', `/api/unmount?path=${enc(r.path)}`);
          toast('Unmounted '+name, 'success');
          loadRoots(false);
        } catch(err) { toast('Unmount failed: '+err.message, 'error'); }
      };
    }
    grid.appendChild(btn);
  });
}

// ── Skeletons ──────────────────────────────────────────────────────────────
function renderSkeletons() {
  $('file-list').innerHTML = Array.from({length:8}, ()=>`
    <div class="skel-row">
      <div class="skel skel-a"></div>
      <div class="skel skel-b"></div>
    </div>`).join('');
}

// ── Directory render ────────────────────────────────────────────────────────
let _lastEntries = [];

function renderDir(entries) {
  _lastEntries = entries;
  $('upload-bar').classList.add('visible');
  updateSortBar();
  const sorted = sortEntries(entries);
  const fl = $('file-list');
  if (!sorted.length) {
    fl.innerHTML = `<div class="empty-state"><strong>Empty folder</strong>Upload files using the buttons above.</div>`;
    return;
  }

  fl.innerHTML = '';
  // When sorting by date or size, render one flat list; otherwise dirs+files sections
  const useSections = SORT.key === 'name' || SORT.key === 'type';
  const groups = useSections
    ? [{items: sorted.filter(e=>e.is_dir), label:'Folders'},
       {items: sorted.filter(e=>!e.is_dir), label:'Files'}]
    : [{items: sorted, label:''}];

  groups.forEach(({items, label}) => {
    if (!items.length) return;
    const sec = document.createElement('div');
    sec.className = 'list-section';
    if (label) sec.innerHTML = `<div class="list-label">${label}</div>`;

    items.forEach(entry => {
      const ext = (entry.extension||'').toLowerCase();
      const row = document.createElement('div');
      row.className = 'file-row';

      if (entry.is_dir) {
        row.innerHTML = `
          ${iconHTML(entry)}
          <div class="row-info">
            <div class="row-name">${esc(entry.name)}</div>
            ${entry.mod_time ? `<div class="row-meta"><span>${esc(fmtDate(entry.mod_time))}</span></div>` : ''}
          </div>
          <div class="row-actions">
            <button class="act-btn" data-action="copy"   title="Copy">⧉</button>
            <button class="act-btn" data-action="rename" title="Rename">✎</button>
            <button class="act-btn danger" data-action="delete" title="Delete folder">×</button>
            <a class="act-btn" href="/api/download?path=${enc(entry.path)}" download="${esc(entry.name)}.zip" title="Download as ZIP">⤓</a>
          </div>
          <span class="folder-arrow">›</span>`;
        row.querySelector('.row-info').onclick = () => navigate(entry.path, entry.name);
        row.querySelector('[data-action="copy"]').onclick = e => {
          e.stopPropagation(); copyToClip(entry.path, entry.name);
        };
        row.querySelector('[data-action="rename"]').onclick = e => {
          e.stopPropagation(); openRenameSheet(entry.path, entry.name);
        };
        row.querySelector('[data-action="delete"]').onclick = e => {
          e.stopPropagation(); openSheet(entry.path, entry.name, true);
        };
        row.querySelector('a.act-btn').onclick = e => {
          e.stopPropagation(); toast('Zipping "'+entry.name+'"…', 'info');
        };
      } else {
        row.innerHTML = `
          ${iconHTML(entry)}
          <div class="row-info">
            <div class="row-name">${esc(entry.name)}${extBadge(ext)}</div>
            <div class="row-meta">
              ${entry.size ? `<span>${esc(fmtSize(entry.size))}</span>` : ''}
              ${entry.mod_time ? `<span>${esc(fmtDate(entry.mod_time))}</span>` : ''}
            </div>
          </div>
          <div class="row-actions">
            <button class="act-btn" data-action="copy"   title="Copy">⧉</button>
            <button class="act-btn" data-action="rename" title="Rename">✎</button>
            <a class="act-btn" href="/api/download?path=${enc(entry.path)}" download="${esc(entry.name)}" title="Download">↓</a>
            <button class="act-btn danger" data-action="delete" title="Delete">×</button>
          </div>`;
        row.querySelector('.row-info').onclick = () => {
          const a = document.createElement('a');
          a.href = `/api/download?path=${enc(entry.path)}`;
          a.download = entry.name;
          document.body.appendChild(a); a.click(); a.remove();
          toast('Downloading '+entry.name, 'info');
        };
        row.querySelector('[data-action="copy"]').onclick = e => {
          e.stopPropagation(); copyToClip(entry.path, entry.name);
        };
        row.querySelector('[data-action="rename"]').onclick = e => {
          e.stopPropagation(); openRenameSheet(entry.path, entry.name);
        };
        row.querySelector('[data-action="delete"]').onclick = e => {
          e.stopPropagation(); openSheet(entry.path, entry.name, false);
        };
      }
      sec.appendChild(row);
    });
    fl.appendChild(sec);
  });
}

// ── Navigation ─────────────────────────────────────────────────────────────
async function loadRoots(pushHistory=true) {
  S.crumbs=[]; S.path=null;
  if (pushHistory) history.pushState({view:'home'}, '');
  renderHeader(); show('home');
  $('roots-grid').innerHTML = '<div style="padding:32px 0;color:var(--grey-mid);font-size:14px">Loading…</div>';
  try { renderRoots(await api('GET','/api/roots')); }
  catch(e) { $('roots-grid').innerHTML=`<div class="empty-state"><strong>Connection error</strong>${esc(e.message)}</div>`; }
}

async function navigate(path, name) {
  S.crumbs.push({path, name});
  history.pushState({view:'dir', path, crumbs: JSON.parse(JSON.stringify(S.crumbs))}, '');
  await loadDir(path, false);
}

async function loadDir(path, pushHistory=true) {
  S.path = path;
  if (pushHistory) history.pushState({view:'dir', path, crumbs: JSON.parse(JSON.stringify(S.crumbs))}, '');
  renderHeader(); show('dir');
  $('upload-bar').classList.remove('visible');
  renderSkeletons();
  try {
    const entries = await api('GET', `/api/list?path=${enc(path)}`);
    renderDir(entries);
  } catch(e) {
    $('file-list').innerHTML=`<div class="empty-state"><strong>Error</strong>${esc(e.message)}</div>`;
  }
}

// ── History (browser back/forward) ─────────────────────────────────────────
window.addEventListener('popstate', async e => {
  const state = e.state || {view:'home'};
  if (state.view === 'dir') {
    S.crumbs = state.crumbs || [];
    S.path   = state.path;
    renderHeader(); show('dir');
    $('upload-bar').classList.remove('visible');
    renderSkeletons();
    try {
      const entries = await api('GET', `/api/list?path=${enc(state.path)}`);
      renderDir(entries);
    } catch(err) {
      $('file-list').innerHTML=`<div class="empty-state"><strong>Error</strong>${esc(err.message)}</div>`;
    }
  } else if (state.view === 'stats') {
    show('stats');
    $('btn-back').className = 'visible';
    $('header-sep').style.display = 'none';
    $('header-nav').style.display = 'none';
  } else if (state.view === 'display') {
    show('display');
    $('btn-back').className = 'visible';
    $('header-sep').style.display = 'none';
    $('header-nav').style.display = 'none';
  } else if (state.view === 'midi') {
    show('midi');
    $('btn-back').className = 'visible';
    $('header-sep').style.display = 'none';
    $('header-nav').style.display = 'none';
  } else if (state.view === 'browser') {
    show('browser');
    $('btn-back').className = 'visible';
    $('header-sep').style.display = 'none';
    $('header-nav').style.display = 'none';
  } else {
    S.crumbs=[]; S.path=null;
    renderHeader(); show('home');
    if (!$('roots-grid').querySelector('.root-card')) loadRoots(false);
  }
});

// ── Upload ─────────────────────────────────────────────────────────────────
async function uploadFiles(files, isFolder) {
  if (!S.path) { toast('Navigate into a folder first','error'); return; }
  if (!files?.length) return;

  const progress = $('upload-progress');
  const bar      = $('upload-progress-bar');
  progress.classList.add('visible');
  bar.style.width = '0%';

  let done = 0;
  const total = files.length;

  for (const file of files) {
    const fd = new FormData();
    fd.append('file', file);
    // For folder uploads, include relative path to preserve directory structure
    if (isFolder && file.webkitRelativePath) {
      fd.append('relativePath', file.webkitRelativePath);
    }
    try {
      await api('POST', `/api/upload?path=${enc(S.path)}`, fd);
      done++;
      bar.style.width = (done/total*100)+'%';
    } catch(e) { toast('Failed: '+file.name+' — '+e.message,'error'); }
  }

  setTimeout(()=>{ progress.classList.remove('visible'); bar.style.width='0%'; }, 600);

  const label = isFolder ? 'folder' : (total===1?'file':'files');
  toast(`Uploaded ${done} of ${total} ${label}`, done===total?'success':'info');
  await loadDir(S.path);
}

// ── Upload buttons ─────────────────────────────────────────────────────────
$('btn-upload-files').onclick  = () => { if (!S.path) { toast('Navigate into a folder first','info'); return; } $('file-input').click(); };
$('btn-upload-folder').onclick = () => { if (!S.path) { toast('Navigate into a folder first','info'); return; } $('folder-input').click(); };
$('file-input').onchange   = () => { const f=Array.from($('file-input').files);   $('file-input').value='';   uploadFiles(f, false); };
$('folder-input').onchange = () => { const f=Array.from($('folder-input').files); $('folder-input').value=''; uploadFiles(f, true);  };

$('rename-input').addEventListener('keydown', e => {
  if (e.key === 'Enter') doRename();
  if (e.key === 'Escape') closeRenameSheet();
});

// ── Sort bar ───────────────────────────────────────────────────────────────
document.querySelectorAll('.sort-btn').forEach(btn => {
  btn.addEventListener('click', () => {
    const k = btn.dataset.sort;
    if (SORT.key === k) {
      SORT.dir *= -1;
    } else {
      SORT.key = k;
      SORT.dir = k === 'date' ? -1 : 1; // date: newest-first; all others: asc
    }
    if (_lastEntries.length) renderDir(_lastEntries);
  });
});

// ── Display view ───────────────────────────────────────────────────────────
let _dispTimer    = null;
let _dispCanvas   = null;
let _dispCtx      = null;
let _dispLastMode = -1;
let _dispImgData  = null; // last uploaded image as ImageData
let _dispDzBound  = false;
let _dispUiMode   = 'off'; // 'off'|'debug'|'image'|'video'

// ── Video streaming ─────────────────────────────────────────────────────────
let _vidStreaming    = false;
let _vidSource      = null;   // <video> or <img> element
let _vidCanvas      = null;
let _vidCtx         = null;
let _vidQuality     = 0.7;
let _vidFpsCnt      = 0;
let _vidStreamTimer = null;   // 66ms send interval
let _vidFpsTimer    = null;   // 1000ms fps display
let _vidFileBound   = false;

// Draw tab state
let _drawCanvas      = null;
let _drawCtx         = null;
let _drawColor       = '#FF5500';
let _drawSize        = 1;
let _drawEraser      = false;
let _drawPainting    = false;
let _drawLastX       = 0;
let _drawLastY       = 0;
let _drawDirty       = false;
let _drawStreamTimer = null;
let _drawBound       = false;

function showDisplay() {
  history.pushState({view:'display'}, '');
  show('display');
  $('btn-back').className = 'visible';
  $('header-sep').style.display = 'none';
  $('header-nav').style.display = 'none';
  $('btn-display').classList.add('active');
}

// ── MIDI Monitor ────────────────────────────────────────────────────────────
let _midiES        = null;  // EventSource
let _midiCntIn     = 0;
let _midiCntOut    = 0;
let _midiConnected = false;

function showMidi() {
  history.pushState({view:'midi'}, '');
  show('midi');
  $('btn-back').className = 'visible';
  $('header-sep').style.display = 'none';
  $('header-nav').style.display = 'none';
  $('btn-midi').classList.add('active');
}

function midiMsgClass(decoded) {
  if (!decoded) return '';
  const d = decoded.toLowerCase();
  if (d.startsWith('note on'))  return 'note-on';
  if (d.startsWith('note off')) return 'note-off';
  if (d.startsWith('cc'))       return 'cc';
  if (d.startsWith('sysex'))    return 'sysex';
  return '';
}

function midiFilterChanged() { /* no-op: filter applied on ingest */ }

async function midiInterceptChanged() {
  const enabled = $('midi-intercept').checked;
  try {
    await api('POST', '/api/midi/filter', JSON.stringify({enabled}));
    toast(enabled ? 'MIDI intercepted — Live will not receive pad/button events' : 'MIDI intercept off — Live receives all events', enabled ? 'info' : 'success');
  } catch(e) {
    $('midi-intercept').checked = !enabled; // revert
    toast('Intercept failed: ' + esc(e.message), 'error');
  }
}

async function loadMidiInterceptState() {
  try {
    const s = await api('GET', '/api/midi/filter/status');
    if (s && $('midi-intercept')) {
      $('midi-intercept').checked = !!s.enabled;
      if (!s.available) $('midi-intercept').disabled = true;
    }
  } catch(_) {}
}

async function midiForwardChanged() {
  const enabled = $('midi-forward').checked;
  try {
    await api('POST', '/api/midi/forward', JSON.stringify({enabled}));
    toast(enabled ? 'Forwarding MIDI to external port (16:2)' : 'MIDI forwarding off', enabled ? 'info' : 'success');
  } catch(e) {
    $('midi-forward').checked = !enabled;
    toast('Forward failed: ' + esc(e.message), 'error');
  }
}

async function loadMidiForward() {
  try {
    const d = await api('GET', '/api/midi/forward');
    if (d && $('midi-forward')) $('midi-forward').checked = !!d.enabled;
  } catch(_) {}
}

async function loadMidiPorts() {
  try {
    const ports = await api('GET', '/api/midi/ports');
    const sel = $('midi-port-sel');
    if (!sel || !Array.isArray(ports)) return;
    const prev = sel.value;
    sel.innerHTML = '';
    if (ports.length === 0) {
      sel.innerHTML = '<option value="">No readable ports</option>';
      return;
    }
    ports.forEach(p => {
      const opt = document.createElement('option');
      opt.value = JSON.stringify({client: p.client, port: p.port});
      opt.textContent = p.name + ' (' + p.client + ':' + p.port + ')';
      opt.selected = p.active;
      sel.appendChild(opt);
    });
    // If previously selected port still exists, restore selection
    if (prev) {
      for (const opt of sel.options) {
        if (opt.value === prev) { opt.selected = true; break; }
      }
    }
  } catch(_) {}
}

// ── LED control ────────────────────────────────────────────────────────────

function ledTypeChanged() {
  const isNote = $('led-type').value === 'note';
  $('led-cc-label').style.display   = isNote ? 'none' : 'flex';
  $('led-note-label').style.display = isNote ? 'flex' : 'none';
}

async function sendLed() {
  const type = $('led-type').value;
  const ch   = parseInt($('led-channel').value, 10);
  const val  = parseInt($('led-value').value, 10);
  const body = type === 'cc'
    ? { type: 'cc',   channel: ch, cc:   parseInt($('led-cc').value, 10),   value:    val }
    : { type: 'note', channel: ch, note: parseInt($('led-note').value, 10), velocity: val };
  try {
    await api('POST', '/api/midi/led', body);
  } catch(e) {
    toast('LED send failed: ' + esc(e.message), 'error');
  }
}

async function sendLedOff() {
  const type = $('led-type').value;
  const ch   = parseInt($('led-channel').value, 10);
  const body = type === 'cc'
    ? { type: 'cc',   channel: ch, cc:   parseInt($('led-cc').value, 10),   value:    0 }
    : { type: 'note', channel: ch, note: parseInt($('led-note').value, 10), velocity: 0 };
  try {
    await api('POST', '/api/midi/led', body);
  } catch(e) {
    toast('LED off failed: ' + esc(e.message), 'error');
  }
}


// ── LED mode config ─────────────────────────────────────────────────────────

function lcfgModeChanged() {
  const isExclusive = $('lcfg-mode').value === 'exclusive';
  $('lcfg-group-label').style.display = isExclusive ? 'flex' : 'none';
}

function lcfgAnimChanged() {
  const hasAnim = $('lcfg-anim-type').value !== '';
  $('lcfg-anim-speed').style.display      = hasAnim ? '' : 'none';
  $('lcfg-anim-color-label').style.display = hasAnim ? 'flex' : 'none';
}

function parseColor(s) {
  return /^\d+$/.test(s) ? parseInt(s, 10) : s;
}

// ── Palette picker ──────────────────────────────────────────────────────────

let _palette = null; // cached [{index,r,g,b,w,hex}]
let _paletteOpen = false;

async function paletteToggle() {
  _paletteOpen = !_paletteOpen;
  const el = $('palette-picker');
  const btn = $('palette-toggle-btn');
  el.style.display = _paletteOpen ? '' : 'none';
  btn.textContent = _paletteOpen ? 'palette ▴' : 'palette ▾';
  if (_paletteOpen && !_palette) await paletteFetch();
}

async function paletteFetch() {
  $('palette-status').textContent = 'Querying hardware (~1s)…';
  try {
    _palette = await api('GET', '/api/midi/palette');
    paletteRender();
    $('palette-status').textContent = `${_palette.length} entries — hover for index`;
  } catch(e) {
    $('palette-status').textContent = 'Error: ' + e.message;
  }
}

function paletteRender() {
  if (!_palette) return;
  const grid = $('palette-swatches');
  grid.innerHTML = _palette.map(e => {
    // Decide text contrast colour for index label
    const luma = 0.299 * e.r + 0.587 * e.g + 0.114 * e.b;
    const fg = luma > 80 ? '#000' : '#fff';
    return `<div title="${e.index}: ${e.hex}"
      style="width:18px;height:18px;background:${e.hex};border-radius:2px;
             border:1px solid rgba(128,128,128,.25);position:relative;
             display:flex;align-items:center;justify-content:center"
      onmouseenter="$('palette-hover-label').textContent='[${e.index}] ${e.hex}  r=${e.r} g=${e.g} b=${e.b}'"
      onmouseleave="$('palette-hover-label').textContent=''"
      onclick="paletteSelect(${e.index})">
    </div>`;
  }).join('');
}

function paletteSelect(idx) {
  // Set whichever color input is currently relevant
  const lcfgColor = $('lcfg-color');
  if (lcfgColor) lcfgColor.value = idx;
  // Also update the manual send value if visible
  const manualVal = $('led-value');
  if (manualVal) manualVal.value = idx;
}

async function lcfgLoad() {
  try {
    const data = await api('GET', '/api/midi/led/config');
    const configs = data.configs || {};
    const rows = $('lcfg-rows');
    const list = $('lcfg-list');
    const entries = Object.entries(configs);
    if (entries.length === 0) { list.style.display = 'none'; return; }
    list.style.display = '';
    rows.innerHTML = entries
      .sort((a, b) => parseInt(a[0]) - parseInt(b[0]))
      .map(([cc, cfg]) => {
        const group = cfg.group     ? ` <span style="color:var(--grey-mid)">group:${esc(cfg.group)}</span>` : '';
        const anim  = cfg.anim_type ? ` <span style="color:var(--blue)">${esc(cfg.anim_type)}@${esc(cfg.anim_speed)}→${cfg.anim_color}</span>` : '';
        return `<div style="display:flex;align-items:center;gap:8px;padding:2px 0">` +
          `<span style="color:var(--fg);min-width:36px">CC${cc}</span>` +
          `<span style="color:var(--orange)">${esc(cfg.mode)}</span>` +
          `<span style="color:var(--grey-mid)">color:${cfg.color}</span>` +
          group + anim +
          `<button onclick="lcfgDeleteCC(${cc})" style="margin-left:auto;background:none;color:var(--grey-mid);border:none;font-size:11px;cursor:pointer;padding:0 4px">✕</button>` +
          `</div>`;
      }).join('');
  } catch(e) {
    // silent
  }
}

async function lcfgSet() {
  const cc        = parseInt($('lcfg-cc').value, 10);
  const mode      = $('lcfg-mode').value;
  const color     = parseColor($('lcfg-color').value.trim());
  const group     = $('lcfg-group').value.trim();
  const animType  = $('lcfg-anim-type').value;
  const animSpeed = $('lcfg-anim-speed').value;
  const animColor = parseColor($('lcfg-anim-color').value.trim());
  const body = { cc, mode, color };
  if (mode === 'exclusive' && group) body.group = group;
  if (animType) { body.anim_type = animType; body.anim_speed = animSpeed; body.anim_color = animColor; }
  try {
    await api('POST', '/api/midi/led/config', body);
    toast(`CC${cc} → ${mode}${animType ? ' + ' + animType : ''}`, 'success');
    lcfgLoad();
  } catch(e) {
    toast('Config failed: ' + esc(e.message), 'error');
  }
}

async function lcfgClearOne() {
  const cc = parseInt($('lcfg-cc').value, 10);
  try {
    await api('DELETE', '/api/midi/led/config?cc=' + cc);
    toast(`CC${cc} config cleared`, 'info');
    lcfgLoad();
  } catch(e) {
    toast('Clear failed: ' + esc(e.message), 'error');
  }
}

async function lcfgDeleteCC(cc) {
  try {
    await api('DELETE', '/api/midi/led/config?cc=' + cc);
    lcfgLoad();
  } catch(e) {
    toast('Clear failed: ' + esc(e.message), 'error');
  }
}

async function lcfgClearAll() {
  try {
    await api('DELETE', '/api/midi/led/config');
    toast('All LED configs cleared', 'info');
    lcfgLoad();
  } catch(e) {
    toast('Clear all failed: ' + esc(e.message), 'error');
  }
}

// ── MIDI mapping (remap) ─────────────────────────────────────────────────────

let _remapLearning = false;

function remapIsEncoderCC(cc) { return (cc >= 71 && cc <= 79) || cc === 14; }

function remapSrcTypeChanged() {
  $('remap-src-num-label').textContent = $('remap-src-type').value === 'note' ? 'Note#' : 'CC#';
}
function remapOutTypeChanged() {
  $('remap-out-num-label').textContent = $('remap-out-type').value === 'note' ? 'Note#' : 'CC#';
}

function remapLearnToggle() {
  _remapLearning = !_remapLearning;
  const btn = $('remap-learn-btn');
  const st  = $('remap-learn-status');
  if (_remapLearning) {
    btn.textContent = '◉ Listening…';
    btn.style.background = 'var(--orange)';
    st.textContent = 'press/move a Push control';
  } else {
    btn.textContent = '◉ Learn';
    btn.style.background = 'var(--blue)';
    st.textContent = 'or enter manually →';
  }
}

// Called from the MIDI SSE handler for each inbound event while learning.
function remapLearnCapture(ev) {
  const b = ev.data;
  if (!Array.isArray(b) || b.length < 2) return;
  const status = b[0] & 0xF0, ch = b[0] & 0x0F;
  let type, num;
  if (status === 0xB0) { type = 'cc'; num = b[1]; }
  else if (status === 0x90 || status === 0x80) { type = 'note'; num = b[1]; }
  else return; // ignore clock, sysex, aftertouch, etc.
  $('remap-src-type').value = type;
  $('remap-src-ch').value = ch;
  $('remap-src-num').value = num;
  $('remap-relative').checked = (type === 'cc' && remapIsEncoderCC(num));
  remapSrcTypeChanged();
  remapLearnToggle(); // stop listening
  toast(`Learned ${type.toUpperCase()} ${num} (ch ${ch})`, 'success');
}

async function loadRemapOutPorts(selClient, selPort) {
  try {
    const ports = await api('GET', '/api/midi/ports?writable=1');
    const sel = $('remap-out-port');
    if (!sel || !Array.isArray(ports)) return;
    sel.innerHTML = '';
    if (ports.length === 0) {
      sel.innerHTML = '<option value="">No writable ports</option>';
      return;
    }
    ports.forEach(p => {
      const opt = document.createElement('option');
      opt.value = JSON.stringify({client: p.client, port: p.port});
      opt.textContent = p.name + ' (' + p.client + ':' + p.port + ')';
      if (selClient !== undefined && p.client === selClient && p.port === selPort) opt.selected = true;
      sel.appendChild(opt);
    });
  } catch(_) {}
}

async function loadRemapConfig() {
  try {
    const data = await api('GET', '/api/midi/mapping');
    $('remap-enabled').checked = !!data.enabled;
    $('remap-require-intercept').checked = !!data.require_intercept;
    await loadRemapOutPorts(data.out_client, data.out_port);
    remapRenderList(data.mappings || {});
  } catch(_) {}
}

function remapRenderList(mappings) {
  const rows = $('remap-rows');
  const list = $('remap-list');
  const entries = Object.entries(mappings);
  if (entries.length === 0) { list.style.display = 'none'; return; }
  list.style.display = '';
  rows.innerHTML = entries.sort((a, b) => a[0].localeCompare(b[0])).map(([key, m]) => {
    const src = `${m.src_type.toUpperCase()} ${m.src_num} ch${m.src_ch}${m.relative ? ' (rel)' : ''}`;
    const out = `${m.out_type.toUpperCase()} ${m.out_num} ch${m.out_ch} [${m.out_min}–${m.out_max}]`;
    return `<div style="display:flex;align-items:center;gap:8px;padding:2px 0">` +
      `<span style="color:var(--fg)">${esc(src)}</span>` +
      `<span style="color:var(--grey-mid)">→</span>` +
      `<span style="color:var(--orange)">${esc(out)}</span>` +
      `<button onclick="remapDelete('${esc(key)}')" style="margin-left:auto;background:none;color:var(--grey-mid);border:none;font-size:11px;cursor:pointer;padding:0 4px">✕</button>` +
      `</div>`;
  }).join('');
}

function remapSelectedPort() {
  const v = $('remap-out-port').value;
  if (!v) return { client: 0, port: 0 };
  try { return JSON.parse(v); } catch(_) { return { client: 0, port: 0 }; }
}

async function saveRemapConfig() {
  const p = remapSelectedPort();
  try {
    await api('POST', '/api/midi/mapping/config', {
      enabled: $('remap-enabled').checked,
      require_intercept: $('remap-require-intercept').checked,
      out_client: p.client,
      out_port: p.port,
    });
  } catch(e) {
    toast('Remap config failed: ' + esc(e.message), 'error');
  }
}

async function saveRemapMapping() {
  const body = {
    src_type: $('remap-src-type').value,
    src_ch:   parseInt($('remap-src-ch').value, 10) || 0,
    src_num:  parseInt($('remap-src-num').value, 10) || 0,
    relative: $('remap-relative').checked,
    out_type: $('remap-out-type').value,
    out_ch:   parseInt($('remap-out-ch').value, 10) || 0,
    out_num:  parseInt($('remap-out-num').value, 10) || 0,
    out_min:  parseInt($('remap-out-min').value, 10) || 0,
    out_max:  parseInt($('remap-out-max').value, 10) || 0,
  };
  try {
    await api('POST', '/api/midi/mapping', body);
    toast(`Mapped ${body.src_type.toUpperCase()} ${body.src_num} → ${body.out_type.toUpperCase()} ${body.out_num}`, 'success');
    loadRemapConfig();
  } catch(e) {
    toast('Save mapping failed: ' + esc(e.message), 'error');
  }
}

async function remapDelete(key) {
  try {
    await api('DELETE', '/api/midi/mapping?key=' + encodeURIComponent(key));
    loadRemapConfig();
  } catch(e) {
    toast('Delete failed: ' + esc(e.message), 'error');
  }
}

async function remapClearAll() {
  try {
    await api('DELETE', '/api/midi/mapping');
    toast('All mappings cleared', 'info');
    loadRemapConfig();
  } catch(e) {
    toast('Clear all failed: ' + esc(e.message), 'error');
  }
}

async function loadMidiChords() {
  try {
    const chords = await api('GET', '/api/midi/chords');
    const wrap = $('midi-chords');
    const list = $('midi-chords-list');
    if (!wrap || !list || !Array.isArray(chords) || chords.length === 0) return;
    list.innerHTML = chords.map(c =>
      `<span style="margin-right:16px;background:rgba(255,100,60,0.10);border:1px solid rgba(255,100,60,0.25);border-radius:3px;padding:1px 6px">` +
      `${esc(c.description)} <span style="opacity:0.6">(${c.ccs.map(n=>'CC'+n).join('+') })</span></span>`
    ).join('');
    wrap.style.display = '';
  } catch(_) {}
}

async function midiPortChanged() {
  const sel = $('midi-port-sel');
  if (!sel || !sel.value) return;
  try {
    const target = JSON.parse(sel.value);
    await api('POST', '/api/midi/subscribe', JSON.stringify(target));
    clearMidiLog();
    toast('MIDI source → ' + sel.options[sel.selectedIndex].textContent, 'success');
  } catch(e) {
    toast('Port switch failed: ' + esc(e.message), 'error');
  }
}

function appendMidiRow(ev) {
  const dec = ev.decoded || '';
  if ($('midi-hide-sensing').checked && dec === 'Active Sensing') return;
  if ($('midi-hide-sysex').checked   && dec.startsWith('SysEx'))  return;
  if ($('midi-hide-cc').checked      && dec.startsWith('CC '))    return;
  if ($('midi-hide-note').checked    && (dec.startsWith('Note On') || dec.startsWith('Note Off'))) return;
  if ($('midi-hide-chanpres').checked && dec.startsWith('Chan Pres')) return;
  const log = $('midi-log');
  const tsMs = ev.ts_ms != null ? ev.ts_ms.toFixed(0) : (ev.ts_us != null ? (ev.ts_us/1000).toFixed(0) : '?');
  const hex  = (ev.data||[]).map(b => b.toString(16).padStart(2,'0').toUpperCase()).join(' ');
  const cls  = midiMsgClass(ev.decoded);
  const isIn = ev.dir === 'IN';
  if (isIn) { _midiCntIn++;  $('midi-cnt-in').textContent  = _midiCntIn; }
  else       { _midiCntOut++; $('midi-cnt-out').textContent = _midiCntOut; }

  const row = document.createElement('div');
  row.className = 'midi-row';
  row.innerHTML =
    `<span class="midi-ts">${tsMs}</span>` +
    `<span class="midi-dir ${isIn?'in':'out'}">${esc(ev.dir)}</span>` +
    `<span class="midi-hex">${esc(hex)}</span>` +
    `<span class="midi-dec ${cls}">${esc(ev.decoded||'')}</span>`;
  log.appendChild(row);

  // Auto-scroll to bottom
  log.scrollTop = log.scrollHeight;

  // Cap to 500 rows to avoid memory growth
  while (log.children.length > 500) log.removeChild(log.firstChild);
}

function setMidiStatus(connected) {
  _midiConnected = connected;
  const dot = $('midi-dot');
  const txt = $('midi-status-txt');
  if (connected) {
    dot.className = 'midi-dot ok';
    txt.textContent = 'Connected · live';
  } else {
    dot.className = 'midi-dot err';
    txt.textContent = 'Not connected — check /dev/snd/seq and Push3 process';
  }
  $('btn-midi').classList.toggle('active', connected || $('view-midi').style.display !== 'none');
}

function startMidiStream() {
  if (_midiES) return; // already running
  loadMidiInterceptState();
  loadMidiForward();
  loadMidiPorts();
  loadMidiChords();
  lcfgLoad();
  loadRemapConfig();
  _midiES = new EventSource('/api/midi/stream');

  _midiES.addEventListener('connected', e => {
    setMidiStatus(e.data === 'true');
  });

  _midiES.onmessage = e => {
    try {
      const ev = JSON.parse(e.data);
      if (_remapLearning && ev.dir === 'IN') { remapLearnCapture(ev); return; }
      if (ev.dir === 'CHORD') {
        if (ev.decoded && ev.decoded.startsWith('intercept_toggle:')) {
          const enabled = ev.decoded.endsWith(':true');
          if ($('midi-intercept')) $('midi-intercept').checked = enabled;
          toast('Chord: Intercept ' + (enabled ? 'ON' : 'OFF'), 'success');
        }
        return;
      }
      appendMidiRow(ev);
    } catch(_) {}
  };

  _midiES.onerror = () => {
    setMidiStatus(false);
    // EventSource auto-reconnects — don't close it
  };
}

function stopMidiStream() {
  if (_midiES) { _midiES.close(); _midiES = null; }
  $('btn-midi').classList.remove('active');
}

function clearMidiLog() {
  $('midi-log').innerHTML = '';
  _midiCntIn = 0; _midiCntOut = 0;
  $('midi-cnt-in').textContent  = '0';
  $('midi-cnt-out').textContent = '0';
}

function showDispTab(tab) {
  $('dtab-control').style.display = tab === 'control' ? '' : 'none';
  $('dtab-draw').style.display    = tab === 'draw'    ? '' : 'none';
  document.querySelectorAll('.disp-tab').forEach(b =>
    b.classList.toggle('active', b.dataset.tab === tab));
  if (tab === 'draw') {
    initDrawCanvas();
    startDrawStream();
  } else {
    stopDrawStream();
  }
}

function startDisplayPolling() {
  if (!_dispCanvas) {
    _dispCanvas = $('push-canvas');
    _dispCtx    = _dispCanvas.getContext('2d');
    renderDispCanvas(0); // initial state
  }
  if (!_dispDzBound) {
    _dispDzBound = true;
    const dz = $('drop-zone');
    dz.addEventListener('dragover',  e => { e.preventDefault(); dz.classList.add('drag-over'); });
    dz.addEventListener('dragleave', ()  => dz.classList.remove('drag-over'));
    dz.addEventListener('drop', e => {
      e.preventDefault(); dz.classList.remove('drag-over');
      const f = e.dataTransfer.files[0];
      if (f) uploadDisplayImage(f);
    });
    $('disp-img-input').addEventListener('change', e => {
      if (e.target.files[0]) uploadDisplayImage(e.target.files[0]);
      e.target.value = '';
    });

    // Video drop zone
    const vdz = $('vid-drop-zone');
    vdz.addEventListener('dragover',  e => { e.preventDefault(); vdz.classList.add('drag-over'); });
    vdz.addEventListener('dragleave', ()  => vdz.classList.remove('drag-over'));
    vdz.addEventListener('drop', e => {
      e.preventDefault(); vdz.classList.remove('drag-over');
      const f = e.dataTransfer.files[0];
      if (f) loadVidFile(f);
    });
    $('vid-file-input').addEventListener('change', e => {
      const f = e.target.files[0]; e.target.value = '';
      if (f) loadVidFile(f);
    });
  }
  loadDisplayStatus();
  _dispTimer = setInterval(loadDisplayStatus, 2000);
}

function stopDisplayPolling() {
  if (_dispTimer) { clearInterval(_dispTimer); _dispTimer = null; }
  stopDrawStream();
  stopVideoStream();
  $('btn-display').classList.remove('active');
}

async function loadDisplayStatus() {
  try {
    const s = await api('GET', '/api/display/status');
    renderDisplayStatus(s);
  } catch(e) {
    $('disp-dot').className = 'disp-dot err';
    $('disp-status-txt').textContent = 'Error: ' + esc(e.message);
  }
}

function renderDisplayStatus(s) {
  const dot = $('disp-dot');
  const txt = $('disp-status-txt');
  if (!s.connected) {
    dot.className = 'disp-dot err';
    txt.textContent = 'Hook not connected — deploy push-display and restart Push3';
    document.querySelectorAll('.mode-pill').forEach(b => b.classList.remove('active'));
    return;
  }
  dot.className = 'disp-dot ok';
  const names = ['Passthrough', 'Debug overlay', 'Image'];
  txt.textContent = 'Connected · ' + (_dispUiMode === 'video' ? 'Video' : (names[s.mode] || '?')) + ' · frame ' + s.frame_seq;

  // Sync _dispUiMode from server mode if not user-chosen
  if (_dispUiMode === 'off' || _dispUiMode === 'debug' || _dispUiMode === 'image') {
    _dispUiMode = s.mode === 0 ? 'off' : s.mode === 1 ? 'debug' : 'image';
  }

  // Update pill active states
  document.querySelectorAll('.mode-pill').forEach(b => {
    const bm = b.dataset.mode;
    let active;
    if (bm === 'video') {
      active = _dispUiMode === 'video';
    } else {
      active = parseInt(bm) === s.mode && _dispUiMode !== 'video';
    }
    b.classList.toggle('active', active);
  });

  // Show correct content section
  $('disp-upload-section').style.display = (s.mode === 2 && _dispUiMode === 'image') ? '' : 'none';
  $('disp-video-section').style.display  = (_dispUiMode === 'video') ? '' : 'none';

  if (s.mode !== _dispLastMode) {
    _dispLastMode = s.mode;
    renderDispCanvas(s.mode);
  }
}

function renderDispCanvas(mode) {
  const ctx = _dispCtx;
  if (!ctx) return;
  ctx.clearRect(0, 0, 960, 160);
  if (mode === 0) {
    const g = ctx.createLinearGradient(0, 0, 960, 0);
    g.addColorStop(0, '#1c1c1e'); g.addColorStop(1, '#131315');
    ctx.fillStyle = g; ctx.fillRect(0, 0, 960, 160);
    ctx.fillStyle = 'rgba(255,255,255,0.13)';
    ctx.font = '700 14px system-ui'; ctx.textAlign = 'center';
    ctx.fillText('PASSTHROUGH — Ableton Live display active', 480, 85);
  } else if (mode === 1) {
    ctx.fillStyle = '#111'; ctx.fillRect(0, 0, 960, 160);
    ctx.fillStyle = '#FF5500'; ctx.fillRect(0, 0, 960, 14);
    ctx.fillStyle = 'rgba(255,255,255,0.13)';
    ctx.font = '700 14px system-ui'; ctx.textAlign = 'center';
    ctx.fillText('BAR OVERLAY', 480, 95);
  } else if (mode === 2) {
    if (_dispImgData) {
      ctx.putImageData(_dispImgData, 0, 0);
    } else {
      ctx.fillStyle = '#111'; ctx.fillRect(0, 0, 960, 160);
      ctx.fillStyle = 'rgba(255,255,255,0.13)';
      ctx.font = '700 14px system-ui'; ctx.textAlign = 'center';
      ctx.fillText('CUSTOM IMAGE — upload below', 480, 85);
    }
  }
}

let _screenshotUrl = null;
async function captureScreenshot() {
  try {
    const res = await api('GET', '/api/display/screenshot');
    const blob = await res.blob();
    if (_screenshotUrl) URL.revokeObjectURL(_screenshotUrl);
    _screenshotUrl = URL.createObjectURL(blob);
    const img = $('screenshot-preview'), dl = $('screenshot-dl');
    img.src = _screenshotUrl; img.style.display = 'block';
    dl.href = _screenshotUrl; dl.style.display = '';
    if (res.headers.get('X-Display-Mode') !== '2') {
      toast('Display not in takeover — frame may be stale (Push native UI is not captured)', 'info');
    } else {
      toast('Screenshot captured', 'info');
    }
  } catch(e) {
    toast('Screenshot failed: ' + esc(e.message), 'error');
  }
}

async function setDisplayMode(mode) {
  _dispUiMode = mode === 0 ? 'off' : mode === 1 ? 'debug' : 'image';
  stopVideoStream();
  try {
    await api('POST', '/api/display/mode', {mode});
    loadDisplayStatus();
  } catch(e) {
    toast('Mode change failed: ' + esc(e.message), 'error');
  }
}

async function setVideoMode() {
  _dispUiMode = 'video';
  stopDrawStream();
  try {
    await api('POST', '/api/display/mode', {mode: 2});
    loadDisplayStatus();
  } catch(e) {
    toast('Mode change failed: ' + esc(e.message), 'error');
  }
}

function setVidQuality(el) {
  _vidQuality = parseFloat(el.dataset.q);
  document.querySelectorAll('.vid-q-btn').forEach(b => b.classList.toggle('active', b === el));
}

function toggleVideoStream() {
  _vidStreaming ? stopVideoStream() : startVideoStream();
}

function startVideoStream() {
  if (!_vidSource) { toast('Pick a video file first', 'info'); return; }
  if (!_vidCanvas) {
    // Create off-DOM canvas — display:none DOM canvas blocks GPU texture updates
    // in Chrome, causing drawImage to always return frame 0 for video sources.
    _vidCanvas = document.createElement('canvas');
    _vidCanvas.width  = 960;
    _vidCanvas.height = 160;
    _vidCtx = _vidCanvas.getContext('2d');
  }
  _vidStreaming    = true;
  _vidFpsCnt      = 0;
  _vidFramesSent  = 0;
  _vidSending     = false;
  _vidLastSend    = 0;
  if (_vidStreamTimer) clearInterval(_vidStreamTimer);
  if (_vidFpsTimer)    clearInterval(_vidFpsTimer);

  _vidFpsTimer = setInterval(() => {
    const s = $('vid-status');
    if (s) s.textContent = '● Streaming · ' + _vidFpsCnt + ' fps';
    _vidFpsCnt = 0;
  }, 1000);

  const btn = $('vid-start-btn');
  if (btn) btn.textContent = '■ Stop';

  // Always use <video> source — loadVidFile routes everything there.
  // requestVideoFrameCallback fires exactly when a new decoded frame is ready,
  // solving the "drawImage always returns frame 0" issue with hidden canvases.
  if (_vidSource.tagName === 'VIDEO') {
    const vid = _vidSource;
    vid.play().catch(e => vidStatus('Play error: ' + e.message));
    // requestVideoFrameCallback fires exactly when a new decoded frame is
    // available — avoids "drawImage always returns frame 0" on hidden canvas.
    if ('requestVideoFrameCallback' in vid) {
      function rvfcLoop() {
        if (!_vidStreaming) return;
        sendVideoFrame();
        vid.requestVideoFrameCallback(rvfcLoop);
      }
      vid.requestVideoFrameCallback(rvfcLoop);
    } else {
      // rAF fallback — fires after compositor updates video frame
      function rafVidLoop() {
        if (!_vidStreaming) return;
        sendVideoFrame();
        requestAnimationFrame(rafVidLoop);
      }
      requestAnimationFrame(rafVidLoop);
    }
  }
}

function stopVideoStream() {
  _vidStreaming = false;
  if (_vidStreamTimer) { clearInterval(_vidStreamTimer); _vidStreamTimer = null; }
  if (_vidFpsTimer)    { clearInterval(_vidFpsTimer);    _vidFpsTimer    = null; }
  _vidSending = false;
  const btn = $('vid-start-btn');
  if (btn) btn.textContent = '▶ Start';
  const s = $('vid-status');
  if (s && _vidSource) s.textContent = 'Stopped';
}

function loadVidFile(f) {
  stopVideoStream();
  const url = URL.createObjectURL(f);
  const vidEl = $('vid-el');
  vidEl.onloadeddata = () => { $('vid-status').textContent = f.name + ' · ready'; };
  vidEl.src = url;
  vidEl.load();
  vidEl.style.display = '';
  _vidSource = vidEl;
  $('vid-status').textContent = 'Loading…';
}

let _vidSending = false;
let _vidFramesSent = 0;
let _vidLastSend = 0;
function vidStatus(msg) { const s = $('vid-status'); if (s) s.textContent = msg; }
function sendVideoFrame() {
  if (!_vidStreaming || !_vidSource || _vidSending) return;
  // Rate-limit: cap at ~15fps regardless of how fast RVFC/rAF fires
  const now = performance.now();
  if (now - _vidLastSend < 60) return;
  _vidLastSend = now;
  // Source must have decoded a frame
  if (_vidSource.tagName === 'VIDEO' && _vidSource.readyState < 2) {
    vidStatus('Waiting for video… (readyState=' + _vidSource.readyState + ')');
    return;
  }
  _vidSending = true;
  // createImageBitmap forces a full async snapshot of the element's current
  // rendered frame — bypasses Chrome's GPU texture caching that causes
  // drawImage(_vidSource) to always return frame 0 on off-DOM canvases.
  createImageBitmap(_vidSource).then(bitmap => {
    _vidCtx.drawImage(bitmap, 0, 0, 960, 160);
    bitmap.close();
    _vidCanvas.toBlob(blob => {
      if (!blob) { vidStatus('toBlob returned null'); _vidSending = false; return; }
      _vidFramesSent++;
      const fd = new FormData();
      fd.append('image', blob, 'frame.jpg');
      fetch('/api/display/image', {method: 'POST', body: fd})
        .then(r => {
          if (!r.ok) { vidStatus('Server error ' + r.status + ' (sent ' + _vidFramesSent + ')'); }
          else _vidFpsCnt++;
        })
        .catch(e => { vidStatus('Network error: ' + e.message); })
        .finally(() => { _vidSending = false; });
    }, 'image/jpeg', _vidQuality);
  }).catch(e => { vidStatus('Frame capture error: ' + e.message); _vidSending = false; });
}

async function uploadDisplayImage(file) {
  const info = $('disp-upload-info');
  info.textContent = 'Uploading…';
  // Local preview immediately
  const reader = new FileReader();
  reader.onload = ev => {
    const img = new Image();
    img.onload = () => {
      if (!_dispCtx) return;
      _dispCtx.clearRect(0, 0, 960, 160);
      _dispCtx.drawImage(img, 0, 0, 960, 160);
      _dispImgData  = _dispCtx.getImageData(0, 0, 960, 160);
      _dispLastMode = 2; // force canvas to re-show on next status render
    };
    img.src = ev.target.result;
  };
  reader.readAsDataURL(file);
  // Upload to Push — set mode=2 first so the frame is shown immediately
  await api('POST', '/api/display/mode', {mode: 2}).catch(() => {});
  const fd = new FormData();
  fd.append('image', file);
  try {
    const r = await fetch('/api/display/image', {method:'POST', body:fd});
    if (!r.ok) throw new Error(await r.text());
    const res = await r.json();
    info.textContent = '✓ ' + file.name + ' → ' + res.size + ' (' + res.format.toUpperCase() + ')';
    loadDisplayStatus();
  } catch(e) {
    info.textContent = 'Upload failed: ' + esc(e.message);
    toast('Upload failed: ' + esc(e.message), 'error');
  }
}

// ── Draw tab ────────────────────────────────────────────────────────────────
function initDrawCanvas() {
  if (_drawBound) return;
  _drawBound = true;
  _drawCanvas = $('draw-canvas');
  _drawCtx    = _drawCanvas.getContext('2d');
  // Start with black background
  _drawCtx.fillStyle = '#000';
  _drawCtx.fillRect(0, 0, 960, 160);

  function pos(e) {
    const r = _drawCanvas.getBoundingClientRect();
    return {
      x: Math.round((e.clientX - r.left) / r.width  * 960),
      y: Math.round((e.clientY - r.top)  / r.height * 160)
    };
  }
  function stroke(x, y, first) {
    const ctx = _drawCtx;
    const col = _drawEraser ? '#000000' : _drawColor;
    const w   = _drawEraser ? _drawSize * 5 : _drawSize * 2;
    if (first) {
      ctx.beginPath();
      ctx.arc(x, y, w / 2, 0, Math.PI * 2);
      ctx.fillStyle = col;
      ctx.fill();
    } else {
      ctx.beginPath();
      ctx.moveTo(_drawLastX, _drawLastY);
      ctx.lineTo(x, y);
      ctx.strokeStyle = col;
      ctx.lineWidth   = w;
      ctx.lineCap     = 'round';
      ctx.lineJoin    = 'round';
      ctx.stroke();
    }
    _drawLastX = x; _drawLastY = y;
    _drawDirty = true;
  }

  _drawCanvas.addEventListener('mousedown', e => {
    _drawPainting = true;
    const p = pos(e); stroke(p.x, p.y, true);
  });
  _drawCanvas.addEventListener('mousemove', e => {
    if (!_drawPainting) return;
    const p = pos(e); stroke(p.x, p.y, false);
  });
  _drawCanvas.addEventListener('mouseup',    () => { _drawPainting = false; });
  _drawCanvas.addEventListener('mouseleave', () => { _drawPainting = false; });

  _drawCanvas.addEventListener('touchstart', e => {
    e.preventDefault();
    _drawPainting = true;
    const t = e.touches[0], p = pos(t); stroke(p.x, p.y, true);
  }, {passive: false});
  _drawCanvas.addEventListener('touchmove', e => {
    e.preventDefault();
    if (!_drawPainting) return;
    const t = e.touches[0], p = pos(t); stroke(p.x, p.y, false);
  }, {passive: false});
  _drawCanvas.addEventListener('touchend', e => {
    e.preventDefault(); _drawPainting = false;
  }, {passive: false});
}

function setDrawColor(el) {
  _drawColor  = el.dataset.color;
  _drawEraser = false;
  document.querySelectorAll('.draw-swatch').forEach(s => s.classList.remove('active'));
  el.classList.add('active');
  $('draw-eraser-btn').classList.remove('active');
}

function setDrawSize(el) {
  _drawSize = parseInt(el.dataset.size);
  document.querySelectorAll('.draw-size-btn').forEach(s => s.classList.remove('active'));
  el.classList.add('active');
}

function toggleDrawEraser() {
  _drawEraser = !_drawEraser;
  $('draw-eraser-btn').classList.toggle('active', _drawEraser);
  document.querySelectorAll('.draw-swatch').forEach(s => s.classList.remove('active'));
}

function clearDrawCanvas() {
  if (!_drawCtx) return;
  _drawCtx.fillStyle = '#000';
  _drawCtx.fillRect(0, 0, 960, 160);
  _drawDirty = true;
}

function startDrawStream() {
  if (_drawStreamTimer) return;
  api('POST', '/api/display/mode', {mode: 2}).catch(() => {});
  _drawStreamTimer = setInterval(flushDrawFrame, 100);
}

function stopDrawStream() {
  if (_drawStreamTimer) { clearInterval(_drawStreamTimer); _drawStreamTimer = null; }
  const el = $('draw-live-status');
  if (el) { el.textContent = '—'; el.className = ''; }
}

let _drawFlushing = false;
async function flushDrawFrame() {
  if (!_drawDirty || !_drawCanvas || _drawFlushing) return;
  _drawDirty   = false;
  _drawFlushing = true;
  const status = $('draw-live-status');
  _drawCanvas.toBlob(async blob => {
    try {
      const fd = new FormData();
      fd.append('image', blob, 'draw.jpg');
      const r = await fetch('/api/display/image', {method:'POST', body:fd});
      if (r.ok) {
        if (status) { status.textContent = '● LIVE'; status.className = 'on'; }
      } else {
        if (status) { status.textContent = 'ERR'; status.className = ''; }
      }
    } catch(e) {
      if (status) { status.textContent = 'ERR'; status.className = ''; }
    } finally {
      _drawFlushing = false;
    }
  }, 'image/jpeg', 0.7);
}

// ── Browser (preset browser) ────────────────────────────────────────────────
const BR = { q:'', cat:'', device:'', source:'', rack:'', fav:false, tag:'', type:'', subtype:'', facetsLoaded:false, devByCat:{}, allDevices:[], sampleSources:[] };
// Categories whose .adg racks get a dedicated dropdown entry (Drums are already
// rack-only). The dropdown value "<cat>|rack" selects category + racks-only.
const BR_RACK_LABEL = { 'Instruments':'Instrument Racks', 'Audio Effects':'Audio Effect Racks', 'MIDI Effects':'MIDI Effect Racks' };
// Split a category dropdown value into {cat, rack} (rack='' or 'rack').
function brParseCat(v) {
  if (v && v.endsWith('|rack')) return { cat: v.slice(0, -5), rack: 'rack' };
  return { cat: v, rack: '' };
}
let _brSearchTimer = null;
const BR_CAP = 300; // max rows rendered at once

function showBrowser() {
  history.pushState({view:'browser'}, '');
  show('browser');
  $('btn-back').className = 'visible';
  $('header-sep').style.display = 'none';
  $('header-nav').style.display = 'none';
}

async function brInit() {
  if (!BR.facetsLoaded) {
    try {
      const f = await api('GET', '/api/presets/facets');
      BR.devByCat = f.devices_by_category || {};
      BR.allDevices = f.devices || [];
      BR.sampleSources = f.sources || [];
      const cat = $('br-category');
      let catOpts = '<option value="">All categories</option>';
      (f.categories||[]).forEach(c => {
        catOpts += `<option value="${esc(c)}">${esc(c)}</option>`;
        // Offer racks of this category as an explicit pick (value "<cat>|rack").
        if (BR_RACK_LABEL[c]) catOpts += `<option value="${esc(c)}|rack">${esc(BR_RACK_LABEL[c])}</option>`;
      });
      cat.innerHTML = catOpts;
      brPopulateDevices('');
      const srcOpts = '<option value="">Packs</option>' +
        (f.sources||[]).map(s => `<option value="${esc(s)}">${esc(s)}</option>`).join('');
      $('br-source').innerHTML = srcOpts;
      $('br-source-sample').innerHTML = srcOpts;
      renderBrSubtypeChips(f.sample_subtypes||[]);
      renderBrTagChips(f.tags||[]);
      BR.facetsLoaded = true;
    } catch(e) { /* index may still be building */ }
  }
  brReload();
}

function brSetType(t) {
  BR.type = t;
  BR.subtype = '';
  // Update type button active states
  ['', 'preset', 'sample'].forEach(v => {
    const btn = $('br-type-' + (v||'all'));
    if (btn) btn.classList.toggle('active', v === t);
  });
  // Show/hide preset vs sample filter rows
  const isSample = t === 'sample';
  $('br-preset-filters').style.display = isSample ? 'none' : 'flex';
  $('br-sample-filters').style.display = isSample ? 'flex' : 'none';
  $('br-subtypes').style.display = isSample ? 'flex' : 'none';
  // Reset subtype chip active states
  document.querySelectorAll('#br-subtypes .br-chip').forEach(c => c.classList.remove('active'));
  brReload();
}

function renderBrSubtypeChips(subtypes) {
  $('br-subtypes').innerHTML = subtypes.map(t =>
    `<span class="br-chip${BR.subtype===t?' active':''}" onclick="brToggleSubtype('${esc(t)}')">${esc(t)}</span>`
  ).join('');
}

function brToggleSubtype(t) {
  BR.subtype = (BR.subtype === t) ? '' : t;
  document.querySelectorAll('#br-subtypes .br-chip').forEach(c =>
    c.classList.toggle('active', c.textContent === BR.subtype));
  brReload();
}

function brPopulateDevices(cat) {
  const devs = cat ? (BR.devByCat[cat] || []) : BR.allDevices;
  $('br-device').innerHTML = '<option value="">All devices</option>' +
    devs.map(d => `<option value="${esc(d)}">${esc(d)}</option>`).join('');
}

function brCategoryChange() {
  const cat = brParseCat($('br-category').value).cat;
  const prevDev = $('br-device').value;
  brPopulateDevices(cat);
  // restore device selection if still valid in new list
  const opt = $('br-device').querySelector(`option[value="${cssEsc(prevDev)}"]`);
  if (opt) $('br-device').value = prevDev;
  brReload();
}

function renderBrTagChips(tags) {
  $('br-tags').innerHTML = tags.map(t =>
    `<span class="br-chip${BR.tag===t?' active':''}" onclick="brToggleTag('${esc(t)}')">${esc(t)}</span>`
  ).join('');
}

function brSearchInput() {
  clearTimeout(_brSearchTimer);
  _brSearchTimer = setTimeout(brReload, 200);
}

function brToggleFav() {
  BR.fav = !BR.fav;
  $('br-fav').classList.toggle('active', BR.fav);
  $('br-fav-sample').classList.toggle('active', BR.fav);
  brReload();
}

function brToggleTag(t) {
  BR.tag = (BR.tag === t) ? '' : t;
  document.querySelectorAll('#br-tags .br-chip').forEach(c =>
    c.classList.toggle('active', c.textContent === BR.tag));
  brReload();
}

async function brReload() {
  BR.q      = $('br-search').value.trim();
  const isSample = BR.type === 'sample';
  const pc = brParseCat(isSample ? '' : $('br-category').value);
  BR.cat    = pc.cat;
  BR.rack   = pc.rack;
  BR.device = isSample ? '' : $('br-device').value;
  BR.source = isSample ? $('br-source-sample').value : $('br-source').value;
  const qs = new URLSearchParams();
  if (BR.q)       qs.set('q', BR.q);
  if (BR.cat)     qs.set('filter', BR.cat);
  if (BR.device)  qs.set('device', BR.device);
  if (BR.source)  qs.set('source', BR.source);
  if (BR.rack)    qs.set('rack', BR.rack);
  if (BR.fav)     qs.set('fav', '1');
  if (BR.tag)     qs.set('tag', BR.tag);
  if (BR.type)    qs.set('type', BR.type);
  if (BR.subtype) qs.set('subtype', BR.subtype);
  try {
    const r = await api('GET', '/api/presets?' + qs.toString());
    renderBrowserList(r.presets || [], r.count, r.total);
  } catch(e) {
    $('browser-list').innerHTML = `<div class="empty-state"><strong>Error</strong>${esc(e.message)}</div>`;
  }
}

function brIcon(p) {
  if (p.type === 'sample') return 'Audio.png';
  if (p.category === 'Drums') return 'Browser_DrumRack.png';
  if (p.category === 'Audio Effects') return p.is_rack ? 'Browser_AudioEffectRack.png' : 'Browser_AudioEffectPreset.png';
  if (p.category === 'MIDI Effects')  return p.is_rack ? 'Browser_MidiEffectRack.png'  : 'Browser_MidiEffectPreset.png';
  if (p.category === 'Instruments')   return p.is_rack ? 'Browser_InstrumentRack.png'  : 'Browser_InstrumentPreset.png';
  return 'PresetDevice.png';
}

function renderBrowserList(presets, count, total) {
  const label = BR.type === 'sample' ? 'sample' : BR.type === 'preset' ? 'preset' : 'item';
  $('br-count').textContent = count > BR_CAP
    ? `showing ${BR_CAP} of ${count} ${label}s (${total} total)`
    : `${count} ${label}${count===1?'':'s'} (${total} total)`;
  if (!presets.length) {
    $('browser-list').innerHTML = `<div class="empty-state"><strong>No results</strong>Adjust filters or press Refresh.</div>`;
    return;
  }
  const rows = presets.slice(0, BR_CAP).map(p => {
    const tags = (p.tags||[]).map(t =>
      `<span class="br-tag">${esc(t)}<b onclick="brRemoveTag('${esc(p.path)}','${esc(t)}')">✕</b></span>`).join('');
    const metaParts = p.type === 'sample'
      ? [p.sample_subtype, p.source].filter(Boolean)
      : [p.device, p.source].filter(Boolean);
    const meta = metaParts.map(esc).join(' · ');
    return `<div class="br-row" data-path="${esc(p.path)}">
      <img class="br-icon" src="/api/assets/Browser/${brIcon(p)}" onerror="this.style.visibility='hidden'">
      <div style="flex:1;min-width:0">
        <div class="br-name">${esc(p.name)}</div>
        <div class="br-meta">${meta}${tags?'  ':''}${tags}<button class="br-addtag" onclick="brAddTag('${esc(p.path)}')">+ tag</button></div>
      </div>
      <button class="br-star${p.favourite?' on':''}" title="Favourite" onclick="brSetFav('${esc(p.path)}',${!p.favourite},this)">${p.favourite?'★':'☆'}</button>
      <button class="br-load" onclick="brLoad('${esc(p.name)}','${esc(p.category)}','${esc(p.type||'')}')">Load</button>
    </div>`;
  }).join('');
  $('browser-list').innerHTML = rows;
}

async function brSetFav(path, fav, btn) {
  try {
    await api('POST', '/api/presets/meta', {path, favourite: fav});
    btn.classList.toggle('on', fav);
    btn.textContent = fav ? '★' : '☆';
    btn.setAttribute('onclick', `brSetFav('${path.replace(/'/g,"\\'")}',${!fav},this)`);
    if (BR.fav) brReload(); // may drop out of the favourites view
  } catch(e) { toast('Failed: '+e.message, 'error'); }
}

async function brAddTag(path) {
  const t = (prompt('Add tag:')||'').trim();
  if (!t) return;
  try {
    const row = document.querySelector(`.br-row[data-path="${cssEsc(path)}"]`);
    const cur = brRowTags(row);
    if (cur.includes(t)) return;
    const r = await api('POST', '/api/presets/meta', {path, tags: [...cur, t]});
    BR.facetsLoaded = false; // new tag may extend the facet list
    brReload(); brInit();
  } catch(e) { toast('Failed: '+e.message, 'error'); }
}

async function brRemoveTag(path, tag) {
  try {
    const row = document.querySelector(`.br-row[data-path="${cssEsc(path)}"]`);
    const cur = brRowTags(row).filter(t => t !== tag);
    await api('POST', '/api/presets/meta', {path, tags: cur});
    brReload();
  } catch(e) { toast('Failed: '+e.message, 'error'); }
}

function brRowTags(row) {
  if (!row) return [];
  return [...row.querySelectorAll('.br-tag')].map(s => s.firstChild.textContent.trim());
}
function cssEsc(s){ return s.replace(/(["\\])/g,'\\$1'); }

async function brLoad(name, category, type) {
  try {
    await api('POST', '/api/live/load', {name, category, type: type||''});
    toast('Loaded "'+name+'" onto the selected track', 'success');
  } catch(e) { toast('Load failed: '+e.message, 'error'); }
}

async function brRefresh() {
  toast('Rescanning presets…', 'info');
  try {
    const r = await api('POST', '/api/presets/refresh');
    BR.facetsLoaded = false;
    await brInit();
    toast(`Indexed ${r.count} presets`, 'success');
  } catch(e) { toast('Refresh failed: '+e.message, 'error'); }
}

// ── Boot ───────────────────────────────────────────────────────────────────
history.replaceState({view:'home'}, '');
loadRoots(false);

// Stop all streaming when tab hides or page unloads — prevents orphaned
// streams that keep mode=2 locked and fight against "Off" mode changes.
document.addEventListener('visibilitychange', () => {
  if (document.hidden) { stopVideoStream(); stopDrawStream(); }
});
window.addEventListener('pagehide', () => { stopVideoStream(); stopDrawStream(); });
