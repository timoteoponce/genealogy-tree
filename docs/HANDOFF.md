# Handoff guide (for continuing development, possibly with a less capable AI model)

This project is deliberately boring-by-design so that a weaker model (or a
tired human) can extend it safely. Follow these rules in every task.

## Ground rules for the implementing agent

1. Run `mise install && mise trust` first; use `mise check` after every task —
   it runs gofmt + `go vet` + all tests. Do not commit red.
2. Only modify the files a task explicitly lists. No refactors "while you're
   in there". No new dependencies (ask first; the dep list is deliberate).
3. Functions ≤ ~80 lines; one concern per file; match the existing style
   (qlen comments at top of every file explaining the package's shape).
4. Always use the existing helpers: `store.NewQueries`, `web.qOf`,
   `web.baseData`, `auth.Wrap` — don't invent parallel paths.
5. Every endpoint change: extend the httptest flow in that package
   (`*_test.go`) with the acceptance assertions listed in the task.
6. If a test fails: fix the CODE, not the test (tests encode the task's
   acceptance criteria). If you believe the test is wrong, stop and ask.

## Task recipe (any feature)

```text
Task: <one sentence>

Files allowed: <list>
Steps:
  1. <precise step, referencing existing helpers>
  2. ...
Acceptance:
  - `mise check` passes
  - <behavioral assertions, e.g. "viewer POST /x → 403">
```

One task = one commit, message prefix "M<number>: ..." following
`git log` history style (M0–M9 so far).

## Extension points already prepared

- **GEDCOM export (M10)**: the store layer has everything; re-encode via
  `elliotchance/gedcom` (its encoder exists) — write a `cmd` subcommand or
  an admin GET that streams a GEDCOM document. Careful: reconstruct names
  (`NAME given /surname/`), events, FAMs, OBJE placeholders.
- **Living-people privacy hardening** (if the deployment ever becomes public
  with narrowed viewer rights): the groundwork (`people.living` stored,
  modeled in `models.IsLiving`) is in place — add a viewer-level filter in
  `web.render`, not in SQL, to keep the graph queries simple.
- **Localization**: templates are plain English strings; retrofit with a
  translation map in `web` if needed (accept the extra complexity only on
  request).
- **More tree views** (fan charts, hourglass): /tree.json already services a
  fully compatible node set; new renderers can reuse it.

## Known limitations (accepted for v1)

- ANSEL exotic letters beyond common Latin may degrade to '?' (warnings
  visible in the import report).
- Photo EXIF orientation is not auto-rotated.
- One tree per deployment (import = replace). Merge/sync between two
  MyHeritage exports is intentionally out of scope.
- No password self-service (admins set passwords).
- Login CSRF is not enforced (no session exists yet on that endpoint);
  credentials-only risk accepted and documented in `internal/auth`.
