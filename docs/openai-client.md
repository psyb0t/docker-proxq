---
title: "OpenAI Go client"
description: "A drop-in openai-go client that sends every SDK call through proxq: enqueue, poll and return the upstream response as a normal SDK result."
nav_order: 6
---

# OpenAI Go client

[`pkg/clients/openai`](https://github.com/psyb0t/docker-proxq/tree/main/pkg/clients/openai) wraps the official [`openai-go`](https://github.com/openai/openai-go) SDK so its calls go through proxq. Your reverse proxy (Cloudflare, nginx and the like) sees fast answers, and the slow model call happens in a proxq job.

It returns a real `*openai.Client`. Every SDK method works the same way: chat completions, embeddings, images, audio, responses. Only the constructor changes.

```bash
go get github.com/psyb0t/docker-proxq/pkg/clients/openai
```

## Before and after

Direct to OpenAI:

```go
client := openai.NewClient(
    option.WithAPIKey("sk-..."),
)
```

Through proxq:

```go
import proxqopenai "github.com/psyb0t/docker-proxq/pkg/clients/openai"

client := proxqopenai.NewClient(proxqopenai.Config{
    ProxqBaseURL: "https://proxq.example.com",
    APIKey:       "sk-...",
})

resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
    Model: openai.ChatModelGPT4o,
    Messages: []openai.ChatCompletionMessageParamUnion{
        openai.UserMessage("hello"),
    },
})
if err != nil {
    return err
}

fmt.Println(resp.Choices[0].Message.Content)
```

`ProxqBaseURL` becomes the SDK's base URL, so proxq needs an upstream that points at the OpenAI API. With the default SDK paths (`/chat/completions` and the rest), a single catch-all upstream does it:

```yaml
upstreams:
  - prefix: "/"
    url: "https://api.openai.com/v1"
    timeout: "10m"
```

## How it works

```
client.Chat.Completions.New(...)
  |
  v
proxqTransport (an http.RoundTripper)
  |-- POST to proxq --------------> 202 {"jobId": "..."}
  |-- GET /__jobs/{id}, repeated until "completed"
  |-- GET /__jobs/{id}/content
  v
the upstream response, handed back to the SDK
```

The client puts a custom `http.RoundTripper` inside the SDK's HTTP client. For each request it:

1. sends the request to proxq;
2. returns the response as-is when it has no `X-Proxq-Source` header, because then it came straight from the upstream through a [direct proxy](routing.md#direct-proxy-bypass);
3. otherwise reads the job ID, polls `GET {JobsPath}/{id}` every `PollInterval` until the status is `completed` or `failed`;
4. fetches `GET {JobsPath}/{id}/content` and returns it to the SDK.

## Streaming

`NewStreaming` calls are queued like every other request. The SDK sends them with a `Content-Length`, so none of the [bypass rules](routing.md#direct-proxy-bypass) fire. The job stores the whole server-sent event stream, and the client hands it to the SDK once the job completes. The stream iterator works, but every chunk arrives at once at the end.

```go
stream := client.Chat.Completions.NewStreaming(ctx, params)
for stream.Next() {
    chunk := stream.Current()
    fmt.Print(chunk.Choices[0].Delta.Content)
}
```

For live streaming, add a `pathFilter` that sends the streaming endpoint direct. proxq matches paths, not bodies, so that sends every request on that path direct, streaming or not.

## Config

```go
type Config struct {
    // Required. proxq base URL, also used as the SDK base URL.
    ProxqBaseURL string

    // Optional. OpenAI API key, sent as the Authorization header
    // through proxq to the upstream.
    APIKey string

    // Optional. proxq jobs API path. Default: "/__jobs".
    JobsPath string

    // Optional. Delay between status polls. Default: 500ms.
    PollInterval time.Duration

    // Optional. Sent as X-Proxq-Timeout on every request when set,
    // overriding the upstream timeout for these jobs.
    Timeout time.Duration

    // Optional. HTTP client for every call to proxq.
    HTTPClient *http.Client
}
```

`NewClient` panics when `ProxqBaseURL` plus `JobsPath` does not parse as a URL.

### Custom TLS and HTTP settings

The client sends every request, polls included, through your `HTTPClient`, so its transport and TLS settings apply. The SDK's own client copies `Timeout`, `Jar` and `CheckRedirect` from it. Note that `Timeout` then covers the whole call, polling included.

```go
client := proxqopenai.NewClient(proxqopenai.Config{
    ProxqBaseURL: "https://proxq.internal",
    APIKey:       "sk-...",
    HTTPClient: &http.Client{
        Timeout: 5 * time.Minute,
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{
                RootCAs: internalCAs, // your *x509.CertPool
            },
        },
    },
})
```

### More SDK options

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

Polling stops when the request context is cancelled or times out, and the call returns the context error. The proxq job keeps running; the client does not cancel it.

```go
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()

resp, err := client.Chat.Completions.New(ctx, params)
if errors.Is(err, context.DeadlineExceeded) {
    // gave up waiting for the proxq job
}
```

## Errors

| Error | Meaning |
| --- | --- |
| `ErrEmptyJobID` | proxq answered with `X-Proxq-Source` but without a job ID |
| `ErrJobFailed` | The job status became `failed` |

Both are sentinel errors for `errors.Is`. A proxq error answer, such as `502` for an unmatched path or `500` when Redis is down, also carries `X-Proxq-Source`, so it surfaces as `ErrEmptyJobID` rather than as an HTTP error from the SDK.
