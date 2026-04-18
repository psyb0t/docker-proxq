# proxq OpenAI client

Drop-in replacement for [`openai-go`](https://github.com/openai/openai-go). Routes requests through [proxq](https://github.com/psyb0t/proxq) so you never hit upstream timeouts — your reverse proxy (Cloudflare, nginx, etc.) sees instant responses while the actual API call happens asynchronously in the background.

Returns a real `*openai.Client`. Every SDK method works — chat completions, embeddings, images, audio, responses, all of it.

## Before (direct OpenAI)

```go
client := openai.NewClient(
    option.WithAPIKey("sk-..."),
)

resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
    Model:    openai.ChatModelGPT4o,
    Messages: []openai.ChatCompletionMessageParamUnion{
        openai.UserMessage("hello"),
    },
})

fmt.Println(resp.Choices[0].Message.Content)
```

## After (through proxq)

```go
client := proxqopenai.NewClient(proxqopenai.Config{
    ProxqBaseURL: "https://proxq.example.com",
    APIKey:       "sk-...",
})

resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
    Model:    openai.ChatModelGPT4o,
    Messages: []openai.ChatCompletionMessageParamUnion{
        openai.UserMessage("hello"),
    },
})

fmt.Println(resp.Choices[0].Message.Content)
```

Same code. Same types. Same return values. The only change is the client constructor.

## How it works

```
Your code
  |
  |-- client.Chat.Completions.New(...)
  |     |
  |     v
  |   proxqTransport (custom http.RoundTripper)
  |     |
  |     |-- POST to proxq --> 202 {"jobId": "..."}
  |     |-- poll GET /__jobs/{id} until completed
  |     |-- GET /__jobs/{id}/content
  |     |
  |     v
  |   returns upstream response as if it came directly
  |
  v
resp.Choices[0].Message.Content  <-- works exactly like direct call
```

Under the hood, the client injects a custom `http.RoundTripper` into the OpenAI SDK. Every request goes through proxq's async queue. The transport handles enqueue, polling, and content retrieval transparently.

## Streaming

Streaming works out of the box. proxq detects chunked/streaming requests and direct-proxies them to upstream — the SSE stream passes through proxq without going through the job queue. The client sees a normal response (no `X-Proxq-Source` header) and returns it as-is.

```go
client := proxqopenai.NewClient(proxqopenai.Config{
    ProxqBaseURL: "https://proxq.example.com",
    APIKey:       "sk-...",
})

// Non-streaming: goes through proxq queue
resp, _ := client.Chat.Completions.New(ctx, params)

// Streaming: proxq direct-proxies the SSE stream
stream := client.Chat.Completions.NewStreaming(ctx, params)
for stream.Next() {
    chunk := stream.Current()
    fmt.Print(chunk.Choices[0].Delta.Content)
}
```

## Direct proxy passthrough

When proxq direct-proxies a request (via path filter, chunked transfer, large body, or WebSocket), the response comes back without the `X-Proxq-Source` header. The client detects this and returns the response as-is — no polling, no job management.

## Config

```go
type Config struct {
    // Required. proxq base URL.
    ProxqBaseURL string

    // Optional. OpenAI API key.
    // Sent as Authorization header through proxq to upstream.
    APIKey        string

    // Optional. proxq jobs API path. Default: "/__jobs"
    JobsPath      string

    // Optional. Poll interval for job status. Default: 500ms
    PollInterval  time.Duration

    // Optional. HTTP client for all requests.
    // TLS config, timeouts, cookie jar, redirect policy
    // are all preserved.
    HTTPClient    *http.Client
}
```

### Custom TLS / HTTP settings

Your `HTTPClient` settings are fully preserved. The client uses it for all actual HTTP calls (to proxq and to direct upstream). The OpenAI SDK client inherits `Timeout`, `Jar`, and `CheckRedirect` from it.

```go
client := proxqopenai.NewClient(proxqopenai.Config{
    ProxqBaseURL: "https://proxq.internal",
    APIKey:       "sk-...",
    HTTPClient: &http.Client{
        Timeout: 30 * time.Second,
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{
                InsecureSkipVerify: true,
            },
        },
    },
})
```

### Additional SDK options

Pass any `option.RequestOption` after the config:

```go
client := proxqopenai.NewClient(
    proxqopenai.Config{
        ProxqBaseURL: "https://proxq.example.com",
    },
    option.WithAPIKey("sk-..."),
    option.WithOrganization("org-..."),
    option.WithMaxRetries(3),
)
```

## Context cancellation

Polling respects context cancellation. If the context expires or is cancelled while waiting for a job to complete, the client returns immediately with the context error.

```go
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()

resp, err := client.Chat.Completions.New(ctx, params)
if errors.Is(err, context.DeadlineExceeded) {
    // timed out waiting for proxq job
}
```

## Errors

- `ErrEmptyJobID` — proxq accepted the request but returned no job ID
- `ErrJobFailed` — the proxq job failed (network error, upstream timeout, etc.)

Both are sentinel errors, usable with `errors.Is`.
