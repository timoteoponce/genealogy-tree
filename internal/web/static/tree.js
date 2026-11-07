// tree.js — interactive family tree (family-chart), self-hosted, no CDN.
// Served at /person/{id}/tree and /tree. Data comes from /tree.json.
const cont = document.getElementById('chart');
if (cont && window.f3) {
  const focusId = cont.dataset.focusId;
  const gens = Number(cont.dataset.gens || 4);
  const orient = cont.dataset.orient === 'h' ? 'h' : 'v';
  const mode = cont.dataset.compact === 'true' ? 'compact' : 'tree';

  main().catch(err => {
    console.error(err);
    cont.innerHTML = '<p class="error">failed to render the tree: ' + err.message + '</p>';
  });

  async function main() {
    const res = await fetch(`/tree.json?person=${focusId}&gens=${gens}`);
    if (!res.ok) throw new Error('tree.json ' + res.status);
    /** family-chart nodes (see docs/data-format.md) */
    const data = await res.json();

    const chart = f3.createChart('#chart', data);

    // Node cards: photo-left cards (imageRect) in BOTH modes. Compact mode
    // (dense trees) renders the same cards smaller via #chart[data-compact]
    // CSS. The name lives INSIDE the card box, which family-chart measures
    // (offsetWidth/offsetHeight) — unlike the imageCircle style, whose
    // absolutely-positioned label escapes the box and overlaps neighbours.
    const card = chart.setCardHtml();
    card.setStyle('imageRect');
    card.setCardDisplay([['first name', 'last name'], ['years']]);

    // Layout: vertical (ancestors ↑ / descendants ↓) or horizontal
    // (ancestors ← / descendants →). Horizontal suits descendant-heavy or
    // large trees, which otherwise collapse into a single vertical line.
    if (orient === 'h') {
      chart.setOrientationHorizontal();
      // In horizontal mode: x spacing = gap between generations (wide, to
      // fit the cards); y spacing = gap between siblings.
      chart.setCardXSpacing(mode === 'compact' ? 220 : 280);
      chart.setCardYSpacing(mode === 'compact' ? 78 : 95);
    } else {
      chart.setCardXSpacing(mode === 'compact' ? 215 : 260);
      chart.setCardYSpacing(mode === 'compact' ? 100 : 140);
    }

    // gens=0 means "everything" (full tree) — render up to a deep ceiling.
    const depth = gens <= 0 ? 30 : gens;
    chart.setProgenyDepth(depth).setAncestryDepth(depth);
    chart.setTransitionTime(400);
    chart.updateTree({ initial: true });

    // Family-chart auto-fits the whole tree into the container on first
    // render, which shrinks large trees to an unreadable scale. Reset to
    // 100% centered on the focus person, then offer zoom controls.
    let fitMode = false;
    window.__f3chart = chart;
    setTimeout(() => {
      if (!fitMode) resetZoomToFocus();
      addZoomControls();
    }, 120);

    function resetZoomToFocus() {
      try {
        const svg = document.querySelector('#chart svg.main_svg');
        const holder = svg && (svg.__zoomObj ? svg : (svg.parentNode && svg.parentNode.__zoomObj ? svg.parentNode : null));
        const zoomObj = holder && holder.__zoomObj;
        const mainDatum = chart.store.getTreeMainDatum();
        const container = document.getElementById('chart');
        if (!svg || !zoomObj || !mainDatum || !container) return;
        const d3 = window.d3;
        if (!d3) return;
        f3.handlers.cardToMiddle({
          datum: mainDatum,
          svg: svg,
          svg_dim: { width: container.clientWidth, height: container.clientHeight },
          scale: 1,
          transition_time: 0,
        });
      } catch (e) { console.error('zoom reset failed:', e); }
    }

    function addZoomControls() {
      const container = document.getElementById('chart');
      if (!container || container.querySelector('.zoom-ctrls')) return;
      const el = document.createElement('div');
      el.className = 'zoom-ctrls';
      el.innerHTML = '<button type="button" data-z="in" aria-label="Zoom in">+</button>'
        + '<button type="button" data-z="out" aria-label="Zoom out">−</button>'
        + '<button type="button" data-z="fit" aria-label="Fit whole tree">fit</button>';
      container.appendChild(el);
      const svg = document.querySelector('#chart svg.main_svg');
      const holder = svg && (svg.__zoomObj ? svg : (svg.parentNode && svg.parentNode.__zoomObj ? svg.parentNode : null));
      const zoomObj = holder && holder.__zoomObj;
      el.addEventListener('click', (e) => {
        const btn = e.target.closest('button[data-z]');
        if (!btn) return;
        const z = btn.dataset.z;
        if (z === 'in' && zoomObj) {
          d3.select(holder).call(zoomObj.scaleBy, 1.3);
        } else if (z === 'out' && zoomObj) {
          d3.select(holder).call(zoomObj.scaleBy, 0.77);
        } else if (z === 'fit') {
          fitMode = true;
          chart.updateTree({ initial: true });
          setTimeout(() => { fitMode = false; }, 250);
        }
      });
    }

    // Click = re-center on that person; double-click = open profile page.
    if (typeof card.setOnCardClick === 'function') {
      card.setOnCardClick((event, datum) => {
        const id = datum && datum.data && datum.data.id;
        if (!id) return;
        if (event && (event.detail || 0) >= 2) {
          window.location.href = '/person/' + id;
          return;
        }
        chart.updateMainId(id);
        chart.updateTree({});
      });
    }

    // Keep the focus bar + URL in sync after every re-center.
    chart.setAfterUpdate(() => setTimeout(syncFocus, 50));
    syncFocus();

    function syncFocus() {
      const d = chart.getMainDatum();
      if (!d || !d.data) return;
      const name = (d.data['first name'] + ' ' + (d.data['last name'] || '')).trim();
      document.getElementById('focusName').textContent = name;
      document.getElementById('focusLink').href = '/person/' + d.data.id;
      const u = new URL(location.href);
      u.searchParams.set('person', d.data.id);
      history.replaceState(null, '', u.pathname + u.search);
    }
  }
}
