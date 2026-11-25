# Decision & fix log

One entry per design decision or non-trivial fix. Newest first.
Keep entries short; link to code, tests or design.md sections where useful.

Template:

```
## YYYY-MM-DD — <short title>
- **Context / failure:** what broke, or what would break without this.
- **Decision:** what we chose.
- **Alternatives:** what else we considered, and why not.
- **Test:** the test or measurement that proves it.
```

---

## 2026-11-25 — Split agent instructions from design
- **Context / failure:** a single long AGENTS.md loaded the whole design into every
  session, bloating context.
- **Decision:** AGENTS.md holds working rules + hard invariants only; full design lives
  in docs/design.md; decisions are logged here.
- **Alternatives:** one file per ADR under docs/decisions/ — more overhead than a
  single log for a solo learning project.
- **Test:** n/a.
