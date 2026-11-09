import { Divider, Grid, MenuItem, Stack, TextField, Typography } from '@mui/material';
import { useState } from 'react';
import { formatVirtualSeconds } from '../scenarios/plannedTopology';
import type { PlannedClientGroup, PlannedHost, PlannedTopology } from '../scenarios/plannedTopology';
import type { ExactInteger, ResolvedLinks, ScenarioLinkConfig, ScenarioStream } from '../scenarios/types';
import { formatNumber } from './format';
import { Fact, QueuePolicyFacts } from './TopologyFacts';

// Keep exact configured integers visible, including values above Number's safe range.
const integer = (value: ExactInteger) => BigInt(value).toLocaleString('en-US');
const seconds = (value: ExactInteger) => `${formatVirtualSeconds(value)} s`;

function PlannedResourceDetails({ plan, selection }: { plan: PlannedTopology; selection: string }) {
  const group = plan.clientGroups.find(item => `group:${item.id}` === selection);
  if (group) return <GeneratorDetails key={group.id} group={group} />;

  const backend = plan.backends.find(item => `backend:${item.id}` === selection);
  if (backend) {
    return (
      <Stack spacing={2}>
        <Typography variant="h6" component="h2">Backend: {backend.name || backend.id}</Typography>
        <Stack component="dl" spacing={1} sx={{ m: 0 }}>
          <Fact label="Resource ID" value={backend.id} />
          <Fact label="Address / port" value={`${backend.address} / ${backend.port}`} />
          <Fact label="Physical availability" value={backend.physicalAvailable ? 'Available' : 'Unavailable'} />
          <Fact label="Administrative state" value={backend.administrativeState} />
        </Stack>
        <Typography variant="caption" color="text.secondary">
          Availability is declared configuration. Observed health and existing mappings require runtime state.
        </Typography>
        <HostDetails host={backend.host} />
        <NetworkDetails network={backend.network} endpoint="Backend" />
      </Stack>
    );
  }

  const lb = plan.loadBalancer;
  return (
    <Stack spacing={2}>
      <Typography variant="h6" component="h2">Load balancer: {lb.id}</Typography>
      <Stack component="dl" spacing={1} sx={{ m: 0 }}>
        <Fact label="Algorithm for new mappings" value={lb.algorithm} />
        <Fact
          label="Shared forwarding budget (all outputs)"
          value={`${integer(lb.forwardingBitsPerSecond)} bit/s`}
        />
      </Stack>
      <HostDetails host={lb.host} />
      <LinkDetails title="Shared ingress (RX)" link={lb.ingress} />
      <Typography variant="caption" color="text.secondary">
        Both client-side and backend-side input queues share this RX bandwidth. Each side has
        the configured waiting capacity. Travel and loss apply once after shared RX transmission.
        Endpoint arrows have their own independent directional limits.
      </Typography>
    </Stack>
  );
}

function GeneratorDetails({ group }: { group: PlannedClientGroup }) {
  const [overrideIndex, setOverrideIndex] = useState('');
  const override = group.overrides.find(item => String(item.index) === overrideIndex);
  return (
    <Stack spacing={2}>
      <Typography variant="h6" component="h2">Client generator: {group.id}</Typography>
      <Stack component="dl" spacing={1} sx={{ m: 0 }}>
        <Fact label="Lifetime birth budget (count)" value={integer(group.count)} />
        <Fact label="Declared start time" value={seconds(group.startAtNanoseconds)} />
        <Fact label="Gap between client births" value={seconds(group.clientGapNanoseconds)} />
        <Fact label="Explicit configured overrides" value={group.overrides.length.toLocaleString('en-US')} />
      </Stack>
      <Typography variant="caption" color="text.secondary">
        The budget is cumulative across the run, not births repeated at this state. This view
        does not determine which slots have been created or which connections remain active.
      </Typography>
      {group.overrides.length > 0 && (
        <TextField
          select
          label="Generator defaults or configured slot"
          value={override ? overrideIndex : ''}
          onChange={event => setOverrideIndex(event.target.value)}
          fullWidth
        >
          <MenuItem value="">Group defaults (slots without an override)</MenuItem>
          {group.overrides.map(item => (
            <MenuItem key={String(item.index)} value={String(item.index)}>
              Slot {item.index}{item.reserved ? ' · configured reserved slot (outside current budget)' : ''}
            </MenuItem>
          ))}
        </TextField>
      )}
      <Typography variant="subtitle2">
        {override ? `Effective settings for slot ${override.index}` : 'Effective group defaults'}
      </Typography>
      {override?.reserved && (
        <Typography variant="body2" color="text.secondary">
          This configured reserved slot is outside the current birth budget. Its declaration
          does not establish whether a historical client still exists.
        </Typography>
      )}
      <StreamDetails stream={override?.stream ?? group.stream} />
      <NetworkDetails network={override?.network ?? group.network} endpoint="Client" />
    </Stack>
  );
}

