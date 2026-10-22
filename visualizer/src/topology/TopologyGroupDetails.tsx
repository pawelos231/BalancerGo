import { Box, Button, Divider, LinearProgress, Stack, Typography } from '@mui/material';
import { formatBandwidth, formatNumber } from './format';
import { Fact, TrafficTotals } from './TopologyFacts';
import { describeLink, linkRole, linkValueRange, roleDetails, totalLinks } from './telemetry';
import type { TopologyBundle, TopologyGroup } from './grouping';
import type { TopologyGraph, TopologyNode, TopologySelection } from './types';

const MEMBER_LIMIT = 12;

// GroupDetailsProps connects group membership to selection and expansion.
export interface GroupDetailsProps {
  group: TopologyGroup;
  topology?: TopologyGraph | null;
  onToggleGroup?: (groupId: string) => void;
  onSelect?: (selection: TopologySelection) => void;
}

// Grouping changes representation; every member keeps its original link settings.
export function GroupDetails({ group, topology, onToggleGroup, onSelect }: GroupDetailsProps) {
  const activeClients = group.clients.filter((client) => client.active === true).length;
  const allActivityKnown = group.clients.every((client) => client.active !== undefined);
  const individualClients = group.clients.length - group.collapsedClients.length;
  const displayedClients = group.clients.slice(0, MEMBER_LIMIT);
  const clientIds = new Set(group.clients.map((client) => client.id));
  const memberLinks = topology?.links.filter((link) => (
    clientIds.has(link.source) || clientIds.has(link.target)
  )) ?? [];
  const bandwidth = linkValueRange(
    memberLinks,
    (link) => link.config.bandwidthBitsPerSecond,
    formatBandwidth,
  );

  return (
    <>
      <Box>
        <Typography variant="h6">{group.label}</Typography>
        <Typography variant="body2" color="text.secondary">Client group</Typography>
      </Box>
      <Typography variant="body2" color="text.secondary">
        Members keep their own access and return links, configurations and queues.
        This group summarizes the view while shared LB limits still apply.
      </Typography>
      <Stack component="dl" spacing={1.25} sx={{ m: 0 }}>
        <Fact label="Group ID" value={group.id} />
        <Fact label="Actual clients" value={formatNumber(group.clients.length)} />
        <Fact
          label={allActivityKnown ? 'Active clients' : 'Known active clients'}
          value={formatNumber(activeClients)}
        />
        <Fact label="Represented by the group" value={formatNumber(group.collapsedClients.length)} />
        <Fact label="Represented individually" value={formatNumber(individualClients)} />
        {memberLinks.length > 0 && <Fact label="Per-link bandwidth" value={bandwidth} />}
      </Stack>
      {onToggleGroup && (
        <Button variant="outlined" onClick={() => onToggleGroup(group.id)}>
          {group.expanded ? 'Collapse group' : 'Expand group'}
        </Button>
      )}
      <Typography variant="caption" color="text.secondary">
        Expanded members use individual nodes; graph limits may still clip the view.
      </Typography>
      <Divider />
      <Typography variant="subtitle2">
        Members · {formatNumber(displayedClients.length)} of {formatNumber(group.clients.length)}
      </Typography>
      <Stack spacing={0.5}>
        {displayedClients.map((client) => (
          <Button
            key={client.id}
            size="small"
            disabled={!onSelect}
            onClick={() => onSelect?.({ kind: 'node', id: client.id })}
            sx={{ justifyContent: 'flex-start', textAlign: 'left', textTransform: 'none' }}
          >
            <Box sx={{ minWidth: 0, overflowWrap: 'anywhere' }}>
              <Typography variant="body2">{client.label}</Typography>
              <Typography variant="caption" color="text.secondary">
                {client.address ?? client.id}
              </Typography>
            </Box>
          </Button>
        ))}
      </Stack>
      <Typography variant="caption" color="text.secondary">
        Search above by label, address or ID to inspect any member.
      </Typography>
    </>
  );
}

// BundleDetailsProps exposes the original links behind a bundled arrow.
export interface BundleDetailsProps {
  bundle: TopologyBundle;
  nodeById: ReadonlyMap<string, TopologyNode>;
  nodeLabels: Map<string, string>;
  onSelect?: (selection: TopologySelection) => void;
}

