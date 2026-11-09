# Vendored upstream Trakt contracts

 is the OpenAPI 3 document generated **from Trakt's own
contracts** — no hand transcription, no third party.

- Source: https://github.com/trakt/trakt-api at commit `99aaec81987fed5e598e1843c248b32acbe2099b`
- Generated with that repo's own generator (,
  the module behind its `bun run generate`), run against
  `projects/api/src/contracts/traktContract.ts` — the ts-rest contract tree that
  Trakt publishes as the authority on its API.
- 310 paths / 336 operations, with every request parameter, query schema, request
  body and response schema ( into `components.schemas`).

## Regenerating

```bash
git clone --depth 1 https://github.com/trakt/trakt-api
# pom deps: zod@3, @ts-rest/core, @anatine/zod-openapi, tsx
# copy projects/api/src/contracts + projects/openapi/generateOpenApi.ts into a
# scratch dir, then call generateOpenApi(traktContract, {info, servers})
tsx run.ts          # writes trakt-openapi.json
```

Do not edit the JSON by hand. When Trakt publishes a new contract version, bump the
commit recorded above, regenerate, and let `scripts/parity-reconcile.py` show what
changed.
