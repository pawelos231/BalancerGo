import { Box, useTheme } from '@mui/material';
import { Background, Controls, MarkerType, ReactFlow } from '@xyflow/react';
import type { ReactFlowInstance } from '@xyflow/react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import '@xyflow/react/dist/style.css';
import { layoutTopology, NODE_HEIGHT, NODE_WIDTH, structureKey } from './layout';
import TopologyNodeView from './TopologyNodeView';
import type { FlowTopologyNode } from './TopologyNodeView';
import TopologyLinkView from './TopologyLinkView';
import type { FlowTopologyLink } from './TopologyLinkView';
import { formatBandwidth, kindLabel, linkIsBusy } from './format';
import type { TopologyGraph, TopologySelection } from './types';

const nodeTypes = { topologyNode: TopologyNodeView };
const edgeTypes = { topologyLink: TopologyLinkView };

interface TopologyCanvasProps {
  graph: TopologyGraph;
  selection: TopologySelection;
  onSelect: (selection: TopologySelection) => void;
}

function TopologyCanvas({ graph, selection, onSelect }: TopologyCanvasProps) {
  const theme = useTheme();
  const instance = useRef<ReactFlowInstance<FlowTopologyNode, FlowTopologyLink> | null>(null);
  const [measurements, setMeasurements] = useState(() => new Map<string, { width: number; height: number }>());
  const key = structureKey(graph);
  const layout = useMemo(() => layoutTopology(key), [key]);
  const selectLink = useCallback((id: string) => onSelect({ kind: 'link', id }), [onSelect]);
  const nodes = useMemo<FlowTopologyNode[]>(() => graph.nodes.map((node) => ({
    id: node.id, type: 'topologyNode', position: layout.positions.get(node.id)!,
    // Measurements belong to the renderer, not the live snapshot. Omitting
    // them from a refreshed controlled node also resets React Flow's handles.
    measured: measurements.get(node.id),
    data: { node }, selected: selection?.kind === 'node' && selection.id === node.id,
    draggable: false, connectable: false, deletable: false,
    ariaLabel: `${node.label}, ${kindLabel(node.kind)}. Press Enter to inspect.`,
    style: { width: NODE_WIDTH, height: NODE_HEIGHT },
  })), [graph.nodes, layout, selection, measurements]);
  const edges = useMemo<FlowTopologyLink[]>(() => graph.links.map((link) => ({
    id: link.id, type: 'topologyLink', source: link.source, target: link.target,
    ...layout.routes.get(link.id),
    data: { link, lane: layout.routes.get(link.id)!.lane, showLabel: graph.nodes.length <= 30, selectLink },
    selected: selection?.kind === 'link' && selection.id === link.id,
    deletable: false, reconnectable: false,
    ariaLabel: `${link.label}, ${formatBandwidth(link.config.bandwidthBitsPerSecond)}, ${linkIsBusy(link) ? 'busy' : 'idle'}. Press Enter to inspect.`,
    markerEnd: { type: MarkerType.ArrowClosed, color: theme.palette.text.secondary, width: 16, height: 16 },
  })), [graph.links, graph.nodes.length, layout, selection, selectLink, theme.palette.text.secondary]);
  const focusedNodeId = selection?.kind === 'node' ? selection.id
    : selection?.kind === 'link' ? graph.links.find((link) => link.id === selection.id)?.source : undefined;

  // User selection can focus a distant element, while live statistics never
  // change layout or viewport. No fitView calls are tied to snapshot objects.
  useEffect(() => {
    if (!focusedNodeId || !instance.current) return;
    const position = layout.positions.get(focusedNodeId);
    if (position) void instance.current.setCenter(position.x + NODE_WIDTH / 2, position.y + NODE_HEIGHT / 2, { zoom: Math.max(instance.current.getZoom(), 0.65), duration: 180 });
  }, [focusedNodeId, layout]);

  return (
    <Box
      onKeyDown={(event) => { if (event.key === 'Escape') onSelect(null); }}
      sx={{ height: { xs: 480, md: 'min(72vh, 800px)' }, minHeight: 420, width: '100%', bgcolor: 'background.default', borderRadius: 2, overflow: 'hidden', '& .react-flow__controls-button': { bgcolor: 'background.paper', color: 'text.primary', borderColor: 'divider' }, '& .react-flow__node:focus-visible, & .react-flow__edge:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: 3 } }}
    >
      <ReactFlow<FlowTopologyNode, FlowTopologyLink>
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onInit={(flow) => { instance.current = flow; }}
        onNodeClick={(_, node) => onSelect({ kind: 'node', id: node.id })}
        onEdgeClick={(_, edge) => selectLink(edge.id)}
        onPaneClick={() => onSelect(null)}
        onNodesChange={(changes) => {
          if (changes.some((change) => change.type === 'dimensions' && change.dimensions)) {
            setMeasurements((previous) => {
              let next = previous;
              for (const change of changes) {
                if (change.type !== 'dimensions' || !change.dimensions) continue;
                const old = previous.get(change.id);
                if (old?.width === change.dimensions.width && old.height === change.dimensions.height) continue;
                if (next === previous) next = new Map(previous);
                next.set(change.id, change.dimensions);
              }
              return next;
            });
          }
          const picked = changes.find((change) => change.type === 'select' && change.selected);
          if (picked?.type === 'select') onSelect({ kind: 'node', id: picked.id });
        }}
        onEdgesChange={(changes) => {
          const picked = changes.find((change) => change.type === 'select' && change.selected);
          if (picked?.type === 'select') selectLink(picked.id);
        }}
        fitView
        fitViewOptions={{ padding: 0.2, minZoom: 0.08, maxZoom: 1 }}
        minZoom={0.08}
        maxZoom={2}
        nodesDraggable={false}
        nodesConnectable={false}
        edgesReconnectable={false}
        nodesFocusable
        edgesFocusable
        deleteKeyCode={null}
        onlyRenderVisibleElements
        colorMode={theme.palette.mode}
        aria-label="Read-only network topology. Select a node or directed link to inspect it."
      >
        <Background gap={22} size={1} color={theme.palette.divider} />
        <Controls showInteractive={false} />
      </ReactFlow>
    </Box>
  );
}

export default TopologyCanvas;
