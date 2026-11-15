# Uniform distribution source examples

These six small scenarios are available in the frontend scenario library under
**Distribution examples**. Select an example to load its editable source, or save a linked
`.lb` file and import it in the language editor.

Uniform syntax and parameter validation are supported. Standalone backend uniform
samplers are available, but scenario compilation and runtime field integration
are pending, so these sources cannot currently be applied or started.
The first three have valid declarations; the final three
intentionally demonstrate parameter errors. No runnable JSON versions are provided.

The files below are the canonical sources used by the frontend library.

| Source | Expected result |
| --- | --- |
| [Backend CPU declaration](../../../visualizer/src/scenarios/examples/sources/uniform-cpu-declaration.lb) | Valid `5us` to `15us` bounds; compilation unavailable. |
| [Travel with mixed units](../../../visualizer/src/scenarios/examples/sources/uniform-travel-declaration.lb) | Valid `500us` to `1ms` bounds; compilation unavailable. |
| [Timeline declaration](../../../visualizer/src/scenarios/examples/sources/uniform-timeline-declaration.lb) | Fixed initial CPU time, then valid uniform bounds at `1s`; compilation unavailable. |
| [Reversed range](../../../visualizer/src/scenarios/examples/sources/uniform-invalid-range.lb) | Minimum must be less than maximum. |
| [Incompatible units](../../../visualizer/src/scenarios/examples/sources/uniform-invalid-units.lb) | Minimum and maximum must use compatible units. |
| [Missing bound](../../../visualizer/src/scenarios/examples/sources/uniform-missing-bound.lb) | Both minimum and maximum are required. |
