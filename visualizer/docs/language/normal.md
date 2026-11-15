# Normal distributions

## Summary

A bell-shaped distribution centered on mean, with spread controlled by stddev. The declared bounds retain and normalize only the selected interval.

Named declarations are stored definitions. They do not change CPU, links, traffic, or other scalar runtime settings. The mathematical preview does not draw random values.

## On this page

[Syntax](#syntax) · [Parameters and units](#parameters-and-units) · [Support and truncation](#support-and-truncation) · [Density preview](#density-preview) · [Numeric admission](#numeric-admission) · [Examples](#examples) · [Errors](#errors) · [References](#references)

## Syntax

```lb-fragment context=top-level
distribution service-time normal {
    mean 10us
    stddev 2us
    min 0us
    max 20us
}
```

Place the declaration before timeline `at` blocks. Names are unique among distributions. Source family `normal` is stored as JSON kind `normal` in `fixed.randomProfiles`. Required source parameter names are case-sensitive; JSON uses camelCase for dashed names.

## Parameters and units

| Parameter | Meaning | Rule |
| --- | --- | --- |
| `mean` | Center of the underlying normal distribution | Nonnegative quantity |
| `stddev` | Underlying standard deviation | Strictly positive quantity |
| `min, max` | Retained interval | 0 <= min < max; compatible quantity units |

Before truncation, mean is the center and stddev is the standard deviation. Truncation can change the resulting mean and standard deviation.

Quantity parameters must share one dimension: time (`ns`, `us`, `ms`, `s`, `m`), bytes (`B`, `KB`, `MB`, `GB`, `TB`, `PB`, `EB`), percentages (`%`), or plain numbers. Different suffixes in one dimension are compatible, such as `500us` and `1ms`. JSON stores exact decimal strings in base units (`nanoseconds`, `bytes`, `percentage`, or `number`). Dimensionless shapes, probabilities, and weights use plain numbers and do not inherit quantity units.

Each required parameter appears exactly once. Unknown and repeated parameters are errors. All quantity values are nonnegative, even for families whose unrestricted mathematical distribution extends below zero.

## Support and truncation

The mathematical support is **[min, max)**. Values below min or at/above max are excluded. The retained density is the underlying density divided by its probability mass in that interval; truncation conditions the distribution on that interval. An interval must have positive probability mass.

Density plots describe probability per horizontal-axis unit; interval probabilities are areas. Discrete plots describe probability at an atom. Neither view is a sampled histogram or a prediction of simulation behavior.

## Density preview

Choose a definition from the library’s **Normal profile definitions** example.
**service-time** shows the centered interval from 0us to 20us;
**upper-tail** retains 12us to 16us from the same underlying curve.

The horizontal axis shows the complete configured interval **[min, max)** in
base units. The vertical axis is probability density per base unit. The filled
minimum marker includes min; the open maximum marker shows the excluded
endpoint’s left-hand limit. The curve is normalized by the probability mass
retained within the bounds, so its analytical area is 1.

The diagram is a deterministic numerical approximation of the continuous
definition before integer flooring or floating-point sampler conversion.
It uses no random values and predicts no future packets. If the exact bounds
or retained mass cannot be resolved reliably, the preview explains the limit
instead of drawing a substitute curve.

## Numeric admission

Compilation and JSON import preserve exact nonnegative decimal strings, including fractions of a base unit. Every quantity and dimensionless parameter is at most 9,223,372,036,854,775,807; percentage quantities are at most 100. The JSON canonical decimal representation has at most 128 characters and no redundant leading zeros or trailing fractional zeros. Shapes and weights are dimensionless, so the percentage quantity ceiling does not apply to them.

Exact profile validation and sampler preparation are separate checks. A valid decimal profile can still be rejected by a standalone sampler if floating-point transformations underflow, its interval has no representable floating-point value, or its retained probability mass cannot be resolved numerically. The sampler reports unavailable numerical preparation instead of substituting another distribution. Scalar runtime fields keep their existing whole-base-unit rules.

## Examples

The scenario library uses these canonical sources. Each source has a complete scalar topology and loads as an editable draft without starting a run.

| Source | Purpose |
| --- | --- |
| [Normal profile definitions](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/normal-profile-definitions.lb) | Multiple valid named presets; every definition remains unbound |
| [Normal invalid parameters](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/normal-invalid-parameters.lb) | A zero standard deviation cannot define a normal density. |
| [Normal missing parameter](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/normal-missing-parameter.lb) | Required `stddev` is omitted |

## Errors

A zero standard deviation cannot define a normal density. Supply all parameters in the table, keep units compatible, and check the support relation before compiling. The invalid and missing-parameter sources above retain compiler source locations so the editor can point to the declaration or offending value. Inline or named runtime binding remains unavailable; keep scalar runtime settings.

## References

[NIST: Normal distribution](https://www.itl.nist.gov/div898/handbook/eda/section3/eda3661.htm) describes the underlying mathematical parameterization. The source syntax, exact decimal rules, finite support, and unbound runtime status on this page are this project's contract.
