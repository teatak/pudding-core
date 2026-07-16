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

When the user explicitly asks to create or update a Skill, write it directly to this directory. There is no draft, submit, or publish step. A user Skill becomes discoverable as soon as it is valid.

Builtin skills are bundled in the binary and appear as `builtin/<name>` in the UI. Do not edit or overwrite builtin skills.

At runtime Pudding injects only the skills index (`name`, `description`, path/source metadata) into the system prompt. The full `SKILL.md` body is loaded on demand with `builtin_skill_read(skill_id="<name>")` when the user's intent matches the description.

## Standard Flow

1. Align on intent: confirm what problem the skill solves and what the user would normally say that should trigger it.
2. Choose a name: use lowercase kebab-case with letters, numbers, and hyphens only.
3. Ensure Code capability, then load `code.files-write` and `code.skill`; load `code.files-read` when inspection is needed.
4. Inspect the existing Skill under `scope="skill"` when updating one.
5. Write `SKILL.md` with YAML frontmatter plus concise operational instructions.
6. Add `assets/icon.svg` by default unless the user explicitly says not to.
7. Call `builtin_skill_validate` with `skill_id`.
8. Fix validation errors in place and validate again.
9. Tell the user the Skill is ready and available in Settings > Knowledge.

## Available Tools

- Use `builtin_file_list` and `builtin_file_read` with `scope="skill"` to inspect global user Skills; pass `path="."` to list the root.
- Use `builtin_file_write` with `scope="skill"` to create files.
- Use `builtin_file_patch` with `scope="skill"` for precise updates that preserve unrelated files.
- Use `builtin_file_delete`, `builtin_file_move`, and `builtin_file_copy` with `scope="skill"` only when the requested change requires them.
- Load the `code.skill` toolkit and use `builtin_skill_validate(skill_id="<name>")` after writing.

Do not delete an entire Skill or unrelated assets unless the user explicitly asks. Builtin Skills are outside the writable `skill` scope.

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

Only `icon.png`, `icon.jpg`, and `icon.svg` are served. Icons are for the Settings UI; create or update the file under the user Skill directory, not under any `/skill-assets` URL or builtin path.

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
