import { Box, useTheme } from '@mui/material';
import { Background, Controls, MarkerType, ReactFlow } from '@xyflow/react';
import type { EdgeChange, NodeChange, ReactFlowInstance } from '@xyflow/react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import '@xyflow/react/dist/style.css';
import { layoutTopology, NODE_HEIGHT, NODE_WIDTH, structureKey } from './layout';
import TopologyNodeView from './TopologyNodeView';
import type { FlowTopologyNode } from './TopologyNodeView';
import TopologyLinkView from './TopologyLinkView';
import type { FlowTopologyLink } from './TopologyLinkView';
import { formatBandwidth, kindLabel, linkIsBusy } from './format';
import type { TopologyGraph, TopologyLink, TopologySelection } from './types';

const nodeTypes = { topologyNode: TopologyNodeView };
const edgeTypes = { topologyLink: TopologyLinkView };

type FlowInstance = ReactFlowInstance<FlowTopologyNode, FlowTopologyLink>;
type NodeChanges = NodeChange<FlowTopologyNode>[];
type Measurements = Map<string, { width: number; height: number }>;

interface TopologyCanvasProps {
  graph: TopologyGraph;
  selection: TopologySelection;
  onSelect: (selection: TopologySelection) => void;
}

function updateMeasurements(previous: Measurements, changes: NodeChanges): Measurements {
  let next = previous;

  for (const change of changes) {
    if (change.type !== 'dimensions' || !change.dimensions) {
      continue;
    }

    const old = previous.get(change.id);
    const sameWidth = old?.width === change.dimensions.width;
    const sameHeight = old?.height === change.dimensions.height;

    if (sameWidth && sameHeight) {
      continue;
    }

    if (next === previous) {
      next = new Map(previous);
    }
    next.set(change.id, change.dimensions);
  }

  return next;
}

function findFocusedNodeId(selection: TopologySelection, links: TopologyLink[]) {
  if (selection?.kind === 'node') {
    return selection.id;
  }
  if (selection?.kind === 'link') {
    return links.find((link) => link.id === selection.id)?.source;
  }

  return undefined;
}

function TopologyCanvas({ graph, selection, onSelect }: TopologyCanvasProps) {
  const theme = useTheme();
  const instance = useRef<FlowInstance | null>(null);
  const [measurements, setMeasurements] = useState<Measurements>(() => new Map());
  const key = structureKey(graph);
  const layout = useMemo(() => layoutTopology(key), [key]);

  const selectLink = useCallback((id: string) => {
    onSelect({ kind: 'link', id });
  }, [onSelect]);

  const nodes = useMemo<FlowTopologyNode[]>(() => (
    graph.nodes.map((node) => ({
      id: node.id,
      type: 'topologyNode',
      position: layout.positions.get(node.id)!,
      // Retain measurements so fresh snapshots cannot hide nodes or reset handles.
      measured: measurements.get(node.id),
      data: { node },
      selected: selection?.kind === 'node' && selection.id === node.id,
      draggable: false,
      connectable: false,
      deletable: false,
      ariaLabel: `${node.label}, ${kindLabel(node.kind)}. Press Enter to inspect.`,
      style: { width: NODE_WIDTH, height: NODE_HEIGHT },
    }))
  ), [graph.nodes, layout, selection, measurements]);

  const edges = useMemo<FlowTopologyLink[]>(() => (
    graph.links.map((link) => {
      const route = layout.routes.get(link.id)!;
      const bandwidth = formatBandwidth(link.config.bandwidthBitsPerSecond);
      const activity = linkIsBusy(link) ? 'busy' : 'idle';

      return {
        id: link.id,
        type: 'topologyLink',
        source: link.source,
        target: link.target,
        ...route,
        data: {
          link,
          lane: route.lane,
          showLabel: graph.nodes.length <= 30,
          selectLink,
        },
        selected: selection?.kind === 'link' && selection.id === link.id,
        deletable: false,
        reconnectable: false,
        ariaLabel: `${link.label}, ${bandwidth}, ${activity}. Press Enter to inspect.`,
        markerEnd: {
          type: MarkerType.ArrowClosed,
          color: theme.palette.text.secondary,
          width: 16,
          height: 16,
        },
      };
    })
  ), [graph.links, graph.nodes.length, layout, selection, selectLink, theme.palette.text.secondary]);

  const focusedNodeId = findFocusedNodeId(selection, graph.links);

  // Only selection or placement changes may move the viewport.
  useEffect(() => {
    const flow = instance.current;
    if (!focusedNodeId || !flow) {
      return;
    }

    const position = layout.positions.get(focusedNodeId);
    if (!position) {
      return;
    }

    const centerX = position.x + NODE_WIDTH / 2;
    const centerY = position.y + NODE_HEIGHT / 2;
    const zoom = Math.max(flow.getZoom(), 0.65);

    void flow.setCenter(centerX, centerY, { zoom, duration: 180 });
  }, [focusedNodeId, layout]);

  function handleNodesChange(changes: NodeChanges) {
    const hasMeasurements = changes.some((change) => (
      change.type === 'dimensions' && change.dimensions
    ));

    if (hasMeasurements) {
      setMeasurements((previous) => updateMeasurements(previous, changes));
    }

    const picked = changes.find((change) => change.type === 'select' && change.selected);
    if (picked?.type === 'select') {
      onSelect({ kind: 'node', id: picked.id });
    }
  }

  function handleEdgesChange(changes: EdgeChange<FlowTopologyLink>[]) {
    const picked = changes.find((change) => change.type === 'select' && change.selected);
    if (picked?.type === 'select') {
      selectLink(picked.id);
    }
  }

  return (
    <Box
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          onSelect(null);
        }
      }}
      sx={{
        height: { xs: 480, md: 'min(72vh, 800px)' },
        minHeight: 420,
        width: '100%',
        bgcolor: 'background.default',
        borderRadius: 2,
        overflow: 'hidden',
        '& .react-flow__controls-button': {
          bgcolor: 'background.paper',
          color: 'text.primary',
          borderColor: 'divider',
        },
        '& .react-flow__node:focus-visible, & .react-flow__edge:focus-visible': {
          outline: '2px solid',
          outlineColor: 'primary.main',
          outlineOffset: 3,
        },
      }}
    >
      <ReactFlow<FlowTopologyNode, FlowTopologyLink>
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onInit={(flow) => {
          instance.current = flow;
        }}
        onNodeClick={(_, node) => onSelect({ kind: 'node', id: node.id })}
        onEdgeClick={(_, edge) => selectLink(edge.id)}
        onPaneClick={() => onSelect(null)}
        onNodesChange={handleNodesChange}
        onEdgesChange={handleEdgesChange}
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