function HostDetails({ host }: { host: PlannedHost }) {
  return (
    <Stack spacing={1}>
      <Typography variant="subtitle2">CPU configuration</Typography>
      <Stack component="dl" spacing={1} sx={{ m: 0 }}>
        <Fact label="Virtual cores" value={integer(host.cores)} />
        <Fact label="Packet processing time" value={seconds(host.packetProcessingTimeNanoseconds)} />
        <Fact label="CPU waiting capacity" value={`${integer(host.queueCapacity)} packets`} />
      </Stack>
      {BigInt(host.packetProcessingTimeNanoseconds) === 0n && (
        <Typography variant="caption" color="text.secondary">
          Zero processing time uses the ideal host model.
        </Typography>
      )}
    </Stack>
  );
}

function StreamDetails({ stream }: { stream: ScenarioStream }) {
  return (
    <Stack spacing={1}>
      <Typography variant="subtitle2">Configured stream per generated client</Typography>
      <Stack component="dl" spacing={1} sx={{ m: 0 }}>
        <Fact label="Payload budget" value={`${integer(stream.bytes)} bytes`} />
        <Fact
          label="Packet-count fallback setting"
          value={`${integer(stream.packets)} × 1024 bytes (when bytes = -1)`}
        />
        <Fact label="Packet generation gap" value={seconds(stream.packetGapNanoseconds)} />
      </Stack>
    </Stack>
  );
}

function NetworkDetails({ network, endpoint }: { network: ResolvedLinks; endpoint: string }) {
  return (
    <>
      <Divider />
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 6 }}>
          <LinkDetails title={`${endpoint} → LB`} link={network.toLoadBalancer} />
        </Grid>
        <Grid size={{ xs: 12, md: 6 }}>
          <LinkDetails title={`LB → ${endpoint}`} link={network.fromLoadBalancer} />
        </Grid>
      </Grid>
      <Typography variant="caption" color="text.secondary">
        Effective limits include network defaults, the selected profile and directional patches.
        Each client slot has separate links; group budgets do not multiply the shared LB limits.
      </Typography>
    </>
  );
}

function LinkDetails({ title, link }: { title: string; link: ScenarioLinkConfig }) {
  return (
    <Stack spacing={1}>
      <Typography variant="subtitle2">{title}</Typography>
      <Stack component="dl" spacing={1} sx={{ m: 0 }}>
        <Fact label="Bandwidth limit" value={`${integer(link.bandwidthBitsPerSecond)} bit/s`} />
        <Fact label="Travel time" value={seconds(link.packetTravelTimeNanoseconds)} />
        <Fact label="Waiting capacity" value={`${integer(link.queueCapacity)} packets`} />
        <Fact label="Configured random loss" value={`${formatNumber(link.lossRate * 100, 4)}%`} />
        <QueuePolicyFacts config={link} />
      </Stack>
    </Stack>
  );
}

export default PlannedResourceDetails;
