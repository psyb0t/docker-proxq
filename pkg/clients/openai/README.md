# proxq OpenAI client

Drop-in replacement for the [`openai-go`](https://github.com/openai/openai-go) client constructor. It returns a real `*openai.Client` whose calls go through [proxq](https://github.com/psyb0t/docker-proxq): each request is queued, polled and returned as if it came straight from the upstream.

```go
import proxqopenai "github.com/psyb0t/docker-proxq/pkg/clients/openai"

client := proxqopenai.NewClient(proxqopenai.Config{
    ProxqBaseURL: "https://proxq.example.com",
    APIKey:       "sk-...",
})
```

The full guide, with the proxq upstream config, streaming behavior, every `Config` field, context cancellation and errors, is on the documentation site: [OpenAI Go client](https://psyb0t.github.io/docker-proxq/openai-client/).
