// Keep numeric tokens until the schema decides whether they are integers or floats.
// JSON.parse alone would round an unsafe integer and silently overwrite duplicate keys.
export class JSONNumber {
  readonly text: string;

  constructor(text: string) {
    this.text = text;
  }
}

// Read strict JSON while retaining number spelling and detecting decoded duplicate keys.
export function parseExactJSON(document: string): unknown {
  let position = 0;

  function fail(path: string, message: string): never {
    throw new Error(`${path}: ${message} (character ${position + 1})`);
  }

  function skipWhitespace(): void {
    while (' \t\r\n'.includes(document[position]) && position < document.length) position++;
  }

  function readString(path: string): string {
    const start = position++;
    while (position < document.length) {
      const character = document[position++];
      if (character === '\\') {
        position++;
      } else if (character === '"') {
        try {
          return replaceLoneSurrogates(JSON.parse(document.slice(start, position)) as string);
        } catch {
          return fail(path, 'invalid JSON string');
        }
      }
    }
    return fail(path, 'unterminated JSON string');
  }

  function readObject(path: string): Record<string, unknown> {
    position++;
    const result: Record<string, unknown> = Object.create(null);
    skipWhitespace();
    if (document[position] === '}') {
      position++;
      return result;
    }

    while (position < document.length) {
      if (document[position] !== '"') fail(path, 'expected an object key');
      const name = readString(path);
      const fieldPath = `${path}[${JSON.stringify(name)}]`;
      if (Object.prototype.hasOwnProperty.call(result, name)) fail(fieldPath, 'duplicate field');

      skipWhitespace();
      if (document[position++] !== ':') fail(fieldPath, 'expected a colon');
      result[name] = readValue(fieldPath);
      skipWhitespace();
      const delimiter = document[position++];
      if (delimiter === '}') return result;
      if (delimiter !== ',') fail(path, 'expected a comma or closing brace');
      skipWhitespace();
    }
    return fail(path, 'unterminated object');
  }

  function readArray(path: string): unknown[] {
    position++;
    const result: unknown[] = [];
    skipWhitespace();
    if (document[position] === ']') {
      position++;
      return result;
    }

    while (position < document.length) {
      result.push(readValue(`${path}[${result.length}]`));
      skipWhitespace();
      const delimiter = document[position++];
      if (delimiter === ']') return result;
      if (delimiter !== ',') fail(path, 'expected a comma or closing bracket');
      skipWhitespace();
    }
    return fail(path, 'unterminated array');
  }

  function readValue(path: string): unknown {
    skipWhitespace();
    const character = document[position];
    if (character === '"') return readString(path);
    if (character === '{') return readObject(path);
    if (character === '[') return readArray(path);

    for (const [token, value] of [['true', true], ['false', false], ['null', null]] as const) {
      if (document.startsWith(token, position)) {
        position += token.length;
        return value;
      }
    }

    const start = position;
    while (position < document.length && /[0-9eE+.-]/.test(document[position])) position++;
    const number = document.slice(start, position);
    const validNumber = /^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$/.test(number);
    if (!validNumber) return fail(path, 'expected a JSON value');
    return new JSONNumber(number);
  }

  const result = readValue('scenario');
  skipWhitespace();
  if (position !== document.length) fail('scenario', 'trailing JSON after the document');
  return result;
}

// Go's JSON reader replaces unmatched UTF-16 surrogates; preserve valid pairs such as emoji.
export function replaceLoneSurrogates(value: string): string {
  let result = '';
  for (let index = 0; index < value.length; index++) {
    const code = value.charCodeAt(index);
    const next = value.charCodeAt(index + 1);
    if (code >= 0xd800 && code <= 0xdbff && next >= 0xdc00 && next <= 0xdfff) {
      result += value.slice(index, index + 2);
      index++;
    } else {
      result += code >= 0xd800 && code <= 0xdfff ? '\ufffd' : value[index];
    }
  }
  return result;
}
