# Tasks

- [ ] Add a renderer regression for the Hub-sequenced canonical input → injection update → admitted echo sequence, asserting one final turn input and preserved Agent icon.
- [ ] Add negative regressions for absent, untrusted, and non-Hub-attested canonical provenance.
- [ ] Add a correlation regression proving a different delivery cannot inherit the icon.
- [ ] Exercise history/replay merging with the same event sequence and confirm SSE ordering metadata remains intact.
- [ ] Implement the renderer-only provenance retention at both live and replay merge paths; do not modify Hub code.
