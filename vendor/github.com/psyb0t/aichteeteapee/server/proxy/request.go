package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

type RequestPayload struct {
	Method   string              `json:"method"`
	URL      string              `json:"url"`
	Headers  map[string][]string `json:"headers,omitempty"`
	Body     []byte              `json:"body,omitempty"`
	ClientIP string              `json:"clientIp,omitempty"`
	Proto    string              `json:"proto,omitempty"`
}

func (p *RequestPayload) Hash() string {
	h := sha256.New()
	h.Write([]byte(p.Method))
	h.Write([]byte("\n"))
	h.Write([]byte(p.URL))

	if len(p.Headers) > 0 {
		keys := make([]string, 0, len(p.Headers))

		for k := range p.Headers {
			keys = append(keys, k)
		}

		sort.Strings(keys)

		for _, k := range keys {
			h.Write([]byte("\n"))
			h.Write([]byte(k))

			for _, v := range p.Headers[k] {
				h.Write([]byte("\n"))
				h.Write([]byte(v))
			}
		}
	}

	if len(p.Body) > 0 {
		h.Write([]byte("\n"))
		h.Write(p.Body)
	}

	return hex.EncodeToString(h.Sum(nil))
}

type ResponseResult struct {
	StatusCode int                 `json:"statusCode"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       []byte              `json:"body,omitempty"`
}
