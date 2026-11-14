import type { Token } from './lexer.ts';
import { LanguageError } from './source.ts';

const prefixPattern = /^[a-z]+(?:-[a-z]+)*$/;
const endpointPattern = /^[a-z]+(?:-[a-z]+)*-(0|[1-9][0-9]*)$/;
const clientGroupPattern = /^[a-z]+(?:-[a-z0-9]+)*$/;
const clientAliasPattern = /^[a-z]+(?:-[a-z0-9]+)*-(0|[1-9][0-9]*)$/;

// The LB, profiles and backend templates use a plain prefix, for example web-api.
export function prefixName(token: Token): string {
  return checkedName(token, prefixPattern, 'Expected a lowercase name with letters and internal hyphens.');
}

// A single endpoint has one final index: api-0 is valid; api2-0 and api-01 are not.
export function endpointName(token: Token): string {
  return checkedName(token, endpointPattern, 'Expected an endpoint name such as api-0, without digits in its prefix.');
}

// Workload groups retain names such as wave-20s without relaxing backend identities.
export function clientGroupName(token: Token): string {
  return checkedName(token, clientGroupPattern, 'Expected a lowercase client group name with letters and optional hyphen-separated digits.');
}

// The last dash separates a group name from its canonical zero-based client index.
export function clientAliasName(token: Token): string {
  return checkedName(token, clientAliasPattern, 'Expected an endpoint name for a client alias, such as regular-0 or wave-20s-0.');
}

function checkedName(token: Token, pattern: RegExp, message: string): string {
  if (token.kind !== 'word' || token.text.length > 128 || !pattern.test(token.text)) {
    throw new LanguageError(message, token.range);
  }
  return token.text;
}

// Reserve addresses in ASCII ID order, using 254 host addresses in each third octet.
export function backendAddresses(ids: string[]): Map<string, string> {
  return new Map([...ids].sort().map((id, index) => [
    id, `10.0.${Math.floor(index / 254)}.${index % 254 + 1}`,
  ]));
}
