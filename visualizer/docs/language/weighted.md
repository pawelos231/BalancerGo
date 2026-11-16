# Weighted discrete distributions

## Summary

A finite list of quantity values with explicit nonnegative weights. Each entry's probability equals its weight divided by the total weight.

Named declarations are stored definitions. They do not change CPU, links, traffic, or other scalar runtime settings. The mathematical preview does not draw random values.

## On this page

[Syntax](#syntax) · [Parameters and units](#parameters-and-units) · [Support and truncation](#support-and-truncation) · [Density preview](#density-preview) · [Numeric admission](#numeric-admission) · [Examples](#examples) · [Errors](#errors) · [References](#references)

## Syntax

```lb-fragment context=top-level
distribution two-time-values weighted {
    entries {
        entry {
            value 1ms
            weight 2
        }
        entry {
            value 3ms
            weight 1
        }
        entry {
            value 3ms
            weight 1
        }
    }
}
```

Place the declaration before timeline `at` blocks. Names are unique among distributions. Source family `weighted` is stored as JSON kind `discreteWeighted` in `fixed.randomProfiles`. Required source parameter names are case-sensitive.

## Parameters and units

| Parameter | Meaning | Rule |
| --- | --- | --- |
| `entries` | Ordered list of entry blocks | 1 to 4096 entries |
| `entry.value` | Quantity at this atom | Nonnegative; all values have compatible units |
| `entry.weight` | Relative probability weight | Nonnegative plain number; total must be positive |

Weights are relative, dimensionless, and need not sum to 1. Zero-weight entries are allowed when at least one weight is positive. Repeated values are allowed; their probabilities add. Entries with weights 2, 1, and 1 have probabilities 1/2, 1/4, and 1/4, so duplicate 3ms entries in the example combine to a mass of 1/2.

Quantity parameters must share one dimension: time (`ns`, `us`, `ms`, `s`, `m`), bytes (`B`, `KB`, `MB`, `GB`, `TB`, `PB`, `EB`), percentages (`%`), or plain numbers. Different suffixes in one dimension are compatible, such as `500us` and `1ms`. JSON stores exact decimal strings in base units (`nanoseconds`, `bytes`, `percentage`, or `number`). Weights are plain numbers and do not inherit the outcome units.

Declare entries once and repeat entry blocks inside it. Each entry requires
value and weight exactly once. Unknown fields are errors; all values and
weights must be nonnegative.

## Support and truncation

This family is discrete. The preview shows probability masses at the listed support values, with total mass 1. No min/max truncation parameters are accepted. A mass at one value is a probability.

Bar heights are probabilities after combining repeated values. The preview is
analytical; it does not sample outcomes or predict simulation behavior.

## Density preview

Choose a definition from the library’s **Weighted discrete profile definitions** example: **two-time-values**, **zero-weight-time**.

The bars show probability mass after exact weight normalization. Repeated values combine into one bar; zero weights contribute no probability. Exact labels remain fractions.

The horizontal axis shows the complete configured support, with exact base-unit labels and padding where appropriate. The caption identifies the vertical-axis units and endpoint conventions.

This is a deterministic analytical approximation of the definition before integer flooring or runtime conversion. It draws no random values and predicts no future packets. If an axis or numerical calculation cannot be resolved reliably, the preview explains the limit instead of drawing a substitute curve.

## Numeric admission

Compilation and JSON import preserve exact nonnegative decimal strings, including fractions of a base unit. Every quantity and dimensionless parameter is at most 9,223,372,036,854,775,807; percentage quantities are at most 100. The JSON canonical decimal representation has at most 128 characters and no redundant leading zeros or trailing fractional zeros. The percentage ceiling does not apply to dimensionless weights.

Exact profile validation and sampler preparation are separate checks. Standalone discrete samplers preserve exact rational selection weights. The integer sampler floors each selected quantity in its base unit. A floating-point sampler requires every positive-mass atom to be exactly representable as binary64 and rejects values that cannot be represented exactly. Scalar runtime fields keep their existing whole-base-unit rules.

## Examples

The scenario library uses these canonical sources. Each source has a complete scalar topology and loads as an editable draft without starting a run.

| Source | Purpose |
| --- | --- |
| [Weighted discrete profile definitions](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/weighted-profile-definitions.lb) | Multiple valid named presets; every definition remains unbound |
| [Weighted discrete invalid parameters](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/weighted-invalid-parameters.lb) | All-zero weights cannot be normalized into probabilities. |
| [Weighted discrete missing parameter](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/weighted-missing-parameter.lb) | Required `entries` is omitted |

## Errors

All-zero weights cannot be normalized into probabilities. Supply 1 to 4096 entries with compatible value units and nonnegative plain-number weights, including at least one positive weight. The invalid and missing-parameter sources above retain compiler source locations so the editor can point to the declaration or offending value. Inline or named runtime binding remains unavailable; keep scalar runtime settings.

## References

[Gonum: categorical sampling of weighted alternatives](https://pkg.go.dev/gonum.org/v1/gonum/stat/distuv#Categorical) describes the underlying mathematical parameterization. The source syntax, exact decimal rules, finite support, and unbound runtime status on this page are this project's contract.
