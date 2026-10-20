import { Box, useTheme } from '@mui/material';
import { BaseEdge, EdgeLabelRenderer, Position } from '@xyflow/react';
import type { Edge, EdgeProps } from '@xyflow/react';
import { memo } from 'react';
import { formatBandwidth, linkIsBusy } from './format';
import type { TopologyLink } from './types';

export type FlowTopologyLink = Edge<{
  link: TopologyLink;
  lane: number;
  showLabel: boolean;
  selectLink: (id: string) => void;
}, 'topologyLink'>;

function TopologyLinkView({ id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, markerEnd, selected, data }: EdgeProps<FlowTopologyLink>) {
  const theme = useTheme();
  if (!data) return null;
  const busy = linkIsBusy(data.link);
  const color = selected ? theme.palette.primary.main : busy ? theme.palette.info.main : theme.palette.text.disabled;
  const distance = Math.max(65, Math.abs(targetX - sourceX) * 0.4);
  const laneOffset = data.lane * 22;
  const control1X = sourceX + (sourcePosition === Position.Left ? -distance : distance);
  const control2X = targetX + (targetPosition === Position.Left ? -distance : distance);
  const path = `M ${sourceX},${sourceY} C ${control1X},${sourceY + laneOffset} ${control2X},${targetY + laneOffset} ${targetX},${targetY}`;
  const labelX = (sourceX + targetX) / 2;
  const labelY = (sourceY + targetY) / 2 + laneOffset * 0.75;

  return (
    <>
      <BaseEdge id={id} path={path} markerEnd={markerEnd} interactionWidth={24} style={{ stroke: color, strokeWidth: selected ? 3 : busy ? 2.2 : 1.5 }} />
      {(data.showLabel || selected) && (
        <EdgeLabelRenderer>
          <Box
            component="button"
            type="button"
            className="nodrag nopan"
            aria-label={`Inspect link ${data.link.label}`}
            onClick={(event) => { event.stopPropagation(); data.selectLink(id); }}
            sx={{ position: 'absolute', transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`, pointerEvents: 'all', border: '1px solid', borderColor: selected ? 'primary.main' : 'divider', borderRadius: 1, bgcolor: 'background.paper', color: 'text.primary', px: 0.75, py: 0.25, fontSize: 10, fontFamily: 'inherit', whiteSpace: 'nowrap', cursor: 'pointer' }}
          >
            {formatBandwidth(data.link.config.bandwidthBitsPerSecond)}{data.link.stats.queuedPackets > 0 ? ` · Q ${data.link.stats.queuedPackets}` : ''}
          </Box>
        </EdgeLabelRenderer>
      )}
    </>
  );
}

export default memo(TopologyLinkView);
