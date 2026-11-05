// graph.js — force-directed "everyone" graph: every person is a node,
// parent↔child and spouse links connect them. Pan/zoom/drag; click a node
// to open the profile. Scales to thousands and shows disconnected branches.
const cont = document.getElementById('graph');
if (cont && window.d3) {
  main().catch(err => {
    console.error(err);
    cont.innerHTML = '<p class="error">failed to render the graph: ' + err.message + '</p>';
  });

  async function main() {
    const res = await fetch('/graph.json');
    if (!res.ok) throw new Error('graph.json ' + res.status);
    const data = await res.json();
    if (!data.nodes.length) {
      cont.innerHTML = '<p class="muted">No people yet.</p>';
      return;
    }

    const width = cont.clientWidth || 900;
    const height = cont.clientHeight || 720;

    const svg = d3.select('#graph').append('svg')
      .attr('width', width).attr('height', height);

    const zoom = d3.zoom().scaleExtent([0.2, 8])
      .on('zoom', (event) => root.attr('transform', event.transform));
    svg.call(zoom);

    const root = svg.append('g');
    const linkLayer = root.append('g').attr('class', 'graph-links');
    const nodeLayer = root.append('g').attr('class', 'graph-nodes');

    const links = data.links.map(l => ({ ...l }));
    const nodes = data.nodes.map(n => ({ ...n }));

    const simulation = d3.forceSimulation(nodes)
      .force('link', d3.forceLink(links).id(d => d.id)
        .distance(l => l.type === 'spouse' ? 70 : 55).strength(0.6))
      .force('charge', d3.forceManyBody().strength(-120))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collide', d3.forceCollide().radius(20))
      .force('x', d3.forceX(width / 2).strength(0.03))
      .force('y', d3.forceY(height / 2).strength(0.03));

    const link = linkLayer.selectAll('line')
      .data(links).join('line')
      .attr('class', l => l.type)
      .attr('stroke', l => l.type === 'spouse' ? '#c48a92' : '#9fb6c2')
      .attr('stroke-width', l => l.type === 'spouse' ? 1.5 : 1)
      .attr('stroke-dasharray', l => l.type === 'spouse' ? null : '3,3');

    const node = nodeLayer.selectAll('g')
      .data(nodes).join('g')
      .style('cursor', 'pointer')
      .call(d3.drag()
        .on('start', (event, d) => { if (!event.active) simulation.alphaTarget(0.3).restart(); d.fx = d.x; d.fy = d.y; })
        .on('drag', (event, d) => { d.fx = event.x; d.fy = event.y; })
        .on('end', (event, d) => { if (!event.active) simulation.alphaTarget(0); d.fx = null; d.fy = null; }));

    node.append('circle')
      .attr('r', 16)
      .attr('fill', d => d.sex === 'F' ? '#c48a92' : d.sex === 'M' ? '#7a9fb0' : '#b7b2a7')
      .attr('stroke', '#fff').attr('stroke-width', 2);

    node.append('image')
      .attr('xlink:href', d => d.avatar)
      .attr('x', -13).attr('y', -13).attr('width', 26).attr('height', 26)
      .attr('clip-path', 'url(#gclip)')
      .attr('preserveAspectRatio', 'xMidYMid slice');

    node.append('title').text(d => d.name + (d.years ? ' (' + d.years + ')' : ''));
    node.on('click', (event, d) => { if (!event.defaultPrevented) location.href = '/person/' + d.id; });

    svg.append('defs').append('clipPath').attr('id', 'gclip')
      .append('circle').attr('r', 13);

    simulation.on('tick', () => {
      link.attr('x1', d => d.source.x).attr('y1', d => d.source.y)
        .attr('x2', d => d.target.x).attr('y2', d => d.target.y);
      node.attr('transform', d => `translate(${d.x},${d.y})`);
    });
  }
}
