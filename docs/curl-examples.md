# open-llm — curl examples

The service reads its Groq API key **server-side** from the environment, so none
of these requests send an auth header. Start the service first:

```powershell
go run ./cmd/open-llm
```

Set a base URL to reuse below.

- **bash / macOS / Linux / Git Bash:** `BASE=http://localhost:8080`
- **PowerShell (Windows):** `$BASE = "http://localhost:8080"`

When deployed to Render, use your public URL, e.g.
`BASE=https://open-llm-xxxx.onrender.com`.

---

## 1. Service info — `GET /`

**bash**
```bash
curl -s "$BASE/"
```

**PowerShell**
```powershell
curl.exe -s "$BASE/"
```

---

## 2. Health check — `GET /health`

**bash**
```bash
curl -s "$BASE/health"
```

**PowerShell**
```powershell
curl.exe -s "$BASE/health"
```

Expected: `{"status":"ok"}`

---

## 3. Chat (blocking) — `POST /v1/chat`

**bash**
```bash
curl -s "$BASE/v1/chat" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "system", "content": "You are concise."},
      {"role": "user", "content": "Explain what a token is in one sentence."}
    ],
    "temperature": 0.7,
    "max_tokens": 256
  }'
```

**PowerShell** — `curl.exe` on Windows dislikes multi-line single quotes, so pass
the JSON as one line (escape the inner quotes with `\"`):
```powershell
curl.exe -s "$BASE/v1/chat" -H "Content-Type: application/json" -d "{\"messages\":[{\"role\":\"system\",\"content\":\"You are concise.\"},{\"role\":\"user\",\"content\":\"Explain what a token is in one sentence.\"}],\"temperature\":0.7,\"max_tokens\":256}"
```

**PowerShell (cleaner, native)** — use `Invoke-RestMethod`:
```powershell
$body = @{
  messages = @(
    @{ role = "system"; content = "You are concise." },
    @{ role = "user";   content = "Explain what a token is in one sentence." }
  )
  temperature = 0.7
  max_tokens  = 256
} | ConvertTo-Json -Depth 5

Invoke-RestMethod -Method Post -Uri "$BASE/v1/chat" -ContentType "application/json" -Body $body
```

Expected response shape:
```json
{
  "content": "...",
  "model": "openai/gpt-oss-20b",
  "usage": { "prompt_tokens": 12, "completion_tokens": 8, "total_tokens": 20 }
}
```

---

## 4. Chat with a per-request model override

**bash**
```bash
curl -s "$BASE/v1/chat" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [{"role": "user", "content": "Give me three uses for Go'\''s context package."}],
    "model": "openai/gpt-oss-120b",
    "temperature": 0.3
  }'
```

**PowerShell**
```powershell
curl.exe -s "$BASE/v1/chat" -H "Content-Type: application/json" -d "{\"messages\":[{\"role\":\"user\",\"content\":\"Give me three uses for Go's context package.\"}],\"model\":\"openai/gpt-oss-120b\",\"temperature\":0.3}"
```

---

## 5. Chat (streaming) — `POST /v1/chat/stream`

Response is plain text streamed as chunks. Use `-N` (`--no-buffer`) to watch
tokens arrive live.

**bash**
```bash
curl -N "$BASE/v1/chat/stream" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [{"role": "user", "content": "Write a haiku about Go."}]
  }'
```

**PowerShell**
```powershell
curl.exe -N "$BASE/v1/chat/stream" -H "Content-Type: application/json" -d "{\"messages\":[{\"role\":\"user\",\"content\":\"Write a haiku about Go.\"}]}"
```

> Note: `Invoke-RestMethod` buffers the whole response, so for live streaming use
> `curl.exe -N` as shown above.

---

## 6. Extract (structured output) — `POST /v1/extract`

Phase 1. Turns free text into JSON constrained by a JSON Schema, so the result
is valid by construction (no regex/repair). Needs a tool/JSON-capable Groq model.

**bash**
```bash
curl -s "$BASE/v1/extract" \
  -H "Content-Type: application/json" \
  -d '{
    "text": "Kirim 500 ribu ke Budi lewat BI-FAST",
    "schema_name": "transfer",
    "schema": {
      "type": "object",
      "properties": {
        "amount":    { "type": "integer" },
        "recipient": { "type": "string" },
        "method":    { "type": "string" }
      },
      "required": ["amount", "recipient"]
    }
  }'
```

