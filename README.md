# open-llm

A small HTTP service (Go 1.27, standard library only) that talks to an
**open-source LLM** on **[Groq](https://groq.com)** through Groq's
**OpenAI-compatible** chat-completions API.

Built as **Phase 0** of an LLM-engineering learning path: it makes the
fundamentals concrete — how a chat API call is shaped (messages, model,
temperature, max_tokens), the difference between a blocking response and a
streamed one, and how to run the same service locally and in the cloud by
changing environment variables only.

- **LLM provider:** [Groq](https://groq.com) — free tier, fast, OpenAI-compatible
- **Deploy:** one-click to [Render](https://render.com) with the included `render.yaml`

Reference for the API shape:
[OpenAI — Text generation & prompting](https://platform.openai.com/docs/guides/text-generation).

> **Groq, not Grok.** This service uses **Groq** (groq.com), an inference
> provider with a free tier. It is unrelated to xAI's **Grok** model.

---

## Project layout

```
cmd/open-llm/main.go        # entrypoint + graceful shutdown
internal/config/            # env-based configuration (no hard-coded secrets)
internal/llm/               # OpenAI-compatible client: Chat + ChatStream (+ tests)
internal/httpapi/           # HTTP handlers: /health, /v1/chat, /v1/chat/stream
Dockerfile                  # multi-stage, distroless, static binary
render.yaml                 # Render Blueprint (free web service)
.env.example                # every env var, documented
```

---

## Run locally

1. Get a free Groq API key at <https://console.groq.com/keys>.
2. Run the service (PowerShell on Windows):
   ```powershell
   $env:LLM_API_KEY="gsk_..."
   go run ./cmd/open-llm
   ```
   Defaults: base URL `https://api.groq.com/openai/v1`, model
   `openai/gpt-oss-20b`, port `8080`.

3. Call it:
   ```bash
   curl -s localhost:8080/v1/chat -d '{
     "messages": [{"role": "user", "content": "Explain what a token is in one sentence."}]
   }'
   ```
   Streaming (watch tokens arrive):
   ```bash
   curl -N localhost:8080/v1/chat/stream -d '{
     "messages": [{"role": "user", "content": "Write a haiku about Go."}]
   }'
   ```

---

## API

| Method & path        | Description                              |
|----------------------|------------------------------------------|
| `GET /health`        | Liveness probe → `{"status":"ok"}`       |
| `GET /`              | Service info (provider, model, endpoints)|
| `POST /v1/chat`      | Blocking chat completion → JSON          |
| `POST /v1/chat/stream` | Streamed completion (chunked text)     |

**Request body** (both chat endpoints):
```json
{
  "messages": [
    {"role": "system", "content": "You are concise."},
    {"role": "user", "content": "Hello"}
  ],
  "model": "optional-override",
  "temperature": 0.7,
  "max_tokens": 256
}
```

**`/v1/chat` response:**
```json
{ "content": "...", "model": "openai/gpt-oss-20b",
  "usage": {"prompt_tokens": 12, "completion_tokens": 8, "total_tokens": 20} }
```

---

## Configuration (environment variables)

| Var | Default | Notes |
|-----|---------|-------|
| `LLM_API_KEY` | — | **required** — your Groq key (`gsk_...`) |
| `LLM_BASE_URL` | `https://api.groq.com/openai/v1` | must include the `/v1` segment |
| `LLM_MODEL` | `openai/gpt-oss-20b` | any Groq-hosted model (see console.groq.com/docs/models) |
| `PORT` | `8080` | injected by Render/most PaaS |
| `LLM_REQUEST_TIMEOUT` | `60` | seconds, or a Go duration like `90s` |

Secrets are **never** hard-coded — the key is read from the environment only.

---

## Test & build

```bash
go test ./...        # unit tests (httptest — no real LLM call, no key needed)
go vet ./...
go build ./...
```

---

## Deploy to Render (free)

1. Push this repo to GitHub.
2. Render dashboard → **New → Blueprint** → select this repo. It reads `render.yaml`.
3. In the service's **Environment** tab, set `LLM_API_KEY` to your Groq key
   (it's marked `sync: false`, so it is never stored in git).
4. Deploy. Render builds the Dockerfile and gives you a public HTTPS URL.
   Health checks hit `/health`.

> Note: Render's free web service **sleeps when idle** and cold-starts on the
> next request (a few seconds). Fine for a portfolio/demo.

### Other platforms
The `Dockerfile` is portable, so the same image runs on **Fly.io**
(`fly launch`), **Google Cloud Run** (`gcloud run deploy --source .`), or
**Railway**. Set the same env vars there.

---

## Roadmap (next learning phases)

- **Phase 1:** add a `/v1/extract` endpoint using **function calling / structured
  outputs** (JSON schema) instead of free-text parsing.
- **Phase 2:** add embeddings + `pgvector` for RAG.
- **Phase 4:** add token/latency metrics, request logging, and an eval harness.
