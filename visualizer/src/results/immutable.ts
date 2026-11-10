// Collection entries and assembled artifacts own their trees, including nested values.
export function freezeTree<T>(value: T): T {
  if (value !== null && typeof value === 'object') {
    Object.values(value).forEach(freezeTree);
    Object.freeze(value);
  }
  return value;
}
