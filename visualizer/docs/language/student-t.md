# Student t distributions

## Summary

A symmetric density with heavier tails than a normal curve. A location, a positive scale, and positive degrees of freedom define the underlying distribution.

Named declarations are stored definitions. They do not change CPU, links, traffic, or other scalar runtime settings. The mathematical preview does not draw random values.

## On this page

[Syntax](#syntax) · [Parameters and units](#parameters-and-units) · [Support and truncation](#support-and-truncation) · [Density preview](#density-preview) · [Numeric admission](#numeric-admission) · [Examples](#examples) · [Errors](#errors) · [References](#references)

## Syntax

```lb-fragment context=top-level
distribution heavy-tail-time student-t {
    location 10ms
    scale 2ms
    degrees-of-freedom 3
    min 0ms
    max 30ms
}
```

Place the declaration before timeline `at` blocks. Names are unique among distributions. Source family `student-t` is stored as JSON kind `studentT` in `fixed.randomProfiles`. Required source parameter names are case-sensitive; JSON uses camelCase for dashed names.

## Parameters and units

| Parameter | Meaning | Rule |
| --- | --- | --- |
| `location` | Underlying center of symmetry | Nonnegative quantity |
| `scale` | Quantity scale | Strictly positive quantity |
| `degrees-of-freedom` | Dimensionless tail parameter | Strictly positive plain number |
| `min, max` | Retained interval | 0 <= min < max; compatible quantity units |

Degrees of freedom, denoted nu, is dimensionless and may be fractional. Scale is not the standard deviation: for nu > 2 the untruncated standard deviation is scale * sqrt(nu / (nu - 2)). Smaller degrees of freedom remain valid even when the untruncated mean or variance does not exist.

Quantity parameters must share one dimension: time (`ns`, `us`, `ms`, `s`, `m`), bytes (`B`, `KB`, `MB`, `GB`, `TB`, `PB`, `EB`), percentages (`%`), or plain numbers. Different suffixes in one dimension are compatible, such as `500us` and `1ms`. JSON stores exact decimal strings in base units (`nanoseconds`, `bytes`, `percentage`, or `number`). Degrees of freedom uses plain numbers and does not inherit quantity units.

Each required parameter appears exactly once. Unknown and repeated parameters are errors. All quantity values are nonnegative, even for families whose unrestricted mathematical distribution extends below zero.

## Support and truncation

The mathematical support is **[min, max)**. Values below min or at/above max are excluded. The retained density is the underlying density divided by its probability mass in that interval; truncation conditions the distribution on that interval. An interval must have positive probability mass.

Density plots describe probability per horizontal-axis unit; interval probabilities are areas. The curve is not a sampled histogram or a prediction of simulation behavior.

## Density preview

Choose a definition from the library’s **Student t profile definitions** example: **heavy-tail-time**, **cauchy-time**.

The density is conditional on [min, max): the underlying density is divided by the probability mass retained within those bounds. Its analytical area is 1.

The horizontal axis shows the complete configured support, with exact base-unit labels and padding where appropriate. The caption identifies the vertical-axis units and endpoint conventions.

This is a deterministic analytical approximation of the definition before integer flooring or runtime conversion. It draws no random values and predicts no future packets. If an axis or numerical calculation cannot be resolved reliably, the preview explains the limit instead of drawing a substitute curve.

## Numeric admission

Compilation and JSON import preserve exact nonnegative decimal strings, including fractions of a base unit. Every quantity and dimensionless parameter is at most 9,223,372,036,854,775,807; percentage quantities are at most 100. The JSON canonical decimal representation has at most 128 characters and no redundant leading zeros or trailing fractional zeros. Degrees of freedom does not inherit the percentage quantity ceiling.

Exact profile validation and sampler preparation are separate checks. A valid decimal profile can still be rejected by a standalone sampler if floating-point transformations underflow, its interval has no representable floating-point value, or its retained probability mass cannot be resolved numerically. The sampler reports unavailable numerical preparation instead of substituting another distribution. Scalar runtime fields keep their existing whole-base-unit rules.

## Examples

The scenario library uses these canonical sources. Each source has a complete scalar topology and loads as an editable draft without starting a run.

| Source | Purpose |
| --- | --- |
| [Student t profile definitions](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/student-t-profile-definitions.lb) | Multiple valid named presets; every definition remains unbound |
| [Student t invalid parameters](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/student-t-invalid-parameters.lb) | Degrees of freedom must be a positive dimensionless value. |
| [Student t missing parameter](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/student-t-missing-parameter.lb) | Required `degrees-of-freedom` is omitted |

## Errors

Degrees of freedom must be a positive dimensionless value. Supply all parameters in the table, keep units compatible, and check the support relation before compiling. The invalid and missing-parameter sources above retain compiler source locations so the editor can point to the declaration or offending value. Inline or named runtime binding remains unavailable; keep scalar runtime settings.

## References

[Gonum: StudentsT parameterization](https://pkg.go.dev/gonum.org/v1/gonum/stat/distuv#StudentsT) describes the underlying mathematical parameterization. The source syntax, exact decimal rules, finite support, and unbound runtime status on this page are this project's contract.
