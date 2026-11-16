# Pareto distributions

## Summary

A power-law tail starting at a positive scale. Bounds retain a finite part of that tail, and min must be at least scale.

Named declarations are stored definitions. They do not change CPU, links, traffic, or other scalar runtime settings. The mathematical preview does not draw random values.

## On this page

[Syntax](#syntax) · [Parameters and units](#parameters-and-units) · [Support and truncation](#support-and-truncation) · [Density preview](#density-preview) · [Numeric admission](#numeric-admission) · [Examples](#examples) · [Errors](#errors) · [References](#references)

## Syntax

```lb-fragment context=top-level
distribution tail-time pareto {
    scale 1ms
    shape 2
    min 1ms
    max 10ms
}
```

Place the declaration before timeline `at` blocks. Names are unique among distributions. Source family `pareto` is stored as JSON kind `pareto` in `fixed.randomProfiles`. Required source parameter names are case-sensitive.

## Parameters and units

| Parameter | Meaning | Rule |
| --- | --- | --- |
| `scale` | Lower support threshold of the underlying distribution | Strictly positive quantity |
| `shape` | Dimensionless tail exponent | Strictly positive plain number |
| `min, max` | Retained interval | scale <= min < max; compatible quantity units |

The underlying density is shape * scale^shape / x^(shape + 1) for x >= scale. Shape is dimensionless. A larger shape makes the tail decay faster. The finite declared interval has finite moments even when an unbounded Pareto mean or variance would not exist.

After conditioning on fixed bounds with min >= scale, scale cancels from the
normalized density. Changing only scale below the same min will therefore
leave the retained curve unchanged; change min to move the start of its support.

Quantity parameters must share one dimension: time (`ns`, `us`, `ms`, `s`, `m`), bytes (`B`, `KB`, `MB`, `GB`, `TB`, `PB`, `EB`), percentages (`%`), or plain numbers. Different suffixes in one dimension are compatible, such as `500us` and `1ms`. JSON stores exact decimal strings in base units (`nanoseconds`, `bytes`, `percentage`, or `number`). `shape` is a dimensionless plain number and does not inherit quantity units.

Each required parameter appears exactly once. Unknown and repeated parameters are errors. All quantity values must be nonnegative.

## Support and truncation

The mathematical support is **[min, max)**. Values below min or at/above max are excluded. The retained density is the underlying density divided by its probability mass in that interval; truncation conditions the distribution on that interval. An interval must have positive probability mass.

Density describes probability per horizontal-axis unit; interval probabilities are areas. The analytical preview is not a sampled histogram or a prediction of simulation behavior.

## Density preview

Choose a definition from the library’s **Pareto profile definitions** example: **tail-time**, **later-tail-time**.

The density is conditional on [min, max): the underlying density is divided by the probability mass retained within those bounds. Its analytical area is 1.

The horizontal axis shows the complete configured support, with exact base-unit labels and padding where appropriate. The caption identifies the vertical-axis units and endpoint conventions.

This is a deterministic analytical approximation of the definition before integer flooring or runtime conversion. It draws no random values and predicts no future packets. If an axis or numerical calculation cannot be resolved reliably, the preview explains the limit instead of drawing a substitute curve.

## Numeric admission

Compilation and JSON import preserve exact nonnegative decimal strings, including fractions of a base unit. Every quantity and dimensionless parameter is at most 9,223,372,036,854,775,807; percentage quantities are at most 100. The JSON canonical decimal representation has at most 128 characters and no redundant leading zeros or trailing fractional zeros. The percentage ceiling does not apply to dimensionless parameters.

Exact profile validation and sampler preparation are separate checks. A valid decimal profile can still be rejected by a standalone sampler if floating-point transformations underflow, its interval has no representable floating-point value, or its retained probability mass cannot be resolved numerically. The sampler reports unavailable numerical preparation instead of substituting another distribution. Scalar runtime fields keep their existing whole-base-unit rules.

## Examples

The scenario library uses these canonical sources. Each source has a complete scalar topology and loads as an editable draft without starting a run.

| Source | Purpose |
| --- | --- |
| [Pareto profile definitions](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/pareto-profile-definitions.lb) | Multiple valid named presets; every definition remains unbound |
| [Pareto invalid parameters](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/pareto-invalid-parameters.lb) | A Pareto minimum cannot lie below the scale threshold. |
| [Pareto missing parameter](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/pareto-missing-parameter.lb) | Required `scale` is omitted |

## Errors

A Pareto minimum cannot lie below the scale threshold. Supply all parameters in the table, keep units compatible, and check the support relation before compiling. The invalid and missing-parameter sources above retain compiler source locations so the editor can point to the declaration or offending value. Inline or named runtime binding remains unavailable; keep scalar runtime settings.

## References

[Gonum: Pareto distribution](https://pkg.go.dev/gonum.org/v1/gonum/stat/distuv#Pareto) describes the underlying mathematical parameterization. The source syntax, exact decimal rules, finite support, and unbound runtime status on this page are this project's contract.
