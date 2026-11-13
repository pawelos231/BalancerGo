import type { Token } from './lexer.ts';
import { LanguageError } from './source.ts';
import type { SourceRange } from './source.ts';
import type { Property } from './syntax.ts';

export type FieldRule =
  | { path: string; read: (token: Token) => unknown; nullable?: boolean }
  | { path: string; fields: FieldRules };
export type FieldRules = Readonly<Record<string, FieldRule>>;

// Translate known fields and remember their origins; no resource-family rules live here.
export class FieldWriter {
  readonly locations: Map<string, SourceRange>;
  private readonly written = new Set<string>();

  constructor(locations = new Map<string, SourceRange>()) {
    this.locations = locations;
  }

  hasWritten(path: string): boolean {
    return this.written.has(path);
  }

  // Aliases share a destination, so mss and another alias cannot silently overwrite each other.
  apply(properties: Property[], rules: FieldRules, target: object, path: string): void {
    const names = new Set<string>();
    for (const property of properties) {
      const name = property.key.text;
      const known = Object.prototype.hasOwnProperty.call(rules, name);
      if (!known) throw new LanguageError(`Unknown setting ${JSON.stringify(name)}.`, property.key.range);
      if (names.has(name)) throw new LanguageError(`Duplicate setting ${JSON.stringify(name)}.`, property.key.range);
      names.add(name);

      const rule = rules[name];
      const nativePath = path ? `${path}.${rule.path}` : rule.path;
      if ('fields' in rule) {
        if (!Array.isArray(property.value)) {
          throw new LanguageError(`Expected a block for ${name}.`, property.key.range);
        }
        const nested = this.destination(target, rule.path);
        this.locations.set(nativePath, property.key.range);
        this.apply(property.value, rule.fields, nested, nativePath);
        continue;
      }

      if (Array.isArray(property.value)) {
        throw new LanguageError(`Expected a scalar value for ${name}.`, property.key.range);
      }
      if (this.written.has(nativePath)) {
        throw new LanguageError(`Setting ${name} overlaps an earlier value.`, property.key.range);
      }
      const token = property.value;
      const inherit = token.kind === 'word' && token.text === 'null' && rule.nullable;
      const value = inherit ? null : rule.read(token);
      const parts = rule.path.split('.');
      const field = parts.pop()!;
      const parent = this.destination(target, parts.join('.'));
      parent[field] = value;
      this.written.add(nativePath);
      this.locations.set(nativePath, token.range);
    }
  }

  // Follow trusted native paths such as rto.minimumTimeoutNanoseconds, creating blocks if absent.
  private destination(target: object, path: string): Record<string, unknown> {
    let current = target as Record<string, unknown>;
    for (const part of path === '' ? [] : path.split('.')) {
      if (current[part] === undefined) current[part] = {};
      const next = current[part];
      if (typeof next !== 'object' || next === null || Array.isArray(next)) {
        throw new Error(`Invalid compiler field path ${path}.`);
      }
      current = next as Record<string, unknown>;
    }
    return current;
  }
}
