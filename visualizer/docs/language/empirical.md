# Empirical distributions

## Summary

A finite collection of observations, each carrying equal probability. Duplicate observations remain repeated and therefore receive proportionally greater combined mass.

Named declarations are stored definitions. They do not change CPU, links, traffic, or other scalar runtime settings. The mathematical preview does not draw random values.

## On this page

[Syntax](#syntax) · [Parameters and units](#parameters-and-units) · [Support and truncation](#support-and-truncation) · [Density preview](#density-preview) · [Numeric admission](#numeric-admission) · [Examples](#examples) · [Errors](#errors) · [References](#references)

## Syntax

```lb-fragment context=top-level
distribution observed-time empirical {
    observations {
        observation 1ms
        observation 1ms
        observation 3ms
    }
}
```

Place the declaration before timeline `at` blocks. Names are unique among distributions. Source family `empirical` is stored as JSON kind `empirical` in `fixed.randomProfiles`. Required source parameter names are case-sensitive.

## Parameters and units

| Parameter | Meaning | Rule |
| --- | --- | --- |
| `observations` | Ordered block of observation values | 1 to 4096 observations |
| `observation` | One equally weighted quantity observation | Nonnegative; all observations have compatible units |

Every listed observation has mass 1 / count. The observations 1ms, 1ms, and 3ms give a combined probability 2/3 at 1ms and 1/3 at 3ms. This distribution is discrete; it does not interpolate values between observations or fit a smooth density.

Quantity parameters must share one dimension: time (`ns`, `us`, `ms`, `s`, `m`), bytes (`B`, `KB`, `MB`, `GB`, `TB`, `PB`, `EB`), percentages (`%`), or plain numbers. Different suffixes in one dimension are compatible, such as `500us` and `1ms`. JSON stores exact decimal strings in base units (`nanoseconds`, `bytes`, `percentage`, or `number`).

Declare observations once and repeat observation values inside it. Repeated
values are intentional and retain their frequency. Unknown fields are errors;
every observation must be nonnegative.

## Support and truncation

This family is discrete. Its ECDF preview shows the fraction of observations
at or below each value. A repeated value produces a larger jump; the curve
stays constant between observations and reaches 1 at the largest observation.
No min/max truncation parameters are accepted.

The ECDF comes directly from the listed observations. It is not a sampled
histogram, a fitted smooth curve, or a prediction of simulation behavior.

## Density preview

Choose a definition from the library’s **Empirical profile definitions** example: **observed-time**, **single-time**.

The ECDF assigns equal mass 1/n to every observation. Duplicate values produce a larger jump. The curve stays constant between observations; open and filled markers distinguish the values before and after each jump. There is no interpolation.

The horizontal axis shows the whole configured range in readable units such as µs or ms, with padding where needed. Captions and table values keep exact decimal precision; only numerical ticks are rounded. The vertical-axis label names its units.

This is a deterministic analytical approximation of the definition before integer flooring or runtime conversion. It draws no random values and predicts no future packets. If an axis or numerical calculation cannot be resolved reliably, the preview explains the limit instead of drawing a substitute curve.

## Numeric admission

Compilation and JSON import preserve exact nonnegative decimal strings, including fractions of a base unit. Every observation is at most 9,223,372,036,854,775,807; percentage observations are at most 100. The JSON canonical decimal representation has at most 128 characters and no redundant leading zeros or trailing fractional zeros.

Exact profile validation and sampler preparation are separate checks. Standalone discrete samplers preserve exact rational selection weights. The integer sampler floors each selected quantity in its base unit. A floating-point sampler requires every positive-mass atom to be exactly representable as binary64 and rejects values that cannot be represented exactly. Scalar runtime fields keep their existing whole-base-unit rules.

## Examples

The scenario library uses these canonical sources. Each source has a complete scalar topology and loads as an editable draft without starting a run.

| Source | Purpose |
| --- | --- |
| [Empirical profile definitions](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/empirical-profile-definitions.lb) | Multiple valid named presets; every definition remains unbound |
| [Empirical invalid parameters](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/empirical-invalid-parameters.lb) | Observation values cannot mix time and byte dimensions. |
| [Empirical missing parameter](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/empirical-missing-parameter.lb) | Required `observations` is omitted |

## Errors

Observation values cannot mix time and byte dimensions. Supply 1 to 4096 nonnegative observations with compatible units; repeated values are allowed. The invalid and missing-parameter sources above retain compiler source locations so the editor can point to the declaration or offending value. Inline or named runtime binding remains unavailable; keep scalar runtime settings.

## References

[Gonum: empirical cumulative distribution function](https://pkg.go.dev/gonum.org/v1/gonum/stat#CDF) describes the underlying mathematical parameterization. The source syntax, exact decimal rules, finite support, and unbound runtime status on this page are this project's contract.
