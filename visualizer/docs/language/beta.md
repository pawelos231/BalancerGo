# Beta distributions

## Summary

A beta density affinely mapped from the unit interval to the declared quantity range. Its two dimensionless shapes control concentration near either endpoint.

Named declarations are stored definitions. They do not change CPU, links, traffic, or other scalar runtime settings. The mathematical preview does not draw random values.

## On this page

[Syntax](#syntax) · [Parameters and units](#parameters-and-units) · [Support and truncation](#support-and-truncation) · [Density preview](#density-preview) · [Numeric admission](#numeric-admission) · [Examples](#examples) · [Errors](#errors) · [References](#references)

## Syntax

```lb-fragment context=top-level
distribution early-time beta {
    alpha 2
    beta 5
    min 1ms
    max 5ms
}
```

Place the declaration before timeline `at` blocks. Names are unique among distributions. Source family `beta` is stored as JSON kind `beta` in `fixed.randomProfiles`. Required source parameter names are case-sensitive.

## Parameters and units

| Parameter | Meaning | Rule |
| --- | --- | --- |
| `alpha` | First dimensionless shape | Strictly positive plain number |
| `beta` | Second dimensionless shape | Strictly positive plain number |
| `min, max` | Affine quantity range | 0 <= min < max; compatible quantity units |

If Y has a beta(alpha, beta) distribution on [0, 1], the declared quantity is X = min + (max - min) * Y. Here min and max define the affine support; they do not truncate an already scaled beta distribution. Alpha below 1 creates an integrable singularity at min; beta below 1 creates one at max.

Quantity parameters must share one dimension: time (`ns`, `us`, `ms`, `s`, `m`), bytes (`B`, `KB`, `MB`, `GB`, `TB`, `PB`, `EB`), percentages (`%`), or plain numbers. Different suffixes in one dimension are compatible, such as `500us` and `1ms`. JSON stores exact decimal strings in base units (`nanoseconds`, `bytes`, `percentage`, or `number`). Dimensionless shape parameters use plain numbers and do not inherit quantity units.

Each required parameter appears exactly once. Unknown and repeated parameters are errors. All quantity values are nonnegative.

## Support and truncation

The declared range defines the mathematical support **[min, max)**. The upper endpoint is excluded by the sampler contract; continuous endpoints have zero probability. The support must have positive width. A finite plot cannot depict an infinite endpoint density height; endpoint singularities are integrable and are not point masses.

Density plots describe probability per horizontal-axis unit; interval probabilities are areas. The curve is not a sampled histogram or a prediction of simulation behavior.

## Density preview

Choose a definition from the library’s **Beta profile definitions** example: **early-time**, **edge-time**.

The density maps the standard Beta law affinely onto [min, max), including the reciprocal-width density factor. Its analytical area is 1.

The horizontal axis shows the complete configured support, with exact base-unit labels and padding where appropriate. The caption identifies the vertical-axis units and endpoint conventions.

When density diverges at an endpoint, an infinity arrow marks the singularity. The caption gives the exact finite-curve boundary and the omitted endpoint interval; no infinite height is replaced by a finite spike.

This is a deterministic analytical approximation of the definition before integer flooring or runtime conversion. It draws no random values and predicts no future packets. If an axis or numerical calculation cannot be resolved reliably, the preview explains the limit instead of drawing a substitute curve.

## Numeric admission

Compilation and JSON import preserve exact nonnegative decimal strings, including fractions of a base unit. Every quantity and dimensionless parameter is at most 9,223,372,036,854,775,807; percentage quantities are at most 100. The JSON canonical decimal representation has at most 128 characters and no redundant leading zeros or trailing fractional zeros. Dimensionless shape parameters do not inherit the percentage quantity ceiling.

Exact profile validation and sampler preparation are separate checks. A valid decimal profile can still be rejected by a standalone sampler if floating-point transformations underflow, its interval has no representable floating-point value, or its quantiles cannot be resolved numerically. The sampler reports unavailable numerical preparation instead of substituting another distribution. Scalar runtime fields keep their existing whole-base-unit rules.

## Examples

The scenario library uses these canonical sources. Each source has a complete scalar topology and loads as an editable draft without starting a run.

| Source | Purpose |
| --- | --- |
| [Beta profile definitions](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/beta-profile-definitions.lb) | Multiple valid named presets; every definition remains unbound |
| [Beta invalid parameters](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/beta-invalid-parameters.lb) | Both beta shape parameters must be positive. |
| [Beta missing parameter](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/beta-missing-parameter.lb) | Required `beta` is omitted |

## Errors

Both beta shape parameters must be positive. Supply all parameters in the table, keep units compatible, and check the support relation before compiling. The invalid and missing-parameter sources above retain compiler source locations so the editor can point to the declaration or offending value. Inline or named runtime binding remains unavailable; keep scalar runtime settings.

## References

[Gonum: Beta distribution](https://pkg.go.dev/gonum.org/v1/gonum/stat/distuv#Beta) describes the underlying mathematical parameterization. The source syntax, exact decimal rules, finite support, and unbound runtime status on this page are this project's contract.