**PowerShell**
```powershell
curl.exe -s "$BASE/v1/extract" -H "Content-Type: application/json" -d "{\"text\":\"Kirim 500 ribu ke Budi lewat BI-FAST\",\"schema_name\":\"transfer\",\"schema\":{\"type\":\"object\",\"properties\":{\"amount\":{\"type\":\"integer\"},\"recipient\":{\"type\":\"string\"},\"method\":{\"type\":\"string\"}},\"required\":[\"amount\",\"recipient\"]}}"
```

Expected: `{ "data": {"amount":500000,"recipient":"Budi","method":"BI-FAST"}, "model": ..., "usage": {...} }`

---

## 7. RAG — ingest a document — `POST /v1/rag/ingest`

Phase 2. Chunks → embeds → stores. Needs embeddings: run
`ollama pull nomic-embed-text` (local, default) or set EMBED_BASE_URL/EMBED_API_KEY.

**bash**
```bash
curl -s "$BASE/v1/rag/ingest" \
  -H "Content-Type: application/json" \
  -d '{
    "source": "kebijakan-pembayaran",
    "text": "Limit transfer QRIS harian adalah 20 juta rupiah per pengguna. Biaya admin gratis untuk merchant kategori UMI. Transfer BI-FAST maksimal 250 juta per transaksi."
  }'
```

**PowerShell**
```powershell
curl.exe -s "$BASE/v1/rag/ingest" -H "Content-Type: application/json" -d "{\"source\":\"kebijakan-pembayaran\",\"text\":\"Limit transfer QRIS harian adalah 20 juta rupiah per pengguna. Biaya admin gratis untuk merchant kategori UMI.\"}"
```

Expected: `{ "source": "kebijakan-pembayaran", "chunks": 1 }`

---

## 8. RAG — query (grounded answer) — `POST /v1/rag/query`

Phase 2. Retrieves relevant chunks and answers grounded in them, with citations.

**bash**
```bash
curl -s "$BASE/v1/rag/query" \
  -H "Content-Type: application/json" \
  -d '{"question": "berapa limit transfer QRIS harian?"}'
```

**PowerShell**
```powershell
curl.exe -s "$BASE/v1/rag/query" -H "Content-Type: application/json" -d "{\"question\":\"berapa limit transfer QRIS harian?\"}"
```

Expected shape:
```json
{
  "answer": "Limit transfer QRIS harian adalah 20 juta rupiah per pengguna [kebijakan-pembayaran].",
  "sources": [{ "source": "kebijakan-pembayaran", "score": 0.87, "excerpt": "Limit transfer QRIS harian..." }],
  "model": "openai/gpt-oss-20b",
  "usage": { "prompt_tokens": 120, "completion_tokens": 24, "total_tokens": 144 }
}
```

> Tip: ask something NOT in the ingested docs — the answer should say it doesn't
> know (anti-hallucination).

---

## 9. Agent (tool-calling) — `POST /v1/agent`

Phase 3. ReAct loop; the model may call `calculator` and (if RAG enabled)
`knowledge_lookup`. Needs a tool-calling-capable Groq model.

**bash**
```bash
curl -s "$BASE/v1/agent" \
  -H "Content-Type: application/json" \
  -d '{"task": "Berapa 240 dikali 3? Jelaskan singkat hasilnya."}'
```

**PowerShell**
```powershell
curl.exe -s "$BASE/v1/agent" -H "Content-Type: application/json" -d "{\"task\":\"Berapa 240 dikali 3? Jelaskan singkat hasilnya.\"}"
```

Expected shape:
```json
{
  "answer": "240 dikali 3 adalah 720.",
  "steps": [{ "tool": "calculator", "args": "{\"a\":240,\"b\":3,\"op\":\"*\"}", "result": "720" }]
}
```

**Multi-tool example** (ingest the payment-policy doc first, step 7): the agent
uses `knowledge_lookup` then `calculator`:
```bash
curl -s "$BASE/v1/agent" \
  -H "Content-Type: application/json" \
  -d '{"task": "Cek dari knowledge base berapa limit QRIS harian, lalu hitung totalnya untuk 3 hari."}'
```

---

## 10. Validation error (empty messages) — `POST /v1/chat`

Shows the 400 path.

**bash**
```bash
curl -s -i "$BASE/v1/chat" \
  -H "Content-Type: application/json" \
  -d '{"messages": []}'
```

**PowerShell**
```powershell
curl.exe -s -i "$BASE/v1/chat" -H "Content-Type: application/json" -d "{\"messages\":[]}"
```

Expected: HTTP 400 with `{"error":"messages must not be empty"}`.
