// Arranges the whole library in the background, so the window never waits for
// it. The result is the same for the same library: d3-force starts every paper
// at a fixed place and draws its random numbers from a fixed seed.
'use strict';

importScripts('d3-dispatch.min.js', 'd3-quadtree.min.js', 'd3-timer.min.js', 'd3-force.min.js');

const TICKS = 300;

self.onmessage = ({ data }) => {
  const nodes = Array.from({ length: data.count }, () => ({}));
  const degree = new Int32Array(data.count);
  data.edges.forEach(([a, b]) => { degree[a]++; degree[b]++; });
  const links = data.edges.map(([source, target]) => ({ source, target }));
  const fewest = (l) => Math.min(degree[l.source.index], degree[l.target.index]);
  const sim = d3.forceSimulation(nodes)
    .force('charge', d3.forceManyBody().strength((n) => -14 - Math.min(degree[n.index], 80)).theta(1.1))
    // A paper with one link stays close by it, so hubs keep their own space.
    .force('link', d3.forceLink(links).distance((l) => (fewest(l) === 1 ? 16 : 70)).strength((l) => 1 / Math.max(1, fewest(l))))
    .force('x', d3.forceX().strength(0.04))
    .force('y', d3.forceY().strength(0.04))
    .stop();
  for (let i = 0; i < TICKS; i++) {
    sim.tick();
    if (i % 20 === 0) self.postMessage({ progress: i / TICKS });
  }
  const xy = new Float32Array(data.count * 2);
  nodes.forEach((n, i) => {
    xy[2 * i] = n.x;
    xy[2 * i + 1] = n.y;
  });
  self.postMessage({ xy }, [xy.buffer]);
};
