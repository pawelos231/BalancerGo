# Stochastic profile contract

The profile format is shared by Go and TypeScript. Since E5.1.3, Scenario v2 can
retain named definitions in optional `fixed.randomProfiles`. Definitions are
unbound: storing one does not assign it to a host, link or client setting.
Existing scalar fields and runnable presets keep their runtime meaning.

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
the two endpoint buckets have different widths. The integer sampler preserves
those exact bucket probabilities without converting the bounds through
float64. Target-domain validation must check the converted support before Start.

There is no implicit clipping to a field limit. Restricting a distribution means
conditioning it on an explicitly admitted interval and renormalizing its
probabilities; clipping instead creates probability mass at a boundary. They are
different operations. This initial format accepts no additional truncation or
clipping parameters. Unbounded families must agree on explicit restrictions
before being added. Target limits cannot silently repair an invalid definition.

Floating point admission must likewise be checked before Start. E5.1.2 supplies
a separate uniform float64 constructor, described below; an exact definition can
remain valid even when its floating representation is unavailable.

## Backend uniform samplers

E5.1.2 compiles immutable sampler settings once, without drawing randomness.
`NewIntegerSampler(profile)` accepts constants and uniform definitions and returns
whole base units. `NewUniformFloatSampler(profile)` accepts only uniform and
returns a float64 approximation. Both expose `Sample(source)` and receive an
explicit `RNG` with `Uint64() uint64`; a stream from `NewRandomGenerators` satisfies it.
The RNG must produce uniform full-width words. Neither sampler owns a global RNG.

```go
profile, err := randomness.DecodeProfile([]byte(
    `{"kind":"uniform","unit":"nanoseconds","min":"5000","max":"15000"}`,
))
if err != nil { panic(err) }
sampler, err := randomness.NewIntegerSampler(profile)
if err != nil { panic(err) }
generators := randomness.NewRandomGenerators(42)
packetTime := sampler.Sample(generators.Stream("backend/api-0", "cpu-time"))
```

The integer sampler preserves exact quantized probabilities. Whole bounds use
unbiased bounded uint64 draws. Fractional bounds are scaled to a common integer
denominator and sampled without float64 conversion. For `[0.5,2.5)`, the integer
probabilities are 0: 25%, 1: 50%, 2: 25%. A constant or a uniform interval that
always floors to one integer consumes no draws. For example, `[1.1,1.2)` always
produces 1. Bounds are validated and copied at preparation; changing the input
profile later cannot change an existing sampler.

The floating sampler uses 53 random bits and linear interpolation. It selects
the first representable value at or above each exact decimal boundary; the upper
one remains exclusive. Rounding at the upper endpoint returns the preceding
representable value. Thus results stay in the original exact `[min,max)`, but
floating rounding and endpoint adjustment are an approximation of the continuous
density, not an exact decimal distribution or equal weights over float64 values.
An interval with no representable float64 value is rejected during preparation;
one with exactly one result needs no draw. For example,
`[9007199254740992.1,9007199254740992.2)` is definition-valid but unavailable as
float64, while its integer conversion can still return 9007199254740992.
Floating constant conversion is not provided in this stage.

These backend APIs are prepared for field integration in E5.15+. Scenario v2
retains scalar runtime settings. Preparing or storing a sampler definition does
not integrate it with a field or alter current simulation results.

## Named definitions in a scenario

```text
distribution cpu-time uniform {
    min 5us
    max 15us
}
```

This compiles to `fixed.randomProfiles["cpu-time"]` with `kind: "uniform"`,
`unit: "nanoseconds"`, `min: "5000"` and `max: "15000"`. Named definitions must
precede every `at` block and have unique lowercase letter/hyphen names. They
occupy a separate namespace from endpoint network `profile` declarations. There
is no `use` binding or named reference syntax for random profiles yet.

Source units infer nanoseconds, bytes, plain numbers or percentage units.
In an unbound definition, `1GB` is a byte quantity. Native JSON additionally
supports explicit `bitsPerSecond`; the compiler does not infer a bit rate from
a definition's name. Bounds convert to canonical decimal strings with exact
bigint arithmetic; no sampling or whole-unit rounding occurs during compilation.

Go and TypeScript validate every definition, including unused ones, before Start.
The native codec delegates each leaf to the strict profile validator, so null,
duplicate, missing and unknown parameters remain errors. Native JSON also accepts
constant definitions using the same standalone contract. The registry is optional
and omitted from old documents. Definitions remain in the original admitted
scenario, CLI manifest and exported results; normalized runtime settings omit
unbound definitions deliberately.

The canonical library source is
[`uniform-profile-definitions.lb`](../../visualizer/src/scenarios/examples/sources/uniform-profile-definitions.lb),
with its shared compiled fixture [`uniform-scenario.json`](uniform-scenario.json).
It can be compiled and validated; its baseline simulation still uses scalar
settings. The other six uniform source examples remain runtime drafts or error
examples. Inline `packet-time uniform { ... }` and `travel uniform { ... }` are
explicitly rejected until their respective field integrations are implemented.

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

Samplers receive their runtime RNG explicitly. Source compilation,
validation, metrics and UI previews must not consume that RNG. A preview shows
the definition, rather than a future runtime realization. Seed provenance and
isolated streams belong to E5.0.2; uniform sampling belongs to E5.1.2.

## Seeded streams and experiment provenance

E5.0.2 supplies `randomness.NewRandomGenerators(seed)`. Each run owns advancing streams
keyed by `(resourceID, phenomenon)`. Repeating a lookup continues the same stream;
creating or drawing from another pair does not advance it. The DES goroutine owns
the registry and its generators, so they do not add locks or concurrent access.
Future CPU times, travel times, arrivals and payload sizes can use separate
phenomena. Reusing a profile does not mean sharing its random sequence.

`resource-phenomenon-v1` uses Go's legacy `math/rand` source. Existing algorithm
selection, balancer loss and `link/` loss/congestion bindings retain their prior
seeds and realized behavior. Other pairs derive their seed with FNV-1a-64 from the
master seed (8 little-endian bytes), followed by the model, resource and phenomenon
UTF-8 strings, each prefixed by its 8-byte little-endian byte length. This makes
tuple boundaries explicit. Stream ownership is separate even if derived seeds
collide; this scheme does not promise statistically independent PRNG sequences.

New live metadata and CLI manifests include `randomnessModel` alongside the
existing model/build identity. The executed scenario retains the exact signed
64-bit seed. Old saved results without `randomnessModel` remain readable and do
not silently gain a claimed version. Future nonempty version IDs are preserved.
Reproduction requires the scenario, seed, model/build and stream/sampler versions;
the stream version alone is not a complete description of an experiment.

Measurements and topology/configuration reads consume no runtime draws. A change
to traffic or routing can still change which packets request loss draws: the same
seed does not promise the same packet losses across different algorithms.

The source AST continues to preserve original units and exact fractional bounds.
Its current declaration checks remain separate from target-field conversion.
Named definitions use the complete source-to-native validation path. Inline
runtime fields keep a guard until field use is handled in E5.15+.

## Shared validation evidence

The frontend's [Uniform distributions chapter](../../visualizer/docs/language/uniform.md)
explains the source syntax and includes an analytical density preview. The chart
uses exact labels and normalized coordinates, with no RNG or predicted runtime
samples. Integer quantization is explained separately from continuous density.

[`profile-cases.json`](profile-cases.json) is the common Go/TypeScript corpus.
Each case contains a name, a `valid` boolean and a raw JSON `document` string,
so duplicate keys and other ambiguous JSON cannot disappear through fixture
parsing. Both implementations check the same exact bounds and rejection cases.
