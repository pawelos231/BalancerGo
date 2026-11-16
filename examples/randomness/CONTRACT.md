# Stochastic profile contract

Backend definitions and strict codecs live in `randomness/profile`, shared RNG
interfaces in `randomness/source`, and uniform implementations in
`randomness/uniform`. The root package keeps compatible aliases and constructors.
Each additional family has its own sampler package. The `randomness/samplers`
factory selects a family; profile validation, RNG interfaces and shared numerical
helpers remain separate from the implementations.

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
unsupported kinds or units. Each family admits only its typed required fields;
there is no generic parameter bag. Entries and observations use explicit arrays,
limited to 4096 items, with the same strict leaf checks.
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
different operations. Unbounded continuous families require finite `min` and `max`
and condition on that interval. Beta uses affine scaling and triangular uses
its complete bounded support instead. Discrete families have the support rules
listed below. There is no implicit clipping. Target limits cannot silently repair an invalid definition.

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
The uniform-only floating constructor still rejects constants. The generic
factory below accepts a constant only when its value is exactly representable
as binary64; neither constant path draws randomness.

These backend APIs are prepared for field integration in E5.15+. Scenario v2
retains scalar runtime settings. Preparing or storing a sampler definition does
not integrate it with a field or alter current simulation results.

## Family samplers (E5.2–E5.14)

The source names below map to native JSON kinds. All scalar parameters remain
canonical decimal strings; source units are converted exactly before encoding.

| Source / JSON kind | Required fields beyond kind and unit | Support |
| --- | --- | --- |
| `normal` | mean, stddev, min, max | Conditional [min,max); stddev > 0 |
| `exponential` | mean, min, max | Conditional [min,max); mean > 0 |
| `gamma` | shape, scale, min, max | Conditional [min,max); shape/scale > 0; integer shape is Erlang |
| `student-t` / `studentT` | location, scale, degreesOfFreedom, min, max | Conditional [min,max); scale/degreesOfFreedom > 0 |
| `beta` | alpha, beta, min, max | Affine [min,max); alpha/beta > 0 |
| `lognormal` | median, logStddev, min, max | Conditional [min,max); median/logStddev > 0 |
| `pareto` | shape, scale, min, max | Conditional [min,max); shape/scale > 0 and min >= scale |
| `weibull` | shape, scale, min, max | Conditional [min,max); shape/scale > 0 |
| `triangular` | min, mode, max | Full [min,max); min <= mode <= max |
| `bernoulli` | probability | Exact outcomes 0 and 1; 0 <= probability <= 1 |
| `poisson` | mean, min, max | Integer [min,max); mean >= 0; zero mean must include zero |
| `weighted` / `discreteWeighted` | entries: [{value, weight}, ...] | Listed values; nonnegative weights with positive total |
| `empirical` | observations: [value, ...] | Listed observations with mass 1/n each; duplicates retain frequency |

Bernoulli and Poisson require JSON unit `number`. Quantity parameters inherit
the profile unit; shape parameters and weights are dimensionless and use the
int64 ceiling, even with percentage outcomes. Probability has its separate
[0,1] limit. Thus Gamma shape `250` with percentage bounds [0,10) is valid.
The DSL accepts Bernoulli `probability 25%` as exact JSON `"0.25"`; this does
not change its outcome unit to percentage.

The generic Go boundary is `samplers.NewInteger(definition)` or
`samplers.NewFloat(definition)`, from `load-balancer/randomness/samplers`.
Both prepare immutable settings without consuming RNG. Call `Sample(rng)`
with the operation's resource/phenomenon stream. The original root constructors
remain uniform/constant compatibility APIs; they do not silently select new
families.

Integer preparation preserves the existing exact uniform path and exact Bernoulli
probabilities. Weighted and empirical samplers floor the selected exact decimal
once, retaining int64 precision; Poisson returns exact integer counts. Weighted selection scales
rational weights to integer coordinates, without floating normalization.
Empirical selection resamples observations without interpolation. Other
continuous families floor their binary64 sample once. Positive-mass discrete
outcomes must be exactly representable for the floating API; use the integer
API to retain int64 values above JavaScript's safe integer range.

Continuous samplers use inverse distribution functions or stable conditional
transforms. Their probability draw is the midpoint of one of 2^52 cells, so
neither 0 nor 1 reaches an unbounded inverse. Uniform retains its established
53-bit path and seeded sequences. Constant profiles and explicitly deterministic
preparations consume no RNG.

Exact definition validation and numerical sampler preparation are separate.
Preparation rejects empty floating supports, unresolved probability intervals
and failed numerical inversions. Conditional probability probes use tolerance
1e-7 as an admission check, not as a claimed global accuracy bound. A bounded
CDF inversion handles unavailable inverse results; there is no clipped-law
fallback. Gamma uses a narrow guard for a pinned Gonum lower-tail underflow
case, covered by regression tests.

Poisson prepares at most 65,536 outcomes with a binary64 cumulative table.
Its integer counts remain exact, but extremely small positive tail masses can
disappear through rounding. It does not provide exact rare-event probabilities;
Bernoulli and weighted selection retain their rational probabilities instead.

Special functions for Gamma, Student t and Beta come from pinned
[Gonum v0.16.0](https://pkg.go.dev/gonum.org/v1/gonum@v0.16.0/stat/distuv).
Gonum does not own or draw the simulation RNG. The analytical documentation
previews use jStat 1.9.6 and have their own explicit numerical display limits;
they are neither sampled histograms nor predictions of future packets.

Every family has a chapter under
[`visualizer/docs/language`](../../visualizer/docs/language/), canonical sources
in the frontend library, shared raw validation cases, and a native CLI fixture.
Their definitions are unbound until E5.15+; all existing runtime fields remain
scalar.


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

The canonical Uniform library source is
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
The additional families use matching `families/*-cases.json` files and native
`families/*-scenario.json` fixtures, paired with their canonical `.lb` sources.
Each raw case contains a name, a `valid` boolean and a JSON `document` string,
so duplicate keys and other ambiguous JSON cannot disappear through fixture
parsing. Both implementations check the same exact bounds and rejection cases.
