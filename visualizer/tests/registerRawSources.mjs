import { readFileSync } from 'node:fs';
import { registerHooks } from 'node:module';

// Match Vite's raw-text imports so tests read the same canonical .lb files as the app.
registerHooks({
  load(url, context, nextLoad) {
    if (url.endsWith('.lb?raw')) {
      const source = readFileSync(new URL(url), 'utf8');
      return { format: 'module', source: `export default ${JSON.stringify(source)};`, shortCircuit: true };
    }
    return nextLoad(url, context);
  },
});
