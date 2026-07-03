## Mode: Impact Assessment (`pass_mode: impact_assessment`)

You will receive:
- User feedback on a specific phase's payloads
- The full `tasks_index.json` (all tasks across all phases)
- The name of the phase being reviewed
- A list of phases already approved by the user

Determine whether the user's feedback requires **structural changes** (adding, removing, splitting,
or re-routing tasks; changing dependencies between tasks) or only **payload-level revisions**
(updating implementation notes, acceptance criteria, target files, or other content within
existing task payloads without changing the task structure).

Respond with ONLY a JSON object (no markdown fencing, no explanation outside the JSON):

```json
{
  "structural": false,
  "affected_phases": ["Phase 3: Phase Name"],
  "revision_notes": "Brief description of what changed and why.",
  "payload_guidance": "Specific guidance for re-generating the phase's payloads."
}
```

When `structural` is `true`, include `skeleton_guidance` instead of `payload_guidance`:

```json
{
  "structural": true,
  "affected_phases": ["Phase 3: Phase Name", "Phase 4: Phase Name"],
  "revision_notes": "Brief description of structural changes needed.",
  "skeleton_guidance": "Specific guidance for regenerating the skeleton: which tasks to add/remove/split, which dependency edges change."
}
```

Rules:
- `structural: true` when feedback requires adding new tasks, removing tasks, splitting a task
  into multiple, merging tasks, changing task IDs, or altering dependency edges in the DAG.
- `structural: false` when feedback only changes content within existing task payloads (objectives,
  implementation notes, acceptance criteria, target files, non-goals).
- `affected_phases` must list ALL phases whose tasks are affected — including downstream phases
  whose dependencies or payloads reference the changed tasks.
- When in doubt, prefer `structural: true` (conservative — triggers full re-evaluation).
