// fan.js — radial pedigree (fan chart): the focus person in the center,
// ancestors in concentric rings, laid out with d3.cluster into polar
// coordinates. Pan/zoom with the mouse or touch; click a node to re-focus.
const cont = document.getElementById('fan');
if (cont && window.d3) {
  const focusId = cont.dataset.focusId;
  const gens = Number(cont.dataset.gens || 5);

  main().catch(err => {
    console.error(err);
    cont.innerHTML = '<p class="error">failed to render the fan: ' + err.message + '</p>';
  });

  async function main() {
    const res = await fetch(`/fan.json?person=${focusId}&gens=${gens}`);
    if (!res.ok) throw new Error('fan.json ' + res.status);
    const root = await res.json();

    const width = cont.clientWidth || 900;
    const height = cont.clientHeight || 640;

    const svg = d3.select('#fan').append('svg')
      .attr('viewBox', `0 0 ${width} ${height}`)
      .attr('width', '100%').attr('height', '100%')
      .attr('preserveAspectRatio', 'xMidYMid meet');

    const defs = svg.append('defs');

    const hierarchy = d3.hierarchy(root, d => d.children);
    const maxDepth = Math.max(1, hierarchy.height);

    // Node radius by depth (bigger toward the center so focus + close
    // ancestors stay legible).
    const nodeRadius = depth => (depth === 0 ? 42 : Math.max(15, 34 - depth * 3));

    // Outermost ring distance, in layout units.
    const radius = Math.min(width, height) / 2 - 60;
    const cluster = d3.cluster().size([2 * Math.PI, radius]);
    cluster(hierarchy);

    // One circular clip path per depth (SVG <image> can't be border-radius'd,
    // so photos are clipped to circles here).
    defs.selectAll('clipPath').data(d3.range(maxDepth + 1)).join('clipPath')
      .attr('id', d => `fanclip-${d}`)
      .append('circle').attr('r', nodeRadius);

    // Zoomable root group + inner group centered on the container.
    const rootG = svg.append('g').attr('class', 'fan-root');
    const fanG = rootG.append('g').attr('class', 'fan-g')
      .attr('transform', `translate(${width / 2},${height / 2})`);

    // Wedges + links (behind nodes).
    fanG.append('g').attr('class', 'fan-links').selectAll('path')
      .data(hierarchy.links())
      .join('path')
      .attr('d', d3.linkRadial().angle(d => d.x).radius(d => d.y))
      .attr('fill', 'none')
      .attr('stroke', '#cfc7b8')
      .attr('stroke-width', 1.2);

    // Nodes.
    const node = fanG.append('g').attr('class', 'fan-nodes').selectAll('g')
      .data(hierarchy.descendants())
      .join('g')
      .attr('transform', d => `translate(${radialX(d)},${radialY(d)})`)
      .style('cursor', 'pointer');

    // Sex-colored ring behind each photo.
    node.append('circle')
      .attr('class', 'fan-ring')
      .attr('r', d => nodeRadius(d.depth) + 2)
      .attr('fill', d => d.data.sex === 'F' ? '#c48a92' : d.data.sex === 'M' ? '#7a9fb0' : '#b7b2a7')
      .attr('stroke', '#fff').attr('stroke-width', 2);

    // Photo, clipped to a circle.
    node.append('image')
      .attr('href', d => d.data.avatar)
      .attr('x', d => -nodeRadius(d.depth))
      .attr('y', d => -nodeRadius(d.depth))
      .attr('width', d => nodeRadius(d.depth) * 2)
      .attr('height', d => nodeRadius(d.depth) * 2)
      .attr('clip-path', d => `url(#fanclip-${d.depth})`)
      .attr('preserveAspectRatio', 'xMidYMid slice');

    // Name labels for the focus + parents; deeper rings use the hover title.
    node.filter(d => d.depth <= 1).append('text')
      .attr('class', 'fan-label')
      .attr('text-anchor', 'middle')
      .attr('y', d => nodeRadius(d.depth) + 15)
      .text(d => shortName(d.data.name));

    // Focus marker ring.
    fanG.append('circle').attr('class', 'fan-focus')
      .attr('r', nodeRadius(0) + 5)
      .attr('fill', 'none').attr('stroke', '#3f7d4e').attr('stroke-width', 3);

    node.append('title').text(d => d.data.name + (d.data.years ? ' (' + d.data.years + ')' : ''));

    // Click → re-focus that person.
    node.on('click', (event, d) => {
      if (d.data.id !== focusId) {
        location.href = '/person/' + d.data.id + '/tree?view=fan&gens=' + gens;
      }
    });

    // Pan + zoom (drag anywhere; pinch/wheel to zoom).
    const zoom = d3.zoom().scaleExtent([0.2, 8])
      .on('zoom', event => rootG.attr('transform', event.transform));
    svg.call(zoom).on('dblclick.zoom', null);

    // Fit the fan to the container on load.
    const maxR = radius + nodeRadius(maxDepth) + 20;
    const fitScale = Math.min(1, (Math.min(width, height) / 2 - 10) / maxR);
    svg.call(zoom.transform, d3.zoomIdentity
      .translate(width / 2, height / 2).scale(fitScale).translate(-width / 2, -height / 2));

    function radialX(d) { return d.y * Math.cos(d.x - Math.PI / 2); }
    function radialY(d) { return d.y * Math.sin(d.x - Math.PI / 2); }
    function shortName(name) {
      return name.length > 22 ? name.slice(0, 21) + '…' : name;
    }
  }
}
