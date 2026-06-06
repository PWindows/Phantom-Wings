package remote

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/apex/log"
	"github.com/cenkalti/backoff/v4"
	"github.com/goccy/go-json"

	"github.com/pwindows/phantom-wings/internal/models"
	"github.com/pwindows/phantom-wings/system"
)

type Client interface {
	GetBackupRemoteUploadURLs(ctx context.Context, backup string, size int64) (BackupRemoteUploadResponse, error)
	GetInstallationScript(ctx context.Context, uuid string) (InstallationScript, error)
	GetServerConfiguration(ctx context.Context, uuid string) (ServerConfigurationResponse, error)
	GetServers(context context.Context, perPage int) ([]RawServerData, error)
	ResetServersState(ctx context.Context) error
	SetArchiveStatus(ctx context.Context, uuid string, successful bool) error
	SetBackupStatus(ctx context.Context, backup string, data BackupRequest) error
	SendRestorationStatus(ctx context.Context, backup string, successful bool) error
	SetInstallationStatus(ctx context.Context, uuid string, data InstallStatusRequest) error
	SetTransferStatus(ctx context.Context, uuid string, successful bool) error
	ValidateSftpCredentials(ctx context.Context, request SftpAuthRequest) (SftpAuthResponse, error)
	SendActivityLogs(ctx context.Context, activity []models.Activity) error
	PushServerStateChange(ctx context.Context, sid string, stateChange ServerStateChange) error
}

type client struct {
	httpClient    *http.Client
	baseUrl       string
	tokenId       string
	token         string
	maxAttempts   int
	customHeaders map[string]string
}

func New(base string, opts ...ClientOption) Client {
	c := client{
		baseUrl: strings.TrimSuffix(base, "/") + "/api/remote",
		httpClient: &http.Client{
			Timeout: time.Second * 15,
		},
		maxAttempts: 0,
	}
	for _, opt := range opts {
		opt(&c)
	}
	return &c
}

func WithCredentials(id, token string) ClientOption {
	return func(c *client) {
		c.tokenId = id
		c.token = token
	}
}

func WithCustomHeaders(headers map[string]string) ClientOption {
	return func(c *client) {
		c.customHeaders = headers
	}
}

func WithHttpClient(httpClient *http.Client) ClientOption {
	return func(c *client) {
		c.httpClient = httpClient
	}
}

func (c *client) Get(ctx context.Context, path string, query q) (*Response, error) {
	return c.request(ctx, http.MethodGet, path, nil, func(r *http.Request) {
		q := r.URL.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		r.URL.RawQuery = q.Encode()
	})
}

func (c *client) Post(ctx context.Context, path string, data interface{}) (*Response, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return c.request(ctx, http.MethodPost, path, bytes.NewBuffer(b))
}

func (c *client) requestOnce(ctx context.Context, method, path string, body io.Reader, opts ...func(r *http.Request)) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseUrl+path, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", fmt.Sprintf("Phantom Wings/v%s (id:%s)", system.Version, c.tokenId))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s.%s", c.tokenId, c.token))

	criticalHeaders := map[string]bool{
		"Authorization": true,
		"User-Agent":    true,
		"Accept":        true,
		"Content-Type":  true,
	}
	for key, value := range c.customHeaders {
		if !criticalHeaders[key] {
			req.Header.Set(key, value)
		}
	}

	for _, o := range opts {
		o(req)
	}

	debugLogRequest(req)

	res, err := c.httpClient.Do(req)
	return &Response{res}, err
}

func (c *client) request(ctx context.Context, method, path string, body *bytes.Buffer, opts ...func(r *http.Request)) (*Response, error) {
	var res *Response
	err := backoff.Retry(func() error {
		var b bytes.Buffer
		if body != nil {
			if _, err := b.Write(body.Bytes()); err != nil {
				return backoff.Permanent(errors.Wrap(err, "http: failed to copy body buffer"))
			}
		}
		r, err := c.requestOnce(ctx, method, path, &b, opts...)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return backoff.Permanent(err)
			}
			return errors.WrapIf(err, "http: request creation failed")
		}
		res = r
		if r.HasError() {
			defer r.Body.Close()
			if r.StatusCode >= 400 && r.StatusCode < 500 {
				return backoff.Permanent(r.Error())
			}
			return r.Error()
		}
		return nil
	}, c.backoff(ctx))
	if err != nil {
		if v, ok := err.(*backoff.PermanentError); ok {
			return nil, v.Unwrap()
		}
		return nil, err
	}
	return res, nil
}

func (c *client) backoff(ctx context.Context) backoff.BackOffContext {
	b := backoff.NewExponentialBackOff()
	b.MaxInterval = time.Second * 12
	b.MaxElapsedTime = time.Second * 30
	if c.maxAttempts > 0 {
		return backoff.WithContext(backoff.WithMaxRetries(b, uint64(c.maxAttempts)), ctx)
	}
	return backoff.WithContext(b, ctx)
}

type Response struct {
	*http.Response
}

func (r *Response) HasError() bool {
	if r.Response == nil {
		return false
	}
	return r.StatusCode >= 300 || r.StatusCode < 200
}

func (r *Response) Read() ([]byte, error) {
	var b []byte
	if r.Response == nil {
		return nil, errors.New("remote: attempting to read missing response")
	}
	if r.Response.Body != nil {
		b, _ = io.ReadAll(r.Response.Body)
	}
	r.Response.Body = io.NopCloser(bytes.NewBuffer(b))
	return b, nil
}

func (r *Response) BindJSON(v interface{}) error {
	b, err := r.Read()
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return errors.Wrap(err, "remote: could not unmarshal response")
	}
	return nil
}

func (r *Response) Error() error {
	if !r.HasError() {
		return nil
	}

	var errs RequestErrors
	_ = r.BindJSON(&errs)

	e := &RequestError{
		Code:   "_MissingResponseCode",
		Status: strconv.Itoa(r.StatusCode),
		Detail: "No error response returned from API endpoint.",
	}
	if len(errs.Errors) > 0 {
		e = &errs.Errors[0]
	}

	e.response = r.Response
	return errors.WithStackDepth(e, 1)
}

func debugLogRequest(req *http.Request) {
	if l, ok := log.Log.(*log.Logger); ok && l.Level != log.DebugLevel {
		return
	}
	headers := make(map[string][]string)
	for k, v := range req.Header {
		if k != "Authorization" || len(v) == 0 || len(v[0]) == 0 {
			headers[k] = v
			continue
		}
		headers[k] = []string{"(redacted)"}
	}

	log.WithFields(log.Fields{
		"method":   req.Method,
		"endpoint": req.URL.String(),
		"headers":  headers,
	}).Debug("making request to external HTTP endpoint")
}