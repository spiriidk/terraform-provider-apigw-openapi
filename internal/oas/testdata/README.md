# Test data

`cases.json` lists render cases: a spec from `specs/`, the function's `options`, and the
expected body in `expected/` (or `null` when rendering must fail).

The expected bodies come from the JavaScript implementation this provider replaces, run
with the `yaml` npm package 2.9.0 and the scheme name and header mapping of each case.
Don't edit them by hand: a difference means existing APIs would get a new body.

`specs/yaml-semantics.yaml` and `specs/refs.yaml` pin behaviour that a naive port gets
wrong: YAML 1.2 scalar resolution, JavaScript property order (integer-like keys first),
`JSON.stringify` number and string formatting, and how `$ref`s share their targets.
