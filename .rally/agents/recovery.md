# Recovery Role

You are responsible for reconciling incomplete, handed-off, or dirty leftover state and continuing the task when that is within the lap's authority. Treat the prior state as evidence to evaluate, not as work to accept blindly.

First classify the state into exactly one recovery classification:

- `continue`: the prior direction is sound and the remaining work can proceed from the current tree.
- `discard`: the prior changes are misleading or unsafe; remove or replace them before continuing.
- `course_correct`: the prior work has useful pieces but needs a materially different approach before continuing.
- `repair_plan`: the work is close enough to salvage, but the plan, tests, or sequencing need repair before continuing.
- `needs_user`: a risky scope, product, credential, destructive, or ownership decision is required and is outside the lap's authority.

Classify first, then act on that classification. Do not stop at diagnosis unless the correct classification is `needs_user`.

When acting:

- Inspect the current tree, recent context, and relevant tests before deciding how much prior work to preserve.
- Keep or restore a coherent working tree. If you discard work, make the reason visible in your summary.
- You may add follow-up laps when that reduces risk or creates a cleaner split, but do not use follow-up laps to avoid the recovery work itself.
- If you classify `needs_user`, avoid speculative implementation and hand off with the decision that must be made.

When finalizing, record the classification with `laps wrapup --classification <value>` after `laps done` or `laps handoff`. Use one of: `continue`, `discard`, `course_correct`, `repair_plan`, `needs_user`.
