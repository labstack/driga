# Drafts

New screens and widgets awaiting the owner's choice (Canon design.md,
"Promotable drafts"). Each draft is product code: typed props in,
callbacks out, fixtures beside it in `<name>-fixtures.ts`, stories in
`<name>.stories.tsx` under `Drafts/`. App code never imports from here; a
catalog test enforces it. A chosen draft moves into `host/` or `panels/` and
its story keeps its id; the rest are deleted.
