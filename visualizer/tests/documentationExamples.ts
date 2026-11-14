import type { Nodes } from 'mdast';
import { toString } from 'mdast-util-to-string';
import remarkGfm from 'remark-gfm';
import remarkParse from 'remark-parse';
import { unified } from 'unified';
import { languagePages } from '../src/docs/language.ts';
import type { LanguagePageId } from '../src/docs/language.ts';

export type DocumentationExampleContext = 'tcp' | 'balancer' | 'backend' | 'clients' | 'top-level' | 'timeline';
export type DocumentationExampleLanguage = 'lb' | 'lb-fragment' | 'lb-invalid';

export interface DocumentationExample {
  name: string;
  page: LanguagePageId;
  path: string;
  line: number;
  code: string;
  language: DocumentationExampleLanguage;
  context?: DocumentationExampleContext;
  expectedError?: string;
}

const contexts: readonly string[] = ['tcp', 'balancer', 'backend', 'clients', 'top-level', 'timeline'];
const parser = unified().use(remarkParse).use(remarkGfm);

function metadata(source: string, name: string) {
  const values = new Map<string, string>();
  const field = /\b(context|error)=("(?:\\.|[^"\\])*"|[^\s"]+)/g;
  for (const match of source.matchAll(field)) {
    if (values.has(match[1])) throw new Error(`${name}: duplicate ${match[1]} metadata`);
    values.set(match[1], match[2].startsWith('"') ? JSON.parse(match[2]) as string : match[2]);
  }
  if (source.replace(field, '').trim()) throw new Error(`${name}: invalid example metadata ${source}`);
  const context = values.get('context');
  if (context !== undefined && !contexts.includes(context)) {
    throw new Error(`${name}: unknown example context ${context}`);
  }
  return {
    context: context as DocumentationExampleContext | undefined,
    expectedError: values.get('error'),
  };
}

// Parse the same authored sources used by the reader; headings inside fences are never prose.
export function documentationExamples(): DocumentationExample[] {
  return languagePages.flatMap(page => {
    const examples: DocumentationExample[] = [];
    let heading: string = page.title;
    function visit(node: Nodes) {
      if (node.type === 'heading') heading = toString(node);
      if (node.type === 'code') {
        const line = node.position?.start.line ?? 1;
        const name = `${page.id}: ${heading} (line ${line})`;
        if (!['lb', 'lb-fragment', 'lb-invalid'].includes(node.lang ?? '')) {
          throw new Error(`${name}: code examples must declare lb, lb-fragment, or lb-invalid`);
        }
        const language = node.lang as DocumentationExampleLanguage;
        const fields = metadata(node.meta ?? '', name);
        if (language === 'lb-invalid' && !fields.expectedError) {
          throw new Error(`${name}: invalid examples require error="expected compiler text"`);
        }
        if (language !== 'lb-invalid' && fields.expectedError !== undefined) {
          throw new Error(`${name}: valid examples cannot declare an expected error`);
        }
        examples.push({ name, page: page.id, path: page.path, line, code: node.value, language, ...fields });
      }
      if ('children' in node) node.children.forEach(visit);
    }
    visit(parser.parse(page.markdown));
    return examples;
  });
}

// A replacement context changes one declaration, so a fragment cannot gain duplicate resources.
export function wrapDocumentationExample(code: string, context?: DocumentationExampleContext): string {
  if (context === undefined) return code;
  const declarations: Record<string, string> = {
    run: 'run 30s {\n    speed 0\n}',
    balancer: 'balancer lb {\n}',
    backend: 'backend api-0 {\n}',
    clients: 'clients users {\n    count 3\n    payload 10KB\n}',
  };
  if (context === 'top-level' || context === 'timeline') {
    declarations.fragment = code;
  } else {
    const header = {
      tcp: 'tcp', balancer: 'balancer lb', backend: 'backend api-0', clients: 'clients users',
    }[context];
    declarations[context] = `${header} {\n${code.split('\n').map(line => `    ${line}`).join('\n')}\n}`;
  }
  return `${Object.values(declarations).join('\n\n')}\n`;
}
