package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/psyb0t/ctxerrors"
	proxqtypes "github.com/psyb0t/proxq/pkg/types"
)

const (
	defaultJobsPath     = "/__jobs"
	defaultPollInterval = 500 * time.Millisecond
)

type Config struct {
	ProxqBaseURL string
	APIKey       string
	JobsPath     string
	PollInterval time.Duration
	HTTPClient   *http.Client
}

func NewClient(
	cfg Config,
	opts ...option.RequestOption,
) *openai.Client {
	transport := buildTransport(cfg)

	sdkClient := cloneClientWithTransport(
		cfg.HTTPClient, transport,
	)

	allOpts := []option.RequestOption{
		option.WithBaseURL(cfg.ProxqBaseURL),
		option.WithHTTPClient(sdkClient),
	}

	if cfg.APIKey != "" {
		allOpts = append(
			allOpts,
			option.WithAPIKey(cfg.APIKey),
		)
	}

	allOpts = append(allOpts, opts...)

	client := openai.NewClient(allOpts...)

	return &client
}

func cloneClientWithTransport(
	base *http.Client,
	rt http.RoundTripper,
) *http.Client {
	if base == nil {
		return &http.Client{Transport: rt}
	}

	return &http.Client{
		Transport:     rt,
		CheckRedirect: base.CheckRedirect,
		Jar:           base.Jar,
		Timeout:       base.Timeout,
	}
}

func buildTransport(cfg Config) *proxqTransport {
	jobsPath := cfg.JobsPath
	if jobsPath == "" {
		jobsPath = defaultJobsPath
	}

	pollInterval := cfg.PollInterval
	if pollInterval == 0 {
		pollInterval = defaultPollInterval
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	jobsBaseURL, err := url.Parse(
		cfg.ProxqBaseURL + jobsPath,
	)
	if err != nil {
		panic("invalid proxq base URL: " + err.Error())
	}

	return &proxqTransport{
		jobsBaseURL:  jobsBaseURL,
		pollInterval: pollInterval,
		httpClient:   httpClient,
	}
}

type jobAccepted struct {
	JobID string `json:"jobId"`
}

type jobStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type proxqTransport struct {
	jobsBaseURL  *url.URL
	pollInterval time.Duration
	httpClient   *http.Client
}

func (t *proxqTransport) RoundTrip(
	req *http.Request,
) (*http.Response, error) {
	resp, err := t.httpClient.Do(req) //nolint:gosec
	if err != nil {
		return nil, ctxerrors.Wrap(
			err, "send request to proxq",
		)
	}

	if resp.Header.Get(
		proxqtypes.HeaderNameXProxqSource,
	) == "" {
		return resp, nil
	}

	jobID, err := t.parseJobID(resp)
	if err != nil {
		return nil, ctxerrors.Wrap(
			err, "parse proxq job response",
		)
	}

	if err := t.waitDone(
		req.Context(), jobID,
	); err != nil {
		return nil, ctxerrors.Wrap(
			err, "wait for proxq job: "+jobID,
		)
	}

	contentResp, err := t.fetchContent(
		req.Context(), jobID,
	)
	if err != nil {
		return nil, ctxerrors.Wrap(
			err, "fetch proxq job content: "+jobID,
		)
	}

	return contentResp, nil
}

func (t *proxqTransport) parseJobID(
	resp *http.Response,
) (string, error) {
	body, err := io.ReadAll(resp.Body)

	closeErr := resp.Body.Close()

	if err != nil {
		return "", ctxerrors.Wrap(
			err, "read proxq response",
		)
	}

	if closeErr != nil {
		return "", ctxerrors.Wrap(
			closeErr, "close proxq response body",
		)
	}

	var accepted jobAccepted
	if err := json.Unmarshal(
		body, &accepted,
	); err != nil {
		return "", ctxerrors.Wrap(
			err, "unmarshal job response",
		)
	}

	if accepted.JobID == "" {
		return "", ErrEmptyJobID
	}

	return accepted.JobID, nil
}

func (t *proxqTransport) waitDone(
	ctx context.Context,
	jobID string,
) error {
	statusURL := t.jobsBaseURL.JoinPath(jobID).String()

	for {
		select {
		case <-ctx.Done():
			return ctxerrors.Wrap(
				ctx.Err(), "poll cancelled",
			)
		case <-time.After(t.pollInterval):
		}

		done, err := t.pollOnce(ctx, statusURL, jobID)
		if err != nil {
			return err
		}

		if done {
			return nil
		}
	}
}

func (t *proxqTransport) pollOnce(
	ctx context.Context,
	statusURL string,
	jobID string,
) (bool, error) {
	req, err := http.NewRequestWithContext( //nolint:gosec
		ctx, http.MethodGet, statusURL, nil,
	)
	if err != nil {
		return false, ctxerrors.Wrap(
			err, "build status request",
		)
	}

	resp, err := t.httpClient.Do(req) //nolint:gosec
	if err != nil {
		return false, ctxerrors.Wrap(
			err, "poll job status",
		)
	}

	body, err := io.ReadAll(resp.Body)

	if closeErr := resp.Body.Close(); closeErr != nil {
		return false, ctxerrors.Wrap(
			closeErr, "close status response body",
		)
	}

	if err != nil {
		return false, ctxerrors.Wrap(
			err, "read status response",
		)
	}

	var status jobStatus
	if err := json.Unmarshal(
		body, &status,
	); err != nil {
		return false, ctxerrors.Wrap(
			err, "unmarshal job status",
		)
	}

	switch status.Status {
	case "completed":
		return true, nil
	case "failed":
		return false, ctxerrors.Wrap(
			ErrJobFailed, jobID,
		)
	}

	return false, nil
}

func (t *proxqTransport) fetchContent(
	ctx context.Context,
	jobID string,
) (*http.Response, error) {
	contentURL := t.jobsBaseURL.JoinPath(
		jobID, "content",
	).String()

	req, err := http.NewRequestWithContext( //nolint:gosec
		ctx, http.MethodGet, contentURL, nil,
	)
	if err != nil {
		return nil, ctxerrors.Wrap(
			err, "build content request",
		)
	}

	resp, err := t.httpClient.Do(req) //nolint:gosec
	if err != nil {
		return nil, ctxerrors.Wrap(
			err, "fetch job content",
		)
	}

	body, err := io.ReadAll(resp.Body)

	if closeErr := resp.Body.Close(); closeErr != nil {
		return nil, ctxerrors.Wrap(
			closeErr, "close content response body",
		)
	}

	if err != nil {
		return nil, ctxerrors.Wrap(
			err, "read content response",
		)
	}

	return &http.Response{
		Status:     resp.Status,
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body: io.NopCloser(
			bytes.NewReader(body),
		),
		ContentLength: int64(len(body)),
		Request:       req,
	}, nil
}
