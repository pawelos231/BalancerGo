import { useCallback, useEffect, useMemo, useState } from 'react';
import { groupTopology } from '../topology/grouping';
import { visibleGraph } from '../topology/layout';
import type { TopologyGraph, TopologySelection } from '../topology/types';
import useTopologySelection from './useTopologySelection';

// Keep physical resources searchable alongside the current presentation groups.
function searchableGraph(raw: TopologyGraph, displayed: TopologyGraph): TopologyGraph {
  const nodes = new Map(raw.nodes.map((node) => [node.id, node]));
  const links = new Map(raw.links.map((link) => [link.id, link]));
  for (const node of displayed.nodes) {
    nodes.set(node.id, node);
  }
  for (const link of displayed.links) {
    links.set(link.id, link);
  }
  return { ...raw, nodes: [...nodes.values()], links: [...links.values()] };
}

// Grouping changes the drawing; the original topology remains the source of truth.
export default function useTopologyView(topology: TopologyGraph | null) {
  const [selection, setSelection] = useState<TopologySelection>(null);
  const [expandedGroups, setExpandedGroups] = useState<ReadonlySet<string>>(
    () => new Set(),
  );

  const onSelect = useCallback((next: TopologySelection) => {
    setSelection((previous) => {
      const unchanged = previous?.kind === next?.kind && previous?.id === next?.id;
      return unchanged ? previous : next;
    });
  }, []);

  const grouped = useMemo(
    () => topology ? groupTopology(topology, expandedGroups, selection) : null,
    [topology, expandedGroups, selection],
  );
  const searchable = useMemo(
    () => topology && grouped ? searchableGraph(topology, grouped.graph) : null,
    [topology, grouped],
  );
  const search = useTopologySelection(searchable, selection, onSelect);
  const graph = useMemo(
    () => grouped ? visibleGraph(grouped.graph, selection) : null,
    [grouped, selection],
  );

  const groups = grouped?.groups;
  useEffect(() => {
    const available = new Set(groups?.map((group) => group.id));
    setExpandedGroups((previous) => {
      const remaining = [...previous].filter((id) => available.has(id));
      return remaining.length === previous.size ? previous : new Set(remaining);
    });
  }, [groups]);

  // Collapsing a selected client moves focus to its group instead of reopening it.
  function toggleGroup(groupId: string) {
    const group = groups?.find((candidate) => candidate.id === groupId);
    if (!group) return;

    setExpandedGroups((previous) => {
      const next = new Set(previous);
      if (next.has(groupId)) {
        next.delete(groupId);
      } else {
        next.add(groupId);
      }
      return next;
    });

    onSelect(group.expanded ? { kind: 'node', id: group.nodeId } : null);
  }

  const selectedGroup = groups?.find((group) => (
    selection?.kind === 'node' && group.nodeId === selection.id
  )) ?? null;
  const selectedBundle = selection?.kind === 'link'
    ? grouped?.bundles.get(selection.id) ?? null
    : null;

  return {
    ...search,
    graph,
    grouped,
    selectedGroup,
    selectedBundle,
    toggleGroup,
    viewKey: JSON.stringify([...expandedGroups].sort()),
  };
}
