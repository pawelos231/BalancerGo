import { useCallback, useEffect, useMemo, useState } from 'react';
import { kindLabel } from '../topology/format';
import type { TopologyGraph, TopologySelection } from '../topology/types';

export interface TopologySearchOption {
  kind: 'node' | 'link';
  id: string;
  label: string;
  detail: string;
}

export default function useTopologySelection(topology: TopologyGraph | null) {
  const [selection, setSelection] = useState<TopologySelection>(null);
  const nodes = topology?.nodes;
  const links = topology?.links;

  const nodeById = useMemo(
    () => new Map(nodes?.map((node) => [node.id, node]) ?? []),
    [nodes],
  );
  const linkById = useMemo(
    () => new Map(links?.map((link) => [link.id, link]) ?? []),
    [links],
  );
  const nodeLabels = useMemo(
    () => new Map(nodes?.map((node) => [node.id, node.label]) ?? []),
    [nodes],
  );

  const onSelect = useCallback((next: TopologySelection) => {
    setSelection((previous) => {
      const unchanged = previous?.kind === next?.kind && previous?.id === next?.id;
      return unchanged ? previous : next;
    });
  }, []);

  useEffect(() => {
    if (!selection) return;

    const index = selection.kind === 'node' ? nodeById : linkById;
    if (!index.has(selection.id)) {
      setSelection(null);
    }
  }, [selection, nodeById, linkById]);

  const options = useMemo<TopologySearchOption[]>(() => {
    const nodeOptions: TopologySearchOption[] = (nodes ?? []).map((node) => {
      const address = node.address ? ` · ${node.address}` : '';

      return {
        kind: 'node',
        id: node.id,
        label: node.label,
        detail: `${kindLabel(node.kind)}${address}`,
      };
    });

    const linkOptions: TopologySearchOption[] = (links ?? []).map((link) => {
      const source = nodeLabels.get(link.source) ?? link.source;
      const target = nodeLabels.get(link.target) ?? link.target;

      return {
        kind: 'link',
        id: link.id,
        label: link.label,
        detail: `${source} → ${target}`,
      };
    });

    return [...nodeOptions, ...linkOptions];
  }, [nodes, links, nodeLabels]);

  const selectedEntry = options.find((option) => (
    option.kind === selection?.kind && option.id === selection.id
  ));
  const selectedKind = selectedEntry?.kind;
  const selectedId = selectedEntry?.id;
  const selectedLabel = selectedEntry?.label;
  const selectedDetail = selectedEntry?.detail;

  // Keep the selected value stable so metric updates cannot reset an active search.
  const selectedOption = useMemo<TopologySearchOption | null>(() => {
    const missingSelection = selectedKind === undefined
      || selectedId === undefined
      || selectedLabel === undefined
      || selectedDetail === undefined;

    if (missingSelection) return null;

    return {
      kind: selectedKind,
      id: selectedId,
      label: selectedLabel,
      detail: selectedDetail,
    };
  }, [selectedKind, selectedId, selectedLabel, selectedDetail]);

  // Resolve the selected ID against each snapshot so the inspector shows fresh data.
  const selectedNode = selection?.kind === 'node'
    ? nodeById.get(selection.id) ?? null
    : null;
  const selectedLink = selection?.kind === 'link'
    ? linkById.get(selection.id) ?? null
    : null;

  return {
    selection,
    onSelect,
    options,
    selectedOption,
    selectedNode,
    selectedLink,
    nodeLabels,
  };
}
