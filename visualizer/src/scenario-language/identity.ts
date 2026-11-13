import type { Token } from './lexer.ts';
import { LanguageError } from './source.ts';

const prefixPattern = /^[a-z]+(?:-[a-z]+)*$/;
const endpointPattern = /^[a-z]+(?:-[a-z]+)*-(0|[1-9][0-9]*)$/;

// Groups and the LB use a plain prefix, for example regular or web-api.
export function prefixName(token: Token): string {
  return checkedName(token, prefixPattern, 'Expected a lowercase name with letters and internal hyphens.');
}

// A single endpoint has one final index: api-0 is valid; api2-0 and api-01 are not.
export function endpointName(token: Token): string {
  return checkedName(token, endpointPattern, 'Expected an endpoint name such as api-0, without digits in its prefix.');
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
