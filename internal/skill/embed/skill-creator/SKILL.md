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

System skills are bundled in the binary and appear as `.system/<name>` in the UI. Do not edit or overwrite system skills.

At runtime Pudding injects only the skills index (`name`, `description`, path/source metadata) into the system prompt. The full `SKILL.md` body is loaded on demand with `builtin_skill_read(skill_id="<name>")` when the user's intent matches the description.

## Standard Flow

1. Align on intent: confirm what problem the skill solves and what the user would normally say that should trigger it.
2. Choose a name: use lowercase kebab-case with letters, numbers, and hyphens only.
3. Create the skill directory under `<home>/skills/<name>/`.
4. Write `SKILL.md` with YAML frontmatter plus concise operational instructions.
5. Add `assets/icon.svg` by default unless the user explicitly says not to.
6. Validate manually:
   - `SKILL.md` exists.
   - Frontmatter has `name` and `description`.
   - Directory name matches the frontmatter name.
   - Body contains useful instructions.
7. Tell the user where the skill was written and refresh the Settings skills list.

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

The backend exposes discovered icons through:

```text
/skill-assets/<name>/assets/icon.<ext>
/skill-assets/.system/<name>/assets/icon.<ext>
```

Only `icon.png`, `icon.jpg`, and `icon.svg` are served. Icons are for the Settings UI; the LLM does not need to read `assets/`.

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
