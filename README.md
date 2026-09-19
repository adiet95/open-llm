# open-llm

A small HTTP service (**Go 1.27, standard library only**) for building on
**open-source LLMs** via **[Groq](https://groq.com)**'s OpenAI-compatible API.
It grows through an LLM-engineering learning path — each phase is a real,
tested feature:

| Phase | Feature | Endpoint(s) |
|-------|---------|-------------|
| **0** | LLM fundamentals — chat, streaming | `/v1/chat`, `/v1/chat/stream` |
| **1** | Structured output (JSON Schema) | `/v1/extract` |
| **2** | RAG — chunk + embed + retrieve, with citations | `/v1/rag/ingest`, `/v1/rag/query` |
| **3** | Tool-calling agent (ReAct loop) | `/v1/agent` |
| **4** | Production — eval harness + request metrics | `internal/eval`, metrics middleware |

- **LLM provider:** [Groq](https://groq.com) — free tier, fast, OpenAI-compatible
- **Embeddings (RAG):** local [Ollama](https://ollama.com) by default (free), or any OpenAI-compatible embeddings endpoint
- **Deploy:** one-click to [Render](https://render.com) via `render.yaml`

> **Groq, not Grok.** This uses **Groq** (groq.com), an inference provider with a
> free tier — unrelated to xAI's **Grok** model.

---

## Project layout

```
cmd/open-llm/main.go     # entrypoint, graceful shutdown, agent + tools wiring
internal/config/         # env-based configuration (no hard-coded secrets) + .env loader
internal/llm/            # OpenAI-compatible client: Chat, ChatStream, Extract (+ tool types)
internal/rag/            # RAG pipeline: chunk, embed, cosine vector store, service
internal/agent/          # ReAct tool-calling loop with guardrails (allow-list, step limit)
internal/eval/           # evaluation harness: run a labelled dataset, score, report accuracy
internal/httpapi/        # HTTP handlers + metrics middleware
docs/                    # Postman collection + curl examples
Dockerfile               # multi-stage, distroless, static binary
render.yaml              # Render Blueprint (free web service)
.env.example             # every env var, documented
```

---

## Run locally

1. Get a free Groq API key at <https://console.groq.com/keys>.
2. (For RAG only) install embeddings: `ollama pull nomic-embed-text`.
3. Run (PowerShell on Windows):
   ```powershell
   $env:LLM_API_KEY="gsk_..."
   go run ./cmd/open-llm
   ```
   Defaults: base URL `https://api.groq.com/openai/v1`, model
   `openai/gpt-oss-20b`, port `8080`.

Full runnable examples (bash + PowerShell) are in
[`docs/curl-examples.md`](docs/curl-examples.md); a Postman collection is in
[`docs/open-llm.postman_collection.json`](docs/open-llm.postman_collection.json).

Quick check:
```bash
curl -s localhost:8080/v1/chat -d '{"messages":[{"role":"user","content":"Explain what a token is in one sentence."}]}'
```

---

## API

| Method & path | Description |
|---|---|
| `GET /health` | Liveness probe → `{"status":"ok"}` |
| `GET /` | Service info (provider, model, feature flags, endpoints) |
| `POST /v1/chat` | Blocking chat completion → JSON |
| `POST /v1/chat/stream` | Streamed completion (chunked text) |
| `POST /v1/extract` | **Structured output** — text → JSON constrained by a JSON Schema |
| `POST /v1/rag/ingest` | **RAG** — add a document: chunk → embed → store |
| `POST /v1/rag/query` | **RAG** — answer a question grounded in ingested docs, with citations |
| `POST /v1/agent` | **Tool-calling agent** — ReAct loop over `calculator` + `knowledge_lookup` |

> `/v1/extract` and `/v1/agent` need a Groq model that supports structured
> outputs / tool calling — set `LLM_MODEL` if the default rejects them.
> `/v1/rag/*` need embeddings (Ollama by default).

### Examples

**Chat**
```json
POST /v1/chat
{ "messages": [{"role":"user","content":"Hello"}], "temperature": 0.7, "max_tokens": 256 }
→ { "content": "...", "model": "openai/gpt-oss-20b", "usage": {...} }
```

**Extract (Phase 1)** — valid JSON by construction, no regex/repair:
```json
POST /v1/extract
{ "text": "Kirim 500 ribu ke Budi lewat BI-FAST",
  "schema": {"type":"object","properties":{"amount":{"type":"integer"},"recipient":{"type":"string"},"method":{"type":"string"}},"required":["amount","recipient"]} }
→ { "data": {"amount":500000,"recipient":"Budi","method":"BI-FAST"}, "model": ..., "usage": {...} }
```

**RAG (Phase 2)**
```json
POST /v1/rag/ingest
{ "source": "kebijakan", "text": "Limit transfer QRIS harian adalah 20 juta rupiah..." }
→ { "source": "kebijakan", "chunks": 1 }

POST /v1/rag/query
{ "question": "berapa limit transfer QRIS harian?" }
→ { "answer": "... [kebijakan]", "sources": [{"source":"kebijakan","score":0.87,"excerpt":"..."}], "model": ..., "usage": {...} }
```

**Agent (Phase 3)** — the model calls tools and returns the steps:
```json
POST /v1/agent
{ "task": "Berapa 240 dikali 3? Jelaskan singkat." }
→ { "answer": "240 dikali 3 adalah 720.", "steps": [{"tool":"calculator","args":"{\"a\":240,\"b\":3,\"op\":\"*\"}","result":"720"}] }
```

---

## Configuration (environment variables)

For local dev a `.env` file is loaded automatically (git-ignored); real OS env
vars take precedence. In production (Render) values come from the platform.

**LLM (Groq)**
| Var | Default | Notes |
|-----|---------|-------|
| `LLM_API_KEY` | — | **required** — your Groq key (`gsk_...`) |
| `LLM_BASE_URL` | `https://api.groq.com/openai/v1` | must include `/v1` |
| `LLM_MODEL` | `openai/gpt-oss-20b` | any Groq-hosted model; use a tool/JSON-capable one for `/v1/extract` & `/v1/agent` |
| `PORT` | `8080` | injected by Render/most PaaS |
| `LLM_REQUEST_TIMEOUT` | `60` | seconds, or a Go duration like `90s` |

**RAG / embeddings**
| Var | Default | Notes |
|-----|---------|-------|
| `EMBED_BASE_URL` | `http://localhost:11434/v1` | Groq has no embeddings; default is local Ollama |
| `EMBED_API_KEY` | — | empty for local Ollama; set for a cloud embeddings endpoint |
| `EMBED_MODEL` | `nomic-embed-text` | `ollama pull nomic-embed-text` first |
| `VECTOR_STORE_PATH` | `data/vectorstore.json` | JSON-backed store (git-ignored) |
| `RAG_CHUNK_SIZE` | `800` | characters per chunk |
| `RAG_CHUNK_OVERLAP` | `150` | overlap between chunks |
| `RAG_TOP_K` | `4` | chunks retrieved per query |

Secrets are **never** hard-coded — keys are read from the environment only.

---

## Architecture notes

- **Provider abstraction:** generation (Groq) and embeddings (Ollama/cloud) are
  separate OpenAI-compatible clients — swap either via env, no code change.
- **Vector store behind an interface:** `rag.Store` is a JSON-backed cosine index
  today; swap in **pgvector** later without touching the pipeline.
- **Agent guardrails:** only registered tools run (allow-list) and a hard step
  limit (default 5) prevents runaway loops.
- **Eval over exact-assert:** `internal/eval` scores a labelled dataset and
  reports accuracy — the right way to test non-deterministic LLM output.
- **Observability:** `withMetrics` logs method/path/status/latency per request;
  `usage` (token counts) is returned by every completion.

---

## Test & build

```bash
go test ./...   # httptest-based — no real LLM/embeddings call, no key needed
go vet ./...
go build ./...
```

---

## Deploy to Render (free)

1. Push this repo to GitHub.
2. Render → **New → Blueprint** → select this repo (reads `render.yaml`).
3. In **Environment**, set `LLM_API_KEY` (marked `sync: false`, never in git).
   For RAG in the cloud, also set `EMBED_BASE_URL` + `EMBED_API_KEY` to a cloud
   embeddings endpoint (a local Ollama is not reachable from Render).
4. Deploy. Health checks hit `/health`.

> Render's free web service **sleeps when idle** and cold-starts on the next
> request. Fine for a portfolio/demo.

### Other platforms
The `Dockerfile` is portable — same image on **Fly.io** (`fly launch`),
**Google Cloud Run** (`gcloud run deploy --source .`), or **Railway**.

---

## Roadmap (next)

- **pgvector** vector store (Supabase/Postgres) behind the existing `rag.Store`
  interface — for larger corpora.
- **File ingest** (PDF/markdown) for RAG.
- **Guardrails** — prompt-injection detection + PII redaction (Phase 4).
- **Multi-agent** orchestrator (planner/worker/synthesizer) — Phase 5, only when
  a use-case truly needs independent roles.
