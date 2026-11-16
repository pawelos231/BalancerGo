# Bernoulli distributions

## Summary

A discrete distribution with values 0 and 1. Probability is the exact chance of 1; the remaining probability belongs to 0.

Named declarations are stored definitions. They do not change CPU, links, traffic, or other scalar runtime settings. The mathematical preview does not draw random values.

## On this page

[Syntax](#syntax) · [Parameters and units](#parameters-and-units) · [Support and truncation](#support-and-truncation) · [Density preview](#density-preview) · [Numeric admission](#numeric-admission) · [Examples](#examples) · [Errors](#errors) · [References](#references)

## Syntax

```lb-fragment context=top-level
distribution quarter-success bernoulli {
    probability 25%
}
```

Place the declaration before timeline `at` blocks. Names are unique among distributions. Source family `bernoulli` is stored as JSON kind `bernoulli` in `fixed.randomProfiles`. Required source parameter names are case-sensitive.

## Parameters and units

| Parameter | Meaning | Rule |
| --- | --- | --- |
| `probability` | Probability of value 1 | Plain number from 0 through 1, or 0% through 100% |

P(X = 1) = probability and P(X = 0) = 1 - probability. Both endpoints are valid: probability 0 always gives 0, and probability 1 always gives 1. Write either `0.25` or `25%` for a 25% chance; plain `25` is outside the allowed range.

The compiler converts percentage literals to an exact fraction. JSON always
stores `unit: "number"` and `probability: "0.25"` in this example. Time and byte
suffixes such as `ms` or `KB` are rejected; outcomes are the numbers 0 and 1.

Declare probability exactly once. Unknown and repeated parameters are errors.

## Support and truncation

This family is discrete. The preview shows probability masses at the listed support values, with total mass 1. No min/max truncation parameters are accepted. A mass at one value is a probability.

Bar heights are probabilities, not density. The preview is analytical; it does
not sample outcomes or predict simulation behavior.

## Density preview

Choose a definition from the library’s **Bernoulli profile definitions** example: **quarter-success**, **never-success**, **always-success**.

The bars show probability mass at 0 and 1, with heights 1 − probability and probability. There is no density between the two outcomes.

The horizontal axis shows the complete configured support, with exact base-unit labels and padding where appropriate. The caption identifies the vertical-axis units and endpoint conventions.

This deterministic preview shows exact probability labels alongside numerical bar heights. It draws no random values and predicts no future packets.

## Numeric admission

Compilation and JSON import preserve the exact probability as a canonical
decimal string of at most 128 characters, without redundant leading zeros or
trailing fractional zeros. The probability must be between 0 and 1 inclusive.

Both standalone samplers preserve the exact rational selection probability.
Their outcomes 0 and 1 are exactly representable as integers and binary64.
Probability 0 or 1 is deterministic and consumes no RNG draw.

## Examples

The scenario library uses these canonical sources. Each source has a complete scalar topology and loads as an editable draft without starting a run.

| Source | Purpose |
| --- | --- |
| [Bernoulli profile definitions](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/bernoulli-profile-definitions.lb) | Multiple valid named presets; every definition remains unbound |
| [Bernoulli invalid parameters](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/bernoulli-invalid-parameters.lb) | A probability cannot be greater than 1. |
| [Bernoulli missing parameter](https://github.com/pawelos231/BalancerGo/blob/main/visualizer/src/scenarios/examples/sources/bernoulli-missing-parameter.lb) | Required `probability` is omitted |

## Errors

A probability cannot be greater than 1. Use a plain number from 0 through 1 or a percentage from 0% through 100%, and declare probability exactly once. The invalid and missing-parameter sources above retain compiler source locations so the editor can point to the declaration or offending value. Inline or named runtime binding remains unavailable; keep scalar runtime settings.

## References

[Gonum: Bernoulli distribution](https://pkg.go.dev/gonum.org/v1/gonum/stat/distuv#Bernoulli) describes the underlying mathematical parameterization. The source syntax, exact decimal rules, finite support, and unbound runtime status on this page are this project's contract.
