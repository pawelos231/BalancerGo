# Poisson distributions

## Summary

A distribution over nonnegative whole-number counts. Its dimensionless mean defines the underlying count distribution, and integer bounds retain a finite set of counts.

Named declarations are stored definitions. They do not change CPU, links, traffic, or other scalar runtime settings. The mathematical preview does not draw random values.

## On this page

[Syntax](#syntax) · [Parameters and units](#parameters-and-units) · [Support and truncation](#support-and-truncation) · [Density preview](#density-preview) · [Numeric admission](#numeric-admission) · [Examples](#examples) · [Errors](#errors) · [References](#references)

## Syntax

```lb-fragment context=top-level
distribution small-count poisson {
    mean 4
    min 0
    max 12
}
```

Place the declaration before timeline `at` blocks. Names are unique among distributions. Source family `poisson` is stored as JSON kind `poisson` in `fixed.randomProfiles`. Required source parameter names are case-sensitive.

## Parameters and units

| Parameter | Meaning | Rule |
| --- | --- | --- |
| `mean` | Underlying expected count | Nonnegative plain number; fractions allowed |
| `min` | First retained count | Nonnegative integer plain number |
| `max` | Excluded upper count | Integer plain number strictly greater than min |

For mean > 0, the underlying mass at integer k is exp(-mean) * mean^k / k!. The retained support is the integers min <= k < max, normalized over that set. Mean may be fractional, but min and max must be integers. Mean 0 is a point mass at 0 and therefore requires min 0 so that the support includes zero.

This family only accepts plain numbers. Unit suffixes such as ms, KB, and % are rejected. The JSON unit is `number`.

Each required parameter appears exactly once. Unknown and repeated parameters are errors. Mean and bounds must be nonnegative.

## Support and truncation

Support contains the integer counts **min <= k < max**. It has no fractional atoms and no continuous density. Truncation divides each retained Poisson mass by the sum of masses in the retained support. A zero mean requires zero to remain in that support.

Bar heights are probabilities at individual counts. The preview is analytical; it does not sample outcomes or predict simulation behavior.

## Density preview

Choose a definition from the library’s **Poisson profile definitions** example: **small-count**, **zero-count**.

The bars show the Poisson PMF conditional on the integer outcomes in [min, max). The excluded maximum has no bar. The numerical bar heights sum to 1.

The horizontal axis shows the complete configured support, with exact base-unit labels and padding where appropriate. The caption identifies the vertical-axis units and endpoint conventions.

This deterministic preview approximates the conditioned Poisson PMF. It draws no random values and predicts no future packets. If the configured interval cannot be displayed reliably, the preview explains the limit instead of omitting outcomes.

## Numeric admission

Compilation and JSON import preserve mean and bounds as exact nonnegative decimal strings. Mean may be fractional; bounds must be integers. Each value is at most 9,223,372,036,854,775,807. The canonical decimal representation has at most 128 characters and no redundant leading zeros or trailing fractional zeros.

Exact profile validation and sampler preparation are separate checks. A valid definition can exceed a standalone sampler's table capacity. The floating sampler also requires every retained count to be exactly representable as binary64. Preparation rejects unsupported definitions instead of substituting another distribution. Scalar runtime fields keep their existing whole-base-unit rules.

The integer sampler prepares a cumulative probability table for at most 65,536
outcomes. Counts remain exact integers, but the probabilities use binary64:
extremely small positive tail masses can disappear through rounding. This
sampler does not promise exact rare-event probabilities. Bernoulli and weighted
selection preserve their exact rational probabilities instead.

The documentation chart has a separate display limit of 512 bars; both mean
and the excluded upper bound max must be at most 1,000,000. Unresolved tail
masses also make the chart unavailable. It explains unsupported inputs rather
than silently omitting outcomes from the configured interval.

## Examples

The scenario library uses these canonical sources. Each source has a complete scalar topology and loads as an editable draft without starting a run.

| Source | Purpose |
| --- | --- |
| [Poisson profile definitions](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/poisson-profile-definitions.lb) | Multiple valid named presets; every definition remains unbound |
| [Poisson invalid parameters](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/poisson-invalid-parameters.lb) | Poisson support bounds must be whole numbers. |
| [Poisson missing parameter](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/poisson-missing-parameter.lb) | Required `mean` is omitted |

## Errors

Poisson support bounds must be whole numbers with min less than max. Supply mean and both bounds as plain numbers; include zero when mean is zero. The invalid and missing-parameter sources above retain compiler source locations so the editor can point to the declaration or offending value. Inline or named runtime binding remains unavailable; keep scalar runtime settings.

## References

[Gonum: Poisson distribution](https://pkg.go.dev/gonum.org/v1/gonum/stat/distuv#Poisson) describes the underlying mathematical parameterization. The source syntax, exact decimal rules, finite support, and unbound runtime status on this page are this project's contract.
