import { Box, useTheme } from '@mui/material';
import { BaseEdge, EdgeLabelRenderer, Position } from '@xyflow/react';
import type { Edge, EdgeProps } from '@xyflow/react';
import { memo } from 'react';
import { formatBandwidth, formatNumber, linkIsBusy } from './format';
import type { TopologyLink } from './types';

export type FlowTopologyLink = Edge<{
  link: TopologyLink;
  lane: number;
  showLabel: boolean;
  selectLink: (id: string) => void;
}, 'topologyLink'>;

function TopologyLinkView({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  markerEnd,
  selected,
  data,
}: EdgeProps<FlowTopologyLink>) {
  const theme = useTheme();

  if (!data) {
    return null;
  }

  const busy = linkIsBusy(data.link);
  let color = theme.palette.text.disabled;
  let strokeWidth = 1.5;

  if (busy) {
    color = theme.palette.info.main;
    strokeWidth = 2.2;
  }

  if (selected) {
    color = theme.palette.primary.main;
    strokeWidth = 3;
  }

  const controlDistance = Math.max(65, Math.abs(targetX - sourceX) * 0.4);
  const laneOffset = data.lane * 22;
  const sourceDirection = sourcePosition === Position.Left ? -1 : 1;
  const targetDirection = targetPosition === Position.Left ? -1 : 1;
  const control1X = sourceX + sourceDirection * controlDistance;
  const control2X = targetX + targetDirection * controlDistance;
  const control1Y = sourceY + laneOffset;
  const control2Y = targetY + laneOffset;
  const path = [
    `M ${sourceX},${sourceY}`,
    `C ${control1X},${control1Y}`,
    `${control2X},${control2Y}`,
    `${targetX},${targetY}`,
  ].join(' ');

  const labelX = (sourceX + targetX) / 2;
  const labelY = (sourceY + targetY) / 2 + laneOffset * 0.75;
  const showLabel = data.showLabel || selected;
  const bandwidth = formatBandwidth(data.link.config.bandwidthBitsPerSecond);
  const memberCount = data.link.memberCount ?? 1;
  const bandwidthLabel = memberCount > 1
    ? `${formatNumber(memberCount)} × ${bandwidth}`
    : bandwidth;
  const queuedPackets = data.link.stats.queuedPackets;
  const queueScope = memberCount > 1 ? 'queued total' : 'Q';
  const queueLabel = queuedPackets > 0 ? ` · ${queueScope} ${formatNumber(queuedPackets)}` : '';
  const label = `${bandwidthLabel}${queueLabel}`;
  const inspectLabel = memberCount > 1
    ? `Inspect ${formatNumber(memberCount)} independent links: ${data.link.label}`
    : `Inspect link ${data.link.label}`;

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={markerEnd}
        interactionWidth={24}
        style={{ stroke: color, strokeWidth }}
      />
      {showLabel && (
        <EdgeLabelRenderer>
          <Box
            component="button"
            type="button"
            className="nodrag nopan"
            aria-label={inspectLabel}
            title="Configured limit per link. Each member keeps its own transmitter and queue."
            onClick={(event) => {
              event.stopPropagation();
              data.selectLink(id);
            }}
            sx={{
              position: 'absolute',
              transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
              pointerEvents: 'all',
              border: '1px solid',
              borderColor: selected ? 'primary.main' : 'divider',
              borderRadius: 1,
              bgcolor: 'background.paper',
              color: 'text.primary',
              px: 0.75,
              py: 0.25,
              fontSize: 10,
              fontFamily: 'inherit',
              whiteSpace: 'nowrap',
              cursor: 'pointer',
            }}
          >
            {label}
          </Box>
        </EdgeLabelRenderer>
      )}
    </>
  );
}

export default memo(TopologyLinkView);
