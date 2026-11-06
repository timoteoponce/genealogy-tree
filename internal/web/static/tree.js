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

    // Node cards: photo + name + life years (imageRect = photo-left node).
    // "compact" mode uses small circular photo chips for dense trees.
    const card = chart.setCardHtml();
    if (mode === 'compact') {
      card.setStyle('imageCircle');
      card.setCardDisplay([['first name', 'last name']]);
    } else {
      card.setStyle('imageRect');
      card.setCardDisplay([['first name', 'last name'], ['years']]);
    }

    // Layout: vertical (ancestors ↑ / descendants ↓) or horizontal
    // (ancestors ← / descendants →). Horizontal suits descendant-heavy or
    // large trees, which otherwise collapse into a single vertical line.
    if (orient === 'h') {
      chart.setOrientationHorizontal();
      // In horizontal mode: x spacing = gap between generations (wide, to
      // fit 240px cards); y spacing = gap between siblings.
      chart.setCardXSpacing(mode === 'compact' ? 150 : 280);
      chart.setCardYSpacing(mode === 'compact' ? 70 : 95);
    } else {
      chart.setCardXSpacing(mode === 'compact' ? 110 : 260);
      chart.setCardYSpacing(mode === 'compact' ? 110 : 140);
    }

    // gens=0 means "everything" (full tree) — render up to a deep ceiling.
    const depth = gens <= 0 ? 30 : gens;
    chart.setProgenyDepth(depth).setAncestryDepth(depth);
    chart.setTransitionTime(400);
    chart.updateTree({ initial: true });

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
