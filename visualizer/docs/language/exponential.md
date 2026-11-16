# Exponential distributions

## Summary

A decreasing density for nonnegative values, parameterized by its underlying mean. Bounds select a finite interval of that density.

Named declarations are stored definitions. They do not change CPU, links, traffic, or other scalar runtime settings. The mathematical preview does not draw random values.

## On this page

[Syntax](#syntax) · [Parameters and units](#parameters-and-units) · [Support and truncation](#support-and-truncation) · [Density preview](#density-preview) · [Numeric admission](#numeric-admission) · [Examples](#examples) · [Errors](#errors) · [References](#references)

## Syntax

```lb-fragment context=top-level
distribution short-delays exponential {
    mean 5ms
    min 0ms
    max 20ms
}
```

Place the declaration before timeline `at` blocks. Names are unique among distributions. Source family `exponential` is stored as JSON kind `exponential` in `fixed.randomProfiles`. Required source parameter names are case-sensitive.

## Parameters and units

| Parameter | Meaning | Rule |
| --- | --- | --- |
| `mean` | Mean of the underlying exponential distribution | Strictly positive quantity |
| `min, max` | Retained interval | 0 <= min < max; compatible quantity units |

The source uses mean, not a rate. The underlying density is exp(-x / mean) / mean for x >= 0. Its mean after truncation usually differs from the parameter.

Quantity parameters must share one dimension: time (`ns`, `us`, `ms`, `s`, `m`), bytes (`B`, `KB`, `MB`, `GB`, `TB`, `PB`, `EB`), percentages (`%`), or plain numbers. Different suffixes in one dimension are compatible, such as `500us` and `1ms`. JSON stores exact decimal strings in base units (`nanoseconds`, `bytes`, `percentage`, or `number`).

Each required parameter appears exactly once. Unknown and repeated parameters are errors. All quantity values are nonnegative.

## Support and truncation

The mathematical support is **[min, max)**. Values below min or at/above max are excluded. The retained density is the underlying density divided by its probability mass in that interval; truncation conditions the distribution on that interval. An interval must have positive probability mass.

Density plots describe probability per horizontal-axis unit; interval probabilities are areas. The curve is not a sampled histogram or a prediction of simulation behavior.

## Density preview

Choose a definition from the library’s **Exponential profile definitions** example: **short-delays**, **tail-delays**.

The density is conditional on [min, max): the underlying density is divided by the probability mass retained within those bounds. Its analytical area is 1.

The horizontal axis shows the whole configured range in readable units such as µs or ms, with padding where needed. Captions and table values keep exact decimal precision; only numerical ticks are rounded. The vertical-axis label names its units.

This is a deterministic analytical approximation of the definition before integer flooring or runtime conversion. It draws no random values and predicts no future packets. If an axis or numerical calculation cannot be resolved reliably, the preview explains the limit instead of drawing a substitute curve.

## Numeric admission

Compilation and JSON import preserve exact nonnegative decimal strings, including fractions of a base unit. Every quantity is at most 9,223,372,036,854,775,807; percentage quantities are at most 100. The JSON canonical decimal representation has at most 128 characters and no redundant leading zeros or trailing fractional zeros.

Exact profile validation and sampler preparation are separate checks. A valid decimal profile can still be rejected by a standalone sampler if floating-point transformations underflow, its interval has no representable floating-point value, or its retained probability mass cannot be resolved numerically. The sampler reports unavailable numerical preparation instead of substituting another distribution. Scalar runtime fields keep their existing whole-base-unit rules.

## Examples

The scenario library uses these canonical sources. Each source has a complete scalar topology and loads as an editable draft without starting a run.

| Source | Purpose |
| --- | --- |
| [Exponential profile definitions](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/exponential-profile-definitions.lb) | Multiple valid named presets; every definition remains unbound |
| [Exponential invalid parameters](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/exponential-invalid-parameters.lb) | The exponential mean must be positive. |
| [Exponential missing parameter](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/exponential-missing-parameter.lb) | Required `mean` is omitted |

## Errors

The exponential mean must be positive. Supply all parameters in the table, keep units compatible, and check the support relation before compiling. The invalid and missing-parameter sources above retain compiler source locations so the editor can point to the declaration or offending value. Inline or named runtime binding remains unavailable; keep scalar runtime settings.

## References

[NIST: Exponential distribution](https://www.itl.nist.gov/div898/handbook/eda/section3/eda3667.htm) describes the underlying mathematical parameterization. The source syntax, exact decimal rules, finite support, and unbound runtime status on this page are this project's contract.