// A bundled arrow summarizes separate queues and transmitters, including mixed settings.
export function BundleDetails({ bundle, nodeById, nodeLabels, onSelect }: BundleDetailsProps) {
  const links = [...new Map(bundle.links.map((link) => [link.id, link])).values()];
  const totals = totalLinks(links);
  const displayedLinks = links.slice(0, MEMBER_LIMIT);
  const bandwidth = linkValueRange(
    links,
    (link) => link.config.bandwidthBitsPerSecond,
    formatBandwidth,
  );
  const travel = linkValueRange(
    links,
    (link) => link.config.packetTravelTimeMs,
    (value) => `${formatNumber(value, 6)} ms`,
  );
  const capacity = linkValueRange(
    links,
    (link) => link.config.queueCapacity,
    (value) => `${formatNumber(value)} packets`,
  );
  const loss = linkValueRange(
    links,
    (link) => link.config.lossRate,
    (value) => `${formatNumber(value * 100, 4)}%`,
  );
  const roles = new Set(links.map((link) => linkRole(link, nodeById)));
  const sharedRole = roles.size === 1 && links.length > 0
    ? describeLink(links[0], nodeById)
    : roleDetails.other;
  const transmittersLabel = `${formatNumber(totals.transmittingLinks)} of ${formatNumber(totals.linkCount)}`;
  const queuePercent = totals.queueCapacity > 0
    ? Math.min(100, totals.queuedPackets / totals.queueCapacity * 100)
    : 0;
  const linkWord = totals.linkCount === 1 ? 'link' : 'links';

  return (
    <>
      <Box>
        <Typography variant="h6">{formatNumber(totals.linkCount)} independent {linkWord}</Typography>
        <Typography variant="body2" color="text.secondary">{sharedRole.label}</Typography>
      </Box>
      <Typography variant="body2" color="text.secondary">
        {sharedRole.explanation} Each member retains its own configuration,
        transmitter and waiting queue.
      </Typography>
      <Stack component="dl" spacing={1.25} sx={{ m: 0 }}>
        <Fact label="Bandwidth per link" value={bandwidth} />
        <Fact label="Travel time per link" value={travel} />
        <Fact label="Capacity per queue" value={capacity} />
        <Fact label="Loss probability per link" value={loss} />
      </Stack>
      <Divider />
      <Typography variant="subtitle2">Activity across member links</Typography>
      <Stack component="dl" spacing={1.25} sx={{ m: 0 }}>
        <Fact label="Waiting packets" value={formatNumber(totals.queuedPackets)} />
        <Fact label="Slots across separate queues" value={formatNumber(totals.queueCapacity)} />
        <Fact label="Occupied transmitters" value={transmittersLabel} />
        <Fact label="Travelling packets" value={formatNumber(totals.travellingPackets)} />
      </Stack>
      {totals.queueCapacity > 0 && (
        <LinearProgress
          variant="determinate"
          value={queuePercent}
          aria-label="Combined waiting slots occupied across independent queues"
        />
      )}
      <Typography variant="caption" color="text.secondary">
        Slots belong to individual queues. A full member queue can drop packets
        while another still has free slots. Travelling packets leave transmitters free.
      </Typography>
      <Divider />
      <Typography variant="subtitle2">Totals across these physical links</Typography>
      <TrafficTotals stats={totals} />
      <Typography variant="caption" color="text.secondary">
        Tail drops happen before transmission; random losses happen after serialization.
        Byte totals include modeled headers. Per-link maxima describe configured limits.
      </Typography>
      <Divider />
      <Typography variant="subtitle2">
        Member links · {formatNumber(displayedLinks.length)} of {formatNumber(totals.linkCount)}
      </Typography>
      <Stack spacing={0.5}>
        {displayedLinks.map((member) => {
          const source = nodeLabels.get(member.source) ?? member.source;
          const target = nodeLabels.get(member.target) ?? member.target;
          const settings = [
            formatBandwidth(member.config.bandwidthBitsPerSecond),
            `${formatNumber(member.config.packetTravelTimeMs, 6)} ms`,
            `Q ${formatNumber(member.config.queueCapacity)}`,
            `${formatNumber(member.config.lossRate * 100, 4)}% loss`,
          ].join(' · ');

          return (
            <Button
              key={member.id}
              size="small"
              disabled={!onSelect}
              onClick={() => onSelect?.({ kind: 'link', id: member.id })}
              sx={{ justifyContent: 'flex-start', textAlign: 'left', textTransform: 'none' }}
            >
              <Box sx={{ minWidth: 0, overflowWrap: 'anywhere' }}>
                <Typography variant="body2">{source} → {target}</Typography>
                <Typography variant="caption" color="text.secondary">{settings}</Typography>
              </Box>
            </Button>
          );
        })}
      </Stack>
      <Typography variant="caption" color="text.secondary">
        Select a member for its exact settings. Search above to find other member links.
      </Typography>
    </>
  );
}

