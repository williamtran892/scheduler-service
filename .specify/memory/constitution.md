<!--
Sync Impact Report
- Version change: unversioned template → 1.0.0 (initial ratification)
- Modified principles: none (all five principles newly defined from template placeholders)
  - [PRINCIPLE_1] → I. Integration-First Feature Validation
  - [PRINCIPLE_2] → II. Test-First Development (NON-NEGOTIABLE)
  - [PRINCIPLE_3] → III. Simplicity
  - [PRINCIPLE_4] → IV. Specs Are Ephemeral
  - [PRINCIPLE_5] → V. Living Documentation
- Added sections: Testing Standards; Development Workflow & Quality Gates; Governance (filled)
- Removed sections: none
- Templates: plan/spec/tasks templates read this constitution at runtime; no edits required
- Follow-up TODOs: none. Remove this comment block before committing.
-->

# Scheduler Service Constitution

## Core Principles

### I. Integration-First Feature Validation

Every feature MUST begin with integration tests written in Gherkin (`Given` / `When` /
`Then`) before any unit test or implementation code exists. These scenarios define the
observable behavior of the feature from the outside and are the executable contract for
the work.

- A feature's Gherkin scenarios MUST be written and approved by the user before
  implementation starts.
- Scenarios MUST describe behavior through the service's real boundaries (API, scheduler
  triggers, persistence, external integrations), not internal function calls.
- Scenarios MUST fail when first run. A scenario that passes before implementation is
  either redundant or wrong and MUST be revised.
- Implementation is complete only when every approved scenario for the feature passes.

Rationale: writing integration scenarios first forces the behavior to be decided before
the design, so the implementation is shaped by what the feature must do rather than by
whatever was easiest to build.

### II. Test-First Development (NON-NEGOTIABLE)

TDD is mandatory for all code. The cycle is strictly: tests written → user approved →
tests fail (Red) → implement until they pass (Green) → refactor with tests still passing.
No step may be skipped or reordered.

- Unit tests MUST be written before the unit they test, after the feature's integration
  scenarios exist.
- Every test MUST be observed failing before the code that makes it pass is written.
- Production code MUST NOT be written without a failing test that requires it.
- Refactoring MUST leave the test suite green; behavior changes require a new failing
  test first.
- Tests MUST be reviewed and approved by the user before implementation begins. The
  approval is the gate between Red and Green.

Rationale: a test that exists before the code proves the code is needed and proves it
works. Tests written afterward only prove the code does what it already does.

### III. Simplicity

Code MUST favor the reader over the writer. The measure of good code in this project is
how quickly a developer can read it, review it, and debug a change to it.

- Prefer the plain, direct solution. Abstractions, indirection, and generalization MUST
  be justified by a present need, never a speculated future one (YAGNI).
- Each function, type, and module MUST have one clear purpose that is evident from its
  name and shape without reading its callers.
- Control flow MUST be easy to follow top to bottom. Avoid clever constructs, hidden
  side effects, and implicit behavior.
- Changes MUST be small and focused so a reviewer can hold the whole diff in mind.
- Errors MUST surface with enough context to locate the cause without a debugger.
- Any added complexity MUST be recorded and justified in the plan's Complexity Tracking
  section; unjustified complexity is grounds for rejecting a change.

Rationale: most of a codebase's lifetime is spent being read, reviewed, and debugged.
Optimizing for those activities pays back continuously; optimizing for writing pays
back once.

### IV. Specs Are Ephemeral

Feature specifications, plans, and task lists under `specs/` are working documents that
drive implementation. Once a feature is implemented, its spec artifacts are frozen and
serve only as historical reference.

- A completed spec MUST NOT be treated as the current description of system behavior.
  The implementation and the project documentation are authoritative.
- Completed specs MUST NOT be edited to track later changes. New work gets a new spec.
- Nothing in the codebase or docs may depend on a spec remaining accurate after
  implementation.

Rationale: specs capture intent at a moment in time. Keeping them "current" duplicates
the documentation and guarantees one copy drifts.

### V. Living Documentation

Project documentation under `docs/` is the single source of truth for how the system
works and MUST always match the implementation. Documentation is updated as part of the
same change that alters the system, never afterward.

- `docs/` MUST contain and maintain at minimum these key sections: **System
  Architecture**, **Features**, and **Development Standards**.
- Every change delivered from a spec MUST update the affected documentation sections
  before the feature is considered complete. The feature's task list MUST include these
  documentation tasks.
- Documentation MUST describe the implemented behavior, not the planned behavior. When
  the implementation diverges from the spec, the docs follow the implementation.
- A change that alters architecture, adds or modifies a feature, or changes a
  development practice is incomplete until the corresponding docs section is updated.

Rationale: documentation that lags the code is worse than none, because readers trust it.
Tying doc updates to the change that needs them is the only reliable way to keep them
true.

## Testing Standards

- Integration tests MUST be expressed as Gherkin feature files, one or more per feature,
  with step definitions kept simple and readable per Principle III.
- Test layering: Gherkin integration scenarios define *what* the feature does; unit tests
  define *how* each component behaves. Both are written before implementation.
- Tests MUST be deterministic. Scheduling and time-dependent behavior MUST be tested
  against controllable clocks or triggers, never wall-clock sleeps.
- Tests MUST be readable as specification: scenario and test names describe behavior in
  plain language, not implementation details.
- The full test suite MUST pass before a change is merged. A red suite blocks merge.
- Test code is held to the same simplicity standard as production code.

## Development Workflow & Quality Gates

The Spec Kit flow (`/speckit-specify` → `/speckit-plan` → `/speckit-tasks` →
`/speckit-implement`) is the standard path for feature work. Within that flow the
following gates apply, in order:

1. **Behavior gate**: Gherkin scenarios written and approved by the user. No
   implementation tasks may start before this.
2. **Red gate**: all new integration and unit tests exist and have been observed failing.
3. **Green gate**: implementation makes the tests pass with no unrelated changes.
4. **Refactor gate**: code reviewed against Principle III with the suite still green.
5. **Docs gate**: `docs/` System Architecture, Features, and Development Standards
   sections updated to reflect the delivered change.
6. **Review gate**: reviewer verifies each gate above was honored and that any added
   complexity is justified in the plan.

A change that fails any gate MUST be sent back to that gate; it MUST NOT proceed by
exception.

## Governance

This constitution supersedes all other development practices, conventions, and templates
in this project. Where a template, skill, or prior habit conflicts with a principle here,
the principle wins.

- **Amendments**: changes to this document are made through `/speckit-constitution`,
  MUST state the rationale for the change, and MUST update the version and Last Amended
  date. Amendments that affect in-flight features MUST note how those features comply.
- **Versioning**: semantic versioning. MAJOR for removing or redefining a principle in a
  backward-incompatible way; MINOR for adding a principle or section or materially
  expanding guidance; PATCH for clarifications and wording that do not change meaning.
- **Compliance review**: every plan MUST pass the Constitution Check against these
  principles before design begins and again after design. Every code review MUST
  verify the gates in Development Workflow & Quality Gates. Violations are recorded in
  the plan's Complexity Tracking section with justification or the change is rejected.
- **Runtime guidance**: the Development Standards section of `docs/` carries the
  day-to-day guidance derived from this constitution and MUST be kept consistent with it.

**Version**: 1.0.0 | **Ratified**: 2026-10-12 | **Last Amended**: 2026-10-12
