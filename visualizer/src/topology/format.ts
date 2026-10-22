import type { TopologyLink, TopologyMetric } from './types';

const bandwidthUnits = [
  [1e12, 'Tbit/s'],
  [1e9, 'Gbit/s'],
  [1e6, 'Mbit/s'],
  [1e3, 'kbit/s'],
] as const;

const byteUnits = [
  [1e12, 'TB'],
  [1e9, 'GB'],
  [1e6, 'MB'],
  [1e3, 'kB'],
] as const;

const kindLabels: Record<string, string> = {
  client: 'TCP client',
  clientGroup: 'Client group',
  loadBalancer: 'Load balancer',
  networkAggregator: 'Upstream aggregation',
  backend: 'Backend',
};

export function formatNumber(value: number, maximumFractionDigits = 2): string {
  if (!Number.isFinite(value)) {
    return 'Unknown';
  }

  return value.toLocaleString('en-US', { maximumFractionDigits });
}

export function formatBandwidth(value: number): string {
  if (!Number.isFinite(value)) {
    return 'Unknown';
  }

  for (const [scale, unit] of bandwidthUnits) {
    if (value >= scale) {
      return `${formatNumber(value / scale)} ${unit}`;
    }
  }

  return `${formatNumber(value)} bit/s`;
}

export function formatBytes(value: number): string {
  for (const [scale, unit] of byteUnits) {
    if (value >= scale) {
      return `${formatNumber(value / scale)} ${unit}`;
    }
  }

  return `${formatNumber(value)} B`;
}

export function formatMetric(metric: TopologyMetric): string {
  switch (metric.unit) {
    case 'bytes':
    case 'B':
      return formatBytes(metric.value);
    case 'bit/s':
      return formatBandwidth(metric.value);
    default: {
      const suffix = metric.unit ? ` ${metric.unit}` : '';
      return `${formatNumber(metric.value)}${suffix}`;
    }
  }
}

export function kindLabel(kind: string): string {
  const fallback = kind
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .replace(/[_-]/g, ' ');

  return kindLabels[kind] ?? fallback;
}

export function linkIsBusy(link: TopologyLink): boolean {
  const { transmitting, queuedPackets, travellingPackets } = link.stats;

  return transmitting || queuedPackets > 0 || travellingPackets > 0;
}
