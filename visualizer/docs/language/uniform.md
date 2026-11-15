# Uniform distributions

## Summary

A uniform definition describes values between `min` and `max`, with equal
probability over equal-width intervals. Named definitions compile into JSON,
but remain unbound: they do not yet change CPU, travel or traffic settings.
The preview shows their mathematical shape without drawing random values.

## On this page

[Declare a definition](#declare-a-definition) ·
[Parameters and units](#parameters-and-units) ·
[Read the density](#read-the-density) · [Density preview](#density-preview) ·
[Integer and floating-point values](#integer-and-floating-point-values) ·
[Current support](#current-support) · [Errors](#errors) ·
[Library examples](#library-examples)

## Declare a definition

This valid top-level fragment declares a range from 5us up to 15us:

```lb-fragment context=top-level
distribution cpu-time uniform {
    min 5us
    max 15us
}
```

Place named definitions before any `at` blocks. Names use lowercase letters
and internal hyphens, such as `cpu-time`, and must be unique among distributions.
A name describes the definition; calling it `cpu-time` does not attach it to a CPU.

Compilation stores this definition at `fixed.randomProfiles["cpu-time"]`, with
`kind` set to `uniform`, `unit` set to `nanoseconds`, and exact decimal strings
`"5000"` and `"15000"` for the bounds. JSON import and export preserve these
definitions. Compilation validates them without sampling.

## Parameters and units

| Parameter | Meaning | Rule |
| --- | --- | --- |
| `min` | Included lower bound | Required, nonnegative |
| `max` | Excluded upper bound | Required, strictly greater than `min` |

Both parameters must use the same kind of quantity. Different units within
that kind are compatible: `min 500us` and `max 1ms` describe `[500000, 1000000)`
in nanoseconds. `min 1ms` and `max 1KB` are incompatible.

| Quantity | Source units | Stored base unit |
| --- | --- | --- |
| Time | `ns`, `us`, `ms`, `s`, `m` | Nanoseconds |
| Bytes | `B`, `KB`, `MB`, `GB`, `TB`, `PB`, `EB` | Bytes, with decimal powers of 1000 |
| Percentage | `%` | Percentage points, from 0 through 100 |
| Plain number | No suffix | Number |

Definition bounds can contain exact fractions of a base unit, such as
`0.125ns`. They are not rounded during compilation. Other than percentages,
each bound must be at most 9,223,372,036,854,775,807 in its base unit.
The canonical decimal stored in JSON is limited to 128 characters.
Scalar runtime settings retain their own whole-unit rules; see
[syntax and units](/docs/language/syntax#units).

## Read the density

The range uses **`[min, max)`**: the minimum belongs to it and the maximum
does not. For `[5us, 15us)`, a value can be at least 5us and must be below 15us.
Equal bounds do not define a uniform range; use a fixed scalar instead.

The probability density is flat at `1 / (max − min)` inside the range and zero
outside. Wider ranges have lower density because the total area remains 1.
Density is measured per unit on the horizontal axis. The probability of an
interval is its area under the curve: `[5us, 10us)` accounts for half the area
of `[5us, 15us)`, so its probability is 50%.

A density height is not the probability of one exact value. In the continuous
definition, any individual point has probability zero. Endpoint inclusion
still matters when values are converted or represented by a sampler.

## Density preview

Choose a definition from **Uniform profile definitions** in the library, or
compare it with that example's current fixed backend CPU cost. The axes show
the full support and their padding explicitly. The fixed reference is one
point, rather than a finite-height density curve.

This is the continuous mathematical definition before conversion to a target
type. It is not a histogram of sampled values or a simulation prediction.
For the precise byte example, integer conversion collapses the interval to
one whole byte value, while a floating-point sampler rejects it because no
representable value lies inside the interval.

## Integer and floating-point values

The standalone integer sampler draws within the declared range and applies
`floor` once in the target base unit. Fractional bounds change the weights
of the edge buckets. For example, `[1.5, 3)` produces integer 1 with probability
one third and integer 2 with probability two thirds. The floored result can be
below a fractional `min`; the continuous value before flooring stays inside
`[min, max)`.

A range whose values all floor to the same integer needs no random draw.
The standalone floating-point sampler approximates the continuous definition
with binary64 values while preserving its endpoint convention. Some exact
decimal ranges contain no binary64 value and are rejected during sampler
preparation. These samplers do not yet provide runtime field binding.

## Current support

Named `distribution` declarations compile, survive native JSON validation and
round-trip through files. They stay fixed, unbound definitions. Existing scalar
settings continue to control the simulation. Network `profile` declarations
and `use` still select network settings; they do not select a distribution.

Inline uniform values inside `packet-time`, `travel`, defaults or timeline
setters are recognized and checked, then rejected with an explicit runtime
sampling diagnostic. There is no supported syntax to attach a named definition
to a runtime field yet.

The preview never uses the simulation's random streams. Opening the guide or
switching examples does not consume random draws. The run's `seed` controls
existing runtime randomness, such as random routing and loss; it does not
activate an unbound definition. See [Time and playback](/docs/language/syntax#time-and-playback)
and [how virtual time works](concept:virtual-time).

## Errors

Both bounds are required, each parameter may appear once, and no other
parameters are accepted. This top-level fragment has an empty range:

```lb-invalid context=top-level error="Uniform min must be less than max; use a scalar for a fixed value."
distribution empty uniform {
    min 5us
    max 5us
}
```

| Problem | Diagnostic | Fix |
| --- | --- | --- |
| A missing bound | `Uniform requires both min and max bounds.` | Supply both `min` and `max` |
| Equal or reversed bounds | `Uniform min must be less than max; use a scalar for a fixed value.` | Order the bounds, or use a scalar for a fixed value |
| Mixed time and byte bounds | `Uniform min and max must use compatible units.` | Use one kind of quantity |
| Repeated `max` | `Duplicate uniform parameter "max".` | Write each parameter once |
| A parameter such as `mean` | `Unknown uniform parameter "mean".` | Use only `min` and `max` |
| Inline runtime field use | `Uniform definitions compile as named distributions; runtime field sampling is not available yet.` | Keep a scalar runtime setting and declare an unbound named definition separately |

Names, unsupported families, negative values and bounds above the limits also
fail validation. Diagnostics retain source locations. See
[Fixing errors](/docs/language/errors#uniform-definitions) for the repair workflow.

## Library examples

Open **Simulation → Scenario library**, choose **Distribution examples**, and
search for these titles. Each entry loads its canonical `.lb` source as a draft;
loading it does not start a run.

| Library title | What it demonstrates |
| --- | --- |
| Uniform profile definitions | Three valid named definitions, including exact fractional bytes; scalar CPU and link settings remain in effect |
| Uniform backend CPU declaration | Valid inline CPU bounds and the pending runtime binding diagnostic |
| Uniform travel with mixed units | Compatible time units and the pending link binding diagnostic |
| Uniform timeline declaration | An inline CPU definition at 1s and the pending timeline binding diagnostic |
| Uniform reversed range | `min` greater than `max` |
| Uniform incompatible units | A time bound paired with a byte bound |
| Uniform missing bound | An omitted `max` |

The named-definition example can be compiled and applied. Starting that
scenario runs its existing scalar settings; the three stored definitions
remain unbound. Other entries demonstrate the expected diagnostics.
