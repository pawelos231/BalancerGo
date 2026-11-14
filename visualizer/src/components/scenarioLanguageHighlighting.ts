import { StreamLanguage } from '@codemirror/language';

const namedDeclarations = new Set(['balancer', 'backend', 'backends', 'clients', 'client', 'profile']);
const commands = new Set(['add', 'set', 'down', 'up', 'drain', 'stop', 'remove']);
const keywords = new Set([
  'language', 'run', 'defaults', 'tcp', ...namedDeclarations, ...commands,
  'use', 'at', 'arrivals',
  'seed', 'speed', 'snapshots', 'max-events', 'link', 'travel', 'queue', 'loss',
  'queue-policy', 'ecn-threshold', 'signal-probability', 'to-lb', 'from-lb',
  'cores', 'packet-time', 'host', 'mapping-timeouts', 'half-open', 'established',
  'closing', 'reset', 'payload', 'packets', 'segment-gap', 'count', 'start', 'every',
  'from', 'mss', 'congestion', 'receive-buffer', 'SACK', 'ECN', 'client-idle',
  'backend-half-open-idle', 'backend-established-idle', 'congestion-control',
  'mode', 'fast-retransmit', 'fast-convergence', 'initial-window', 'maximum-window',
  'ssthresh', 'rto', 'backoff', 'initial', 'minimum', 'maximum', 'close', 'msl',
  'fin-wait', 'fin-retries', 'delayed-ack', 'enabled', 'segments', 'max-delay',
  'algo', 'ingress', 'egress', 'name', 'address', 'available', 'admin',
]);

type Role = 'statement' | 'field' | 'value' | 'name' | 'target';
type Block = 'fields' | 'statements';

interface HighlightState {
  role: Role;
  blocks: Block[];
  nextBlock: Block;
  command: string | null;
  afterName: 'field' | 'statement';
}

// Track token positions for color only; the compiler remains the semantic validator.
export const scenarioLanguage = StreamLanguage.define<HighlightState>({
  startState: () => ({
    role: 'statement', blocks: [], nextBlock: 'fields', command: null, afterName: 'field',
  }),
  copyState: state => ({ ...state, blocks: [...state.blocks] }),
  token(stream, state) {
    if (stream.sol()) {
      state.role = state.blocks[state.blocks.length - 1] === 'fields' ? 'field' : 'statement';
      state.nextBlock = 'fields';
      state.command = null;
    }
    if (stream.eatSpace()) return null;
    if (stream.eat('#')) {
      stream.skipToEnd();
      return 'comment';
    }
    if (stream.eat('{')) {
      state.blocks.push(state.nextBlock);
      state.role = state.nextBlock === 'fields' ? 'field' : 'statement';
      state.nextBlock = 'fields';
      return 'punctuation';
    }
    if (stream.eat('}')) {
      state.blocks.pop();
      state.role = state.blocks[state.blocks.length - 1] === 'fields' ? 'field' : 'statement';
      state.nextBlock = 'fields';
      return 'punctuation';
    }
    if (stream.eat('"')) {
      let escaped = false;
      while (!stream.eol()) {
        const character = stream.next();
        if (character === '"' && !escaped) break;
        escaped = character === '\\' && !escaped;
      }
      state.role = state.role === 'name' ? state.afterName : 'field';
      return 'string';
    }
    if (stream.match(/^-?[0-9]+(?:\.[0-9]+)?(?:[A-Za-z]+|%)?/)) {
      state.role = state.role === 'name' ? state.afterName : 'field';
      return 'number';
    }
    if (stream.match(/^[A-Za-z_][A-Za-z0-9_-]*/)) {
      const word = stream.current();
      if (state.role === 'name') {
        state.role = state.afterName;
        return 'variableName';
      }
      if (state.role === 'value') {
        state.role = 'field';
        if (word === 'true' || word === 'false') return 'bool';
        return word === 'null' ? 'null' : 'variableName';
      }
      if (state.role === 'target') {
        state.role = state.command === 'set' && word === 'balancer' ? 'field' : 'name';
        return keywords.has(word) ? 'keyword' : 'variableName';
      }
      if (state.role === 'field') {
        state.role = 'value';
      } else if (namedDeclarations.has(word)) {
        state.role = 'name';
        state.afterName = 'field';
      } else if (word === 'run' || word === 'language' || word === 'at') {
        state.role = 'value';
        if (word === 'at') state.nextBlock = 'statements';
      } else if (commands.has(word)) {
        state.role = 'target';
        state.command = word;
        state.afterName = word === 'add' || word === 'set' ? 'field' : 'statement';
      } else if (word === 'defaults' || word === 'tcp') {
        state.role = 'field';
      }
      return keywords.has(word) ? 'keyword' : 'variableName';
    }
    stream.next();
    return 'punctuation';
  },
});
