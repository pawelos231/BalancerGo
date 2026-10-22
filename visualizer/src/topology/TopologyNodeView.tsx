import { Box, Stack, Typography, useTheme } from '@mui/material';
import GroupsOutlinedIcon from '@mui/icons-material/GroupsOutlined';
import ComputerOutlinedIcon from '@mui/icons-material/ComputerOutlined';
import HubOutlinedIcon from '@mui/icons-material/HubOutlined';
import DnsOutlinedIcon from '@mui/icons-material/DnsOutlined';
import WidgetsOutlinedIcon from '@mui/icons-material/WidgetsOutlined';
import { Handle, Position } from '@xyflow/react';
import type { Node, NodeProps } from '@xyflow/react';
import { memo } from 'react';
import { formatMetric, kindLabel } from './format';
import { NODE_HEIGHT, NODE_WIDTH } from './layout';
import type { TopologyNode } from './types';

export type FlowTopologyNode = Node<{ node: TopologyNode }, 'topologyNode'>;

function TopologyNodeView({ data, selected }: NodeProps<FlowTopologyNode>) {
  const theme = useTheme();
  const node = data.node;
  let color = theme.palette.primary.main;
  let Icon = WidgetsOutlinedIcon;

  switch (node.kind) {
    case 'client':
      color = theme.palette.secondary.main;
      Icon = ComputerOutlinedIcon;
      break;
    case 'clientGroup':
      color = theme.palette.secondary.main;
      Icon = GroupsOutlinedIcon;
      break;
    case 'loadBalancer':
      Icon = HubOutlinedIcon;
      break;
    case 'backend':
      color = theme.palette.success.main;
      Icon = DnsOutlinedIcon;
      break;
  }

  if (node.active === false) {
    color = theme.palette.text.disabled;
  }

  const subtitle = node.address ?? kindLabel(node.kind);
  const displayedMetrics = node.metrics.slice(0, 2);

  return (
    <Box
      sx={{
        width: NODE_WIDTH,
        height: NODE_HEIGHT,
        boxSizing: 'border-box',
        border: '2px solid',
        borderColor: selected ? 'primary.main' : 'divider',
        borderRadius: 2,
        bgcolor: 'background.paper',
        boxShadow: selected ? 4 : 1,
        p: 1.5,
      }}
    >
      <Handle
        id="in-left"
        type="target"
        position={Position.Left}
        style={{ top: '32%', background: color }}
        isConnectable={false}
      />
      <Handle
        id="out-left"
        type="source"
        position={Position.Left}
        style={{ top: '68%', background: color }}
        isConnectable={false}
      />
      <Handle
        id="out-right"
        type="source"
        position={Position.Right}
        style={{ top: '32%', background: color }}
        isConnectable={false}
      />
      <Handle
        id="in-right"
        type="target"
        position={Position.Right}
        style={{ top: '68%', background: color }}
        isConnectable={false}
      />

      <Stack direction="row" spacing={1} alignItems="center">
        <Icon sx={{ color }} fontSize="small" />
        <Typography variant="subtitle2" noWrap title={node.label} sx={{ flex: 1 }}>
          {node.label}
        </Typography>
        {node.active !== undefined && (
          <Box
            component="span"
            aria-label={node.active ? 'Active' : 'Inactive'}
            sx={{
              width: 7,
              height: 7,
              flexShrink: 0,
              borderRadius: '50%',
              bgcolor: color,
            }}
          />
        )}
      </Stack>

      <Typography
        variant="caption"
        color="text.secondary"
        noWrap
        display="block"
        sx={{ mt: 0.25 }}
      >
        {subtitle}
      </Typography>

      <Stack direction="row" spacing={1.5} sx={{ mt: 0.75, overflow: 'hidden' }}>
        {displayedMetrics.map((metric) => (
          <Box key={metric.key} sx={{ minWidth: 0 }}>
            <Typography
              variant="caption"
              color="text.secondary"
              noWrap
              display="block"
              sx={{ fontSize: 10 }}
            >
              {metric.label}
            </Typography>
            <Typography variant="caption" fontWeight={600} noWrap display="block">
              {formatMetric(metric)}
            </Typography>
          </Box>
        ))}
      </Stack>
    </Box>
  );
}

export default memo(TopologyNodeView);
