# API docs

- `openapi.yaml` — OpenAPI 3.0.3 spec, source of truth for all endpoints.
- `postman_collection.json` — ready-to-import Postman collection (Postman > Import > file), with `{{base_url}}`, `{{admin_token}}`, `{{tenant_api_key}}` collection variables already wired to bearer auth per folder.

Either file can be imported directly into Postman (Import > File, or Import > Link if hosted). For Admin requests, set `base_url` to `http://localhost:18080` after running:

```bash
kubectl port-forward -n default svc/asset-api 18080:8080
```

For Ops requests, the default `base_url` (`https://assets.kangoprek.my.id`) works as-is.
