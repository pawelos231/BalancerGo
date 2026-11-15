# Uniform distribution source examples

These seven small scenarios are available in the frontend scenario library under
**Distribution examples**. Select an example to load its editable source, or save a linked
`.lb` file and import it in the language editor.

Named uniform definitions compile to `fixed.randomProfiles` and pass native
backend validation. They are unbound, so they do not change the scalar CPU or
link settings of a run. Inline runtime field integration is pending. The original
three field declarations remain drafts; three invalid examples show parameter
errors. See the shared [compiled definition fixture](../../randomness/uniform-scenario.json).

The files below are the canonical sources used by the frontend library.

| Source | Expected result |
| --- | --- |
| [Named profile definitions](../../../visualizer/src/scenarios/examples/sources/uniform-profile-definitions.lb) | Compiles exact CPU, travel and high-precision byte definitions; runtime fields remain scalar. |
| [Backend CPU declaration](../../../visualizer/src/scenarios/examples/sources/uniform-cpu-declaration.lb) | Valid `5us` to `15us` bounds; CPU binding pending. |
| [Travel with mixed units](../../../visualizer/src/scenarios/examples/sources/uniform-travel-declaration.lb) | Valid `500us` to `1ms` bounds; link binding pending. |
| [Timeline declaration](../../../visualizer/src/scenarios/examples/sources/uniform-timeline-declaration.lb) | Fixed initial CPU time, then valid uniform bounds at `1s`; runtime binding pending. |
| [Reversed range](../../../visualizer/src/scenarios/examples/sources/uniform-invalid-range.lb) | Minimum must be less than maximum. |
| [Incompatible units](../../../visualizer/src/scenarios/examples/sources/uniform-invalid-units.lb) | Minimum and maximum must use compatible units. |
| [Missing bound](../../../visualizer/src/scenarios/examples/sources/uniform-missing-bound.lb) | Both minimum and maximum are required. |
