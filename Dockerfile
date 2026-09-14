# ---- build stage ----
FROM golang:1.27-alpine AS build

WORKDIR /src

# Cache module downloads (only go.mod here since we use stdlib only).
COPY go.mod ./
RUN go mod download

COPY . .
# Static build, no CGO, small binary.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/open-llm ./cmd/open-llm

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/open-llm /open-llm

# Render/most PaaS inject PORT; the app reads it and defaults to 8080.
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/open-llm"]
