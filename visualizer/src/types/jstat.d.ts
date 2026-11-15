// The upstream 1.9.6 package does not ship TypeScript declarations.
// Keep this private declaration limited to the deterministic functions we use.
declare module 'jstat' {
  interface Law {
    pdf(value: number, first: number, second?: number, third?: number): number;
    cdf(value: number, first: number, second?: number, third?: number): number;
  }
  const jstat: {
    jStat: {
      normal: Law; exponential: Law; gamma: Law; studentt: Law; beta: Law;
      lognormal: Law; pareto: Law; weibull: Law; triangular: Law;
      gammaln(value: number): number;
    };
  };
  export default jstat;
}
