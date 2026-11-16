# Triangular distributions

## Summary

A continuous density rising linearly to a mode and falling linearly afterward. The declared minimum and maximum are its support, and the mode may equal either endpoint.

Named declarations are stored definitions. They do not change CPU, links, traffic, or other scalar runtime settings. The mathematical preview does not draw random values.

## On this page

[Syntax](#syntax) · [Parameters and units](#parameters-and-units) · [Support and truncation](#support-and-truncation) · [Density preview](#density-preview) · [Numeric admission](#numeric-admission) · [Examples](#examples) · [Errors](#errors) · [References](#references)

## Syntax

```lb-fragment context=top-level
distribution symmetric-time triangular {
    min 1ms
    mode 3ms
    max 5ms
}
```

Place the declaration before timeline `at` blocks. Names are unique among distributions. Source family `triangular` is stored as JSON kind `triangular` in `fixed.randomProfiles`. Required source parameter names are case-sensitive.

## Parameters and units

| Parameter | Meaning | Rule |
| --- | --- | --- |
| `min` | Lower support bound | Nonnegative quantity |
| `mode` | Location of the density peak | min <= mode <= max |
| `max` | Upper support bound | Strictly greater than min; compatible quantity units |

The density's peak lies at mode. For an interior mode, its height is 2 / (max - min); the area under both straight segments is 1. An endpoint mode gives a single descending or ascending segment. No extra truncation is applied beyond this support.

Quantity parameters must share one dimension: time (`ns`, `us`, `ms`, `s`, `m`), bytes (`B`, `KB`, `MB`, `GB`, `TB`, `PB`, `EB`), percentages (`%`), or plain numbers. Different suffixes in one dimension are compatible, such as `500us` and `1ms`. JSON stores exact decimal strings in base units (`nanoseconds`, `bytes`, `percentage`, or `number`).

Each required parameter appears exactly once. Unknown and repeated parameters are errors. All quantity values must be nonnegative.

## Support and truncation

The declared range defines the mathematical support **[min, max)**. The upper endpoint is excluded by the sampler contract; continuous endpoints have zero probability. The support must have positive width.

Density describes probability per horizontal-axis unit; interval probabilities are areas. The analytical preview is not a sampled histogram or a prediction of simulation behavior.

## Density preview

Choose a definition from the library’s **Triangular profile definitions** example: **symmetric-time**, **descending-time**.

The density shows the complete triangular support [min, max), including the configured mode. Its analytical area is 1.

The horizontal axis shows the whole configured range in readable units such as µs or ms, with padding where needed. Captions and table values keep exact decimal precision; only numerical ticks are rounded. The vertical-axis label names its units.

This is a deterministic analytical approximation of the definition before integer flooring or runtime conversion. It draws no random values and predicts no future packets. If an axis or numerical calculation cannot be resolved reliably, the preview explains the limit instead of drawing a substitute curve.

## Numeric admission

Compilation and JSON import preserve exact nonnegative decimal strings, including fractions of a base unit. Every quantity is at most 9,223,372,036,854,775,807; percentage quantities are at most 100. The JSON canonical decimal representation has at most 128 characters and no redundant leading zeros or trailing fractional zeros.

Exact profile validation and sampler preparation are separate checks. A valid decimal profile can still be rejected by a standalone sampler if its interval has no representable floating-point value or its inverse cannot be resolved numerically. The sampler reports unavailable numerical preparation instead of substituting another distribution. Scalar runtime fields keep their existing whole-base-unit rules.

## Examples

The scenario library uses these canonical sources. Each source has a complete scalar topology and loads as an editable draft without starting a run.

| Source | Purpose |
| --- | --- |
| [Triangular profile definitions](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/triangular-profile-definitions.lb) | Multiple valid named presets; every definition remains unbound |
| [Triangular invalid parameters](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/triangular-invalid-parameters.lb) | The triangular mode cannot lie outside its support. |
| [Triangular missing parameter](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/triangular-missing-parameter.lb) | Required `mode` is omitted |

## Errors

The triangular mode cannot lie outside its support. Supply all parameters in the table, keep units compatible, and check the support relation before compiling. The invalid and missing-parameter sources above retain compiler source locations so the editor can point to the declaration or offending value. Inline or named runtime binding remains unavailable; keep scalar runtime settings.

## References

[Gonum: Triangular distribution](https://pkg.go.dev/gonum.org/v1/gonum/stat/distuv#Triangle) describes the underlying mathematical parameterization. The source syntax, exact decimal rules, finite support, and unbound runtime status on this page are this project's contract.
