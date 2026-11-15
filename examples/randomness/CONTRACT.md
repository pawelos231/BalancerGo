# Stochastic profile contract

E5.0.1 defines a standalone profile format shared by Go and TypeScript. It does
not add a field to Scenario v2, compile a distribution into a scenario, or sample
values. The existing scalar fields, 37 runnable presets and their runtime
behavior keep their current meaning. The uniform source examples remain drafts
until the later compilation and field integration stages support them.

```json
{"kind":"constant","unit":"nanoseconds","value":"5"}
{"kind":"uniform","unit":"nanoseconds","min":"5.25","max":"15"}
```

Every definition has an explicit `kind` and canonical base `unit`. A constant
requires exactly `value`; uniform requires exactly `min` and `max`, describing
the continuous half-open interval `[min, max)`. Uniform requires `min < max`;
equal bounds use a constant instead. Definitions preserve their quantities
exactly and do not choose a realization during parsing, validation or encoding.

| Unit | Meaning | Permitted definition quantities |
| --- | --- | --- |
| `nanoseconds` | Virtual time in ns | 0 through 9223372036854775807 |
| `bytes` | Byte quantities | 0 through 9223372036854775807 |
| `bitsPerSecond` | Bit rate in bit/s | 0 through 9223372036854775807 |
| `number` | Dimensionless quantity or count | 0 through 9223372036854775807 |
| `percentage` | Percent units, so 1 means 1% | 0 through 100 |

These are inclusive definition limits. A particular scenario field may have a
narrower domain, including a strictly positive minimum, when that field gains
profile support. Percent quantities are not probability fractions: a later
probability field conversion must convert 1% to 0.01 explicitly. Source time,
byte and byte-rate units must likewise convert exactly into the chosen base
unit; source units are not accepted directly by this JSON format.

Every quantity is a JSON string containing 1 to 128 ASCII characters in canonical
nonnegative decimal form. `0`, `12` and `0.125` are valid. Signs, leading integer
zeros, exponents, whitespace, omitted integer parts, empty fractions and trailing
fractional zeros are invalid. For example, `01`, `-0`, `.5`, `1.`, `1.20` and `1e3`
are rejected. The character limit includes the decimal point. All comparison and
unit conversion must retain exact decimal digits, including integers above
JavaScript's safe integer limit and differences smaller than float64 precision.

The decoders reject nulls, numeric quantity tokens, missing required fields,
unknown or duplicate fields, inappropriate parameters, trailing JSON and all
unimplemented kinds or units. There is no generic parameter bag. Future families
extend the typed definition and its validator when their parameters are agreed.
The Go boundary is `ValidateProfile`, `DecodeProfile` and `EncodeProfile`; ordinary
`encoding/json.Unmarshal` does not provide this strict contract.

## Target conversion and distribution restrictions

Definition validation is independent of a target field. Fractional constants and
uniform bounds remain definition-valid even for `nanoseconds` and `bytes`.
Existing scalar scenario values retain their exact whole-unit admission rules;
adding this profile format does not round a scalar or alter a scalar baseline.

For a profile admitted to a native nonnegative integer target, the agreed
realization rule is to sample the continuous definition and then apply floor
exactly once. A constant follows the same conversion without drawing randomness.
Thus `[0.125, 1)` realizes 0, and `[1.1, 1.2)` realizes 1. A quantized integer can
be below a fractional lower bound; the original continuous sample remains inside
`[min, max)`. Quantized uniform values need not have equal probabilities when
the two endpoint buckets have different widths. The later integer sampler must
preserve those exact bucket probabilities without converting the bounds through
float64. Target-domain validation must check the converted support before Start.

There is no implicit clipping to a field limit. Restricting a distribution means
conditioning it on an explicitly admitted interval and renormalizing its
probabilities; clipping instead creates probability mass at a boundary. They are
different operations. This initial format accepts no additional truncation or
clipping parameters. Unbounded families must agree on explicit restrictions
before being added. Target limits cannot silently repair an invalid definition.

For a future floating point target, its precision and boundary conversion must
be selected and validated before Start. This stage supplies no floating point
target converter and does not claim that every exact decimal remains distinct
after such a conversion.

## Future scenario and runtime use

Definitions in `fixed` will be immutable for a run. A state change will select an
updated definition for future operations; each complete state resolves its own
settings rather than inheriting omitted settings from the previous state.
Configuration events at a virtual timestamp precede work events at that same
timestamp, following the engine's existing configuration priority.

A sample belongs to the operation that requested it. Started processing or a
packet already travelling retains its realized value until that operation ends;
a profile update cannot resample work in progress. Field integration stages
E5.15+ choose the exact sampling moment and correlation scope for each phenomenon.
Reusing a definition does not by itself introduce correlated samples. No new
correlation mechanism is part of this initial contract.

Future samplers receive their runtime RNG explicitly. Source compilation,
validation, metrics and UI previews must not consume that RNG. A preview shows
the definition, rather than a future runtime realization. Seed provenance and
isolated streams belong to E5.0.2; uniform sampling belongs to E5.1.2.

The source AST continues to preserve original units and exact fractional bounds.
Its current declaration checks remain separate from target-field conversion.
The explicit compilation guard stays in place until E5.1.3 supplies the complete
source-to-native validation path; field runtime use is handled in E5.15+.

## Shared validation evidence

[`profile-cases.json`](profile-cases.json) is the common Go/TypeScript corpus.
Each case contains a name, a `valid` boolean and a raw JSON `document` string,
so duplicate keys and other ambiguous JSON cannot disappear through fixture
parsing. Both implementations check the same exact bounds and rejection cases.
