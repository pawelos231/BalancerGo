import { readFileSync } from 'node:fs';
import { registerHooks } from 'node:module';

// Match Vite's raw imports for scenario sources and authored documentation.
registerHooks({
  load(url, context, nextLoad) {
    if (/\.(lb|md)\?raw$/.test(url)) {
      const source = readFileSync(new URL(url), 'utf8');
      return { format: 'module', source: `export default ${JSON.stringify(source)};`, shortCircuit: true };
    }
    return nextLoad(url, context);
  },
});
