---
name: skill-creator
description: Create or update Pudding global skills. Trigger when the user asks to create a new skill, turn repeated experience into a skill, or improve an existing skill.
---

# Skill Creator

Pudding has two skill scopes:

- Global skills live under `<home>/skills/<name>/SKILL.md` and are not tied to an app.
- App skills live inside an app definition and describe how to use that app's endpoints.

Use this skill for global skills only. If the skill describes REST or GraphQL operations for one app, create an app skill instead.

## Write Location

`<home>` is the Pudding data directory:

- Dev builds: `~/.pudding-dev`
- Release builds: `~/.pudding`

Global user skills live here:

```text
<home>/skills/<name>/SKILL.md
<home>/skills/<name>/assets/icon.svg
```

LLM-created or LLM-edited skills must be drafted first:

```text
<home>/skills-draft/<name>/SKILL.md
<home>/skills-draft/<name>/assets/icon.svg
```

Only the user can publish a draft from Settings > Knowledge. Do not write directly to `<home>/skills/` unless the user explicitly asks to bypass the review flow outside the normal app behavior.

Builtin skills are bundled in the binary and appear as `builtin/<name>` in the UI. Do not edit or overwrite builtin skills.

At runtime Pudding injects only the skills index (`name`, `description`, path/source metadata) into the system prompt. The full `SKILL.md` body is loaded on demand with `builtin_skill_read(skill_id="<name>")` when the user's intent matches the description.

## Standard Flow

1. Align on intent: confirm what problem the skill solves and what the user would normally say that should trigger it.
2. Choose a name: use lowercase kebab-case with letters, numbers, and hyphens only.
3. Create or update the draft directory under `<home>/skills-draft/<name>/`.
4. Write `SKILL.md` with YAML frontmatter plus concise operational instructions.
5. Add `assets/icon.svg` by default unless the user explicitly says not to.
6. Call `builtin_skill_validate` with `draft_id`.
7. Fix validation errors by editing the draft.
8. Call `builtin_skill_submit` so the draft appears in Settings for user review.
9. Tell the user the draft is ready to review in Settings > Knowledge.

## Available Draft Tools

- Use `builtin_file_list` to inspect `scope="skill_draft"` or `scope="skill_published"`; pass `path="."` to list a scope root.
- Use `builtin_file_read` to inspect existing draft or published files.
- Use `builtin_file_write` to create or overwrite draft files.
- Use `builtin_file_patch` for precise draft edits.
- Use `builtin_file_delete` and `builtin_file_move` only inside `skill_draft` when needed.
- Use `builtin_skill_validate` before submission.
- Use `builtin_skill_submit` after validation passes.

The `skill_published` scope is read-only. Publishing is done by the user in Settings, not by a tool call.

When updating an existing skill, the draft is incremental:

- Write only files that actually change under `skill_draft/<name>/...`.
- Unmentioned published files such as `assets/icon.svg` are preserved automatically.
- Use `builtin_file_delete` only when the user explicitly wants to remove a published file; it records the deletion in the draft.
- Use `builtin_file_patch` when editing a published text file; it copies only that file into the draft and applies the patch.

## Minimal SKILL.md Template

```markdown
---
name: <kebab-case-name>
description: State what this skill does and what user request should trigger it.
---

# <Title>

## Procedure

1. First step
2. Second step

## Gotchas

- Important pitfall or boundary.
```

`name` and `description` are the trigger metadata. Keep operational details in the body.

## Description Guidelines

- Explain both what the skill does and when to use it.
- Include representative trigger wording.
- Keep it to one or two sentences.
- Avoid vague descriptions such as "help the user".

## Icons

New skills should include an icon by default.

Pudding discovers one icon per skill in this order:

```text
<name>/assets/icon.png
<name>/assets/icon.jpg
<name>/assets/icon.svg
```

The file name is always `icon`; it is independent of the skill directory name.

Only `icon.png`, `icon.jpg`, and `icon.svg` are served. Icons are for the Settings UI; create or update the file under the skill draft, not under any `/skill-assets` URL or builtin path.

When creating a new SVG icon:

- Use simple geometry that remains readable at small sizes.
- Avoid external images, fonts, scripts, or remote URLs.
- Keep strong contrast between foreground and background.

## Body Writing Principles

- Assume the base model is already capable.
- Write only the details it would miss without the skill: trigger conditions, steps, counterexamples, naming rules, and known pitfalls.
- If the body grows too large, split reference material into `references/*.md` and keep `SKILL.md` as navigation.
- Do not add README, CHANGELOG, install notes, or meta documentation.

## Anti-Patterns

- Do not put app-specific endpoint instructions in a global skill.
- Do not omit `description`; it is the primary trigger signal.
- Do not make the description too broad.
- Do not create extra files that are not directly useful to the skill.
