import { Box, Stack, Typography, useTheme } from '@mui/material';
import GroupsOutlinedIcon from '@mui/icons-material/GroupsOutlined';
import ComputerOutlinedIcon from '@mui/icons-material/ComputerOutlined';
import HubOutlinedIcon from '@mui/icons-material/HubOutlined';
import DnsOutlinedIcon from '@mui/icons-material/DnsOutlined';
import WidgetsOutlinedIcon from '@mui/icons-material/WidgetsOutlined';
import { Handle, Position } from '@xyflow/react';
import type { Node, NodeProps } from '@xyflow/react';
import { memo } from 'react';
import { formatMetric, formatNumber, kindLabel } from './format';
import { nodeHeight, NODE_WIDTH } from './layout';
import TopologyTrafficRates from './TopologyTrafficRates';
import type { TopologyIngress, TopologyNode } from './types';

export type FlowTopologyNode = Node<{
  node: TopologyNode;
  ingress?: TopologyIngress;
  domainLayout?: boolean;
}, 'topologyNode'>;

const inputSides = [
  { side: 'clients', label: 'Frontside · clients' },
  { side: 'backends', label: 'Backside · backends' },
] as const;

// Two waiting queues use one shared receiver; their slots stay independent.
function LoadBalancerInputs({ ingress }: { ingress: TopologyIngress }) {
  return (
    <Stack spacing={0.75} sx={{ mt: 1.5, pt: 1, borderTop: '1px solid', borderColor: 'divider' }}>
      <Typography variant="caption" fontWeight={600}>Input queues</Typography>
      {inputSides.map(({ side, label }) => {
        const queue = ingress.queues.find((entry) => entry.side === side);
        if (!queue) return null;
        const occupancy = `${formatNumber(queue.stats.queuedPackets)} / ${formatNumber(queue.capacity)}`;

        return (
          <Stack key={side} direction="row" justifyContent="space-between" spacing={1}>
            <Typography variant="caption" color="text.secondary">{label}</Typography>
            <Typography variant="caption" fontWeight={600} title="Waiting packets / waiting slots">
              {occupancy}
            </Typography>
          </Stack>
        );
      })}
      <Typography variant="caption" color="text.secondary" sx={{ fontSize: 10 }}>
        Both queues use the shared RX limit.
      </Typography>
    </Stack>
  );
}

function TopologyNodeView({ data, selected }: NodeProps<FlowTopologyNode>) {
  const theme = useTheme();
  const node = data.node;
  const isLoadBalancer = node.kind === 'loadBalancer';
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
  // Keep both shared LB limits visible directly on the diagram.
  const limitMetrics = node.metrics.filter((metric) => (
    metric.key === 'ingressBitsPerSecond' || metric.key === 'forwardingBitsPerSecond'
  ));
  const displayedMetrics = isLoadBalancer && limitMetrics.length > 0
    ? limitMetrics.slice(0, 2)
    : node.metrics.slice(0, 2);

  return (
    <Box
      sx={{
        width: NODE_WIDTH,
        height: nodeHeight(node.kind),
        position: 'relative',
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
        style={{ top: '25%', background: color }}
        isConnectable={false}
      />
      <Handle
        id="out-left"
        type="source"
        position={Position.Left}
        style={{ top: '75%', background: color }}
        isConnectable={false}
      />
      <Handle
        id="out-right"
        type="source"
        position={Position.Right}
        style={{ top: '25%', background: color }}
        isConnectable={false}
      />
      <Handle
        id="in-right"
        type="target"
        position={Position.Right}
        style={{ top: '75%', background: color }}
        isConnectable={false}
      />

      {isLoadBalancer && data.domainLayout && (
        <>
          {[
            { label: 'RX', left: -27, top: '25%', title: 'Frontside input from clients' },
            { label: 'TX', left: -27, top: '75%', title: 'Frontside output to clients' },
            { label: 'TX', right: -27, top: '25%', title: 'Backside output to backends' },
            { label: 'RX', right: -27, top: '75%', title: 'Backside input from backends' },
          ].map(({ label, title, ...position }) => (
            <Typography
              key={title}
              variant="caption"
              title={title}
              sx={{ position: 'absolute', ...position, transform: 'translateY(-50%)', fontSize: 10 }}
            >
              {label}
            </Typography>
          ))}
        </>
      )}

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
              {isLoadBalancer && metric.key === 'ingressBitsPerSecond'
                ? 'RX shared limit'
                : isLoadBalancer && metric.key === 'forwardingBitsPerSecond'
                  ? 'TX shared budget'
                  : metric.label}
            </Typography>
            <Typography variant="caption" fontWeight={600} noWrap display="block">
              {formatMetric(metric)}
            </Typography>
          </Box>
        ))}
      </Stack>
      <TopologyTrafficRates traffic={node.traffic} grouped={node.kind === 'clientGroup'} />
      {isLoadBalancer && data.ingress && <LoadBalancerInputs ingress={data.ingress} />}
    </Box>
  );
}

export default memo(TopologyNodeView);
