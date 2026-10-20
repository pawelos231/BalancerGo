import type { TopologyLink, TopologyMetric } from './types';

export function formatNumber(value: number, maximumFractionDigits = 2): string {
  return Number.isFinite(value) ? value.toLocaleString('en-US', { maximumFractionDigits }) : 'Unknown';
}

export function formatBandwidth(value: number): string {
  if (!Number.isFinite(value)) return 'Unknown';
  for (const [scale, unit] of [[1e12, 'Tbit/s'], [1e9, 'Gbit/s'], [1e6, 'Mbit/s'], [1e3, 'kbit/s']] as const) {
    if (value >= scale) return `${formatNumber(value / scale)} ${unit}`;
  }
  return `${formatNumber(value)} bit/s`;
}

export function formatBytes(value: number): string {
  for (const [scale, unit] of [[1e12, 'TB'], [1e9, 'GB'], [1e6, 'MB'], [1e3, 'kB']] as const) {
    if (value >= scale) return `${formatNumber(value / scale)} ${unit}`;
  }
  return `${formatNumber(value)} B`;
}

export function formatMetric(metric: TopologyMetric): string {
  if (metric.unit === 'bytes' || metric.unit === 'B') return formatBytes(metric.value);
  if (metric.unit === 'bit/s') return formatBandwidth(metric.value);
  return `${formatNumber(metric.value)}${metric.unit ? ` ${metric.unit}` : ''}`;
}

export function kindLabel(kind: string): string {
  const names: Record<string, string> = { clientGroup: 'Client group', loadBalancer: 'Load balancer', backend: 'Backend' };
  return names[kind] ?? kind.replace(/([a-z])([A-Z])/g, '$1 $2').replace(/[_-]/g, ' ');
}

export function linkIsBusy(link: TopologyLink): boolean {
  return link.stats.transmitting || link.stats.queuedPackets > 0 || link.stats.travellingPackets > 0;
}
