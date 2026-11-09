import type { ExactInteger } from './types.ts';

export const INT64_MAX = (1n << 63n) - 1n;
export const INT64_MIN = -(1n << 63n);
export const UINT64_MAX = (1n << 64n) - 1n;

// Match endpoint comparison, including equivalent IPv6 and IPv4-mapped IPv6.
// The comparison key never replaces the address shown in the configuration.
export function endpointKey(address: string, port: ExactInteger): string {
  // ASCII casing is stable across Go and browser Unicode versions; other code points stay literal.
  const lowercase = address.replace(/[A-Z]/g, (letter) => letter.toLowerCase());
  const words = ipv6Words(lowercase);
  let host = lowercase;

  if (words) {
    const mappedIPv4 = words.slice(0, 5).every((word) => word === 0) && words[5] === 0xffff;
    host = mappedIPv4
      ? [words[6] >> 8, words[6] & 255, words[7] >> 8, words[7] & 255].join('.')
      : words.map((word) => word.toString(16)).join(':');
  }

  return JSON.stringify([host, BigInt(port).toString()]);
}

// Expand an IPv6 address to eight words; non-IP host strings stay unchanged.
function ipv6Words(address: string): number[] | null {
  if (!address.includes(':')) return null;

  let expanded = address;
  if (expanded.includes('.')) {
    const separator = expanded.lastIndexOf(':');
    const octets = expanded.slice(separator + 1).split('.');
    const validIPv4 = octets.length === 4 && octets.every((octet) => {
      const decimal = octet !== '' && !/[^0-9]/.test(octet);
      const noLeadingZero = octet === '0' || octet[0] !== '0';
      return decimal && noLeadingZero && Number(octet) <= 255;
    });
    if (!validIPv4) return null;

    const numbers = octets.map(Number);
    const high = ((numbers[0] << 8) | numbers[1]).toString(16);
    const low = ((numbers[2] << 8) | numbers[3]).toString(16);
    expanded = `${expanded.slice(0, separator + 1)}${high}:${low}`;
  }

  const halves = expanded.split('::');
  if (halves.length > 2) return null;
  const left = halves[0] === '' ? [] : halves[0].split(':');
  const right = halves.length === 1 || halves[1] === '' ? [] : halves[1].split(':');
  const words = [...left, ...right];
  if (!words.every((word) => word.length >= 1 && word.length <= 4 && !/[^0-9a-f]/.test(word))) return null;

  const missing = 8 - words.length;
  if (halves.length === 1 && missing !== 0) return null;
  if (halves.length === 2 && missing < 1) return null;

  return [
    ...left.map((word) => parseInt(word, 16)),
    ...Array<number>(missing).fill(0),
    ...right.map((word) => parseInt(word, 16)),
  ];
}
