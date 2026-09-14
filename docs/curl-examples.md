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

## 6. Validation error (empty messages) — `POST /v1/chat`

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
