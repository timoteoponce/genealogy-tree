// fan.js — radial pedigree (fan chart): the focus person in the center,
// ancestors in concentric rings, laid out with d3.cluster into polar
// coordinates. Click a node to re-focus on that person.
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
      .attr('width', width).attr('height', height);

    const g = svg.append('g')
      .attr('transform', `translate(${width / 2},${height / 2})`);

    const hierarchy = d3.hierarchy(root, d => d.children);
    const maxDepth = Math.max(1, hierarchy.height);

    const radius = Math.min(width, height) / 2 - 60;
    const cluster = d3.cluster().size([2 * Math.PI, radius]);
    cluster(hierarchy);

    // Wedges + links (drawn first, behind nodes).
    svg.append('g').attr('class', 'fan-links').selectAll('path')
      .data(hierarchy.links())
      .join('path')
      .attr('d', d3.linkRadial()
        .angle(d => d.x)
        .radius(d => d.y))
      .attr('fill', 'none')
      .attr('stroke', '#c8c0b0')
      .attr('stroke-width', 1.2);

    const node = svg.append('g').attr('class', 'fan-nodes').selectAll('g')
      .data(hierarchy.descendants())
      .join('g')
      .attr('transform', d => `translate(${radialX(d)},${radialY(d)})`);

    // Circular photo (or placeholder avatar).
    node.append('image')
      .attr('xlink:href', d => d.data.avatar)
      .attr('x', d => nodeRadius(d) * -1)
      .attr('y', d => nodeRadius(d) * -1)
      .attr('width', d => nodeRadius(d) * 2)
      .attr('height', d => nodeRadius(d) * 2)
      .attr('clip-path', d => `url(#fanclip${d.depth})`)
      .attr('preserveAspectRatio', 'xMidYMid slice');

    node.append('title').text(d => d.data.name + (d.data.years ? ' (' + d.data.years + ')' : ''));

    // Focus marker.
    svg.append('circle').attr('class', 'fan-focus')
      .attr('r', nodeRadius({ depth: 0 }) + 4)
      .attr('fill', 'none').attr('stroke', '#3f7d4e').attr('stroke-width', 3);

    // Click → navigate to that person's fan (re-focus).
    node.style('cursor', 'pointer')
      .on('click', (event, d) => {
        if (d.data.id !== focusId) {
          location.href = '/person/' + d.data.id + '/tree?view=fan&gens=' + gens;
        }
      });

    function radialX(d) { return d.y * Math.cos(d.x - Math.PI / 2); }
    function radialY(d) { return d.y * Math.sin(d.x - Math.PI / 2); }
    function nodeRadius(d) {
      // Bigger toward the center so the focus + close ancestors are legible.
      return d.depth === 0 ? 40 : Math.max(14, 34 - d.depth * 3);
    }
  }
}
