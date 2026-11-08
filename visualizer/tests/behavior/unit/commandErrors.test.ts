import assert from 'node:assert/strict';
import test from 'node:test';
import { CommandType } from '../../../src/commands.ts';
import { readCommandError } from '../../../src/hooks/commandErrors.ts';

test('legacy errors and private Start rejections use one readable command error', () => {
  const expected = { commandType: CommandType.StartSimulation, error: 'invalid configuration: speed' };
  assert.deepEqual(readCommandError({ type: 'commandError', ...expected }), expected);
  const rejection = {
    type: 'commandResult', ...expected, runId: 'run-b', commandId: '', status: 'rejected',
  };
  assert.deepEqual(readCommandError(rejection), expected);
  for (const invalid of [
    null,
    { ...rejection, status: 'applied' },
    { ...rejection, commandType: CommandType.StopSimulation },
    { ...rejection, runId: '' },
    { ...rejection, commandId: undefined },
    { ...rejection, error: undefined },
  ]) assert.equal(readCommandError(invalid), null);
});
