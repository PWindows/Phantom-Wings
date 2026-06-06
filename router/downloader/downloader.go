package downloader

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/goccy/go-json"
	"github.com/google/uuid"

	"github.com/pwindows/phantom-wings/config"
	"github.com/pwindows/phantom-wings/server"
)

var client *http.Client

func init() {
	dialer := &net.Dialer{
		LocalAddr: nil,
	}

	trnspt := http.DefaultTransport.(*http.Transport).Clone()
	trnspt.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		ipStr, _, err := net.SplitHostPort(c.RemoteAddr().String())
		if err != nil {
			return c, errors.WithStack(err)
		}
		ip := net.ParseIP(ipStr)
		if ip == nil {
			return c, errors.WithStack(ErrInvalidIPAddress)
		}
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
			return c, errors.WithStack(ErrInternalResolution)
		}
		for _, block := range internalRanges {
			if !block.Contains(ip) {
				continue
			}
			return c, errors.WithStack(ErrInternalResolution)
		}
		return c, nil
	}

	client = &http.Client{
		Timeout:   time.Hour * 12,
		Transport: trnspt,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

var instance = &Downloader{
	downloadCache: make(map[string]*Download),
	serverCache:   make(map[string][]string),
}

var internalRanges = []*net.IPNet{
	mustParseCIDR("127.0.0.1/8"),
	mustParseCIDR("10.0.0.0/8"),
	mustParseCIDR("172.16.0.0/12"),
	mustParseCIDR("192.168.0.0/16"),
	mustParseCIDR("169.254.0.0/16"),
	mustParseCIDR("::1/128"),
	mustParseCIDR("fe80::/10"),
	mustParseCIDR("fc00::/7"),
}

const (
	ErrInternalResolution = errors.Sentinel("downloader: destination resolves to internal network location")
	ErrInvalidIPAddress   = errors.Sentinel("downloader: invalid IP address")
	ErrDownloadFailed     = errors.Sentinel("downloader: download request failed")
)

const defaultMaxRedirects = 10

type Counter struct {
	total   int
	onWrite func(total int)
}

func (c *Counter) Write(p []byte) (int, error) {
	n := len(p)
	c.total += n
	c.onWrite(c.total)
	return n, nil
}

type DownloadRequest struct {
	Directory string
	URL       *url.URL
	FileName  string
	UseHeader bool
}

type Download struct {
	Identifier string
	path       string
	mu         sync.RWMutex
	req        DownloadRequest
	server     *server.Server
	progress   float64
	cancelFunc *context.CancelFunc
}

func New(s *server.Server, r DownloadRequest) *Download {
	dl := Download{
		Identifier: uuid.Must(uuid.NewRandom()).String(),
		req:        r,
		server:     s,
	}
	instance.track(&dl)
	return &dl
}

func ByServer(sid string) []*Download {
	instance.mu.Lock()
	defer instance.mu.Unlock()
	var downloads []*Download
	if v, ok := instance.serverCache[sid]; ok {
		for _, id := range v {
			if dl, ok := instance.downloadCache[id]; ok {
				downloads = append(downloads, dl)
			}
		}
	}
	return downloads
}

func ByID(dlid string) *Download {
	return instance.find(dlid)
}

//goland:noinspection GoVetCopyLock
func (dl Download) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Identifier string
		Progress   float64
	}{
		Identifier: dl.Identifier,
		Progress:   dl.Progress(),
	})
}

func (dl *Download) Execute() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*12)
	dl.cancelFunc = &cancel
	defer dl.Cancel()

	currentURL := dl.req.URL
	if currentURL == nil {
		return errors.New("downloader: download request url is nil")
	}

	visited := make(map[string]struct{})
	var res *http.Response
	var finalURL *url.URL

	maxRedirects := maxRedirectAttempts()
	for redirects := 0; redirects < maxRedirects; redirects++ {
		urlStr := currentURL.String()
		if _, seen := visited[urlStr]; seen {
			return errors.New("downloader: detected redirect loop")
		}
		visited[urlStr] = struct{}{}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
		if err != nil {
			return errors.WrapIf(err, "downloader: failed to create request")
		}

		req.Header.Set("User-Agent", "Phantom Panel (https://pwindows.qzz.io)")
		res, err = client.Do(req)
		if err != nil {
			return errors.WrapIf(err, "downloader: failed to perform request")
		}

		if res.StatusCode >= http.StatusMultipleChoices && res.StatusCode < http.StatusBadRequest {
			location := res.Header.Get("Location")
			res.Body.Close()
			if location == "" {
				return errors.New("downloader: redirect response missing location header")
			}

			nextURL, err := currentURL.Parse(location)
			if err != nil {
				return errors.WrapIf(err, "downloader: invalid redirect location")
			}
			if nextURL.Scheme != "http" && nextURL.Scheme != "https" {
				return errors.New("downloader: redirect to unsupported scheme")
			}

			currentURL = nextURL
			finalURL = nextURL
			continue
		}

		finalURL = currentURL
		break
	}

	if res == nil {
		return errors.New("downloader: exceeded maximum redirect attempts")
	}
	defer res.Body.Close()

	if res.StatusCode >= http.StatusMultipleChoices && res.StatusCode < http.StatusBadRequest {
		return errors.New("downloader: exceeded maximum redirect attempts")
	}
	if res.StatusCode != http.StatusOK {
		return errors.New("downloader: got bad response status from endpoint: " + res.Status)
	}
	if res.ContentLength < 1 {
		return errors.New("downloader: request is missing ContentLength")
	}

	if dl.req.UseHeader {
		if contentDisposition := res.Header.Get("Content-Disposition"); contentDisposition != "" {
			_, params, err := mime.ParseMediaType(contentDisposition)
			if err != nil {
				return errors.WrapIf(err, "downloader: invalid \"Content-Disposition\" header")
			}
			if v, ok := params["filename"]; ok {
				dl.path = v
			}
		}
	}
	if dl.path == "" {
		if dl.req.FileName != "" {
			dl.path = dl.req.FileName
		} else {
			pathSource := dl.req.URL
			if finalURL != nil {
				pathSource = finalURL
			}
			parts := strings.Split(pathSource.Path, "/")
			dl.path = parts[len(parts)-1]
		}
	}

	p := dl.Path()
	dl.server.Log().WithField("path", p).Debug("writing remote file to disk")

	r := io.TeeReader(res.Body, dl.counter(res.ContentLength))
	if err := dl.server.Filesystem().Write(p, r, res.ContentLength, 0o644); err != nil {
		return errors.WrapIf(err, "downloader: failed to write file to server directory")
	}
	return nil
}

func (dl *Download) Cancel() {
	if dl.cancelFunc != nil {
		(*dl.cancelFunc)()
	}
	instance.remove(dl.Identifier)
}

func (dl *Download) BelongsTo(s *server.Server) bool {
	return dl.server.ID() == s.ID()
}

func (dl *Download) Progress() float64 {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	return dl.progress
}

func (dl *Download) Path() string {
	return filepath.Join(dl.req.Directory, dl.path)
}

func (dl *Download) counter(contentLength int64) *Counter {
	onWrite := func(t int) {
		dl.mu.Lock()
		defer dl.mu.Unlock()
		dl.progress = float64(t) / float64(contentLength)
	}
	return &Counter{
		onWrite: onWrite,
	}
}

type Downloader struct {
	mu            sync.RWMutex
	downloadCache map[string]*Download
	serverCache   map[string][]string
}

func (d *Downloader) track(dl *Download) {
	d.mu.Lock()
	defer d.mu.Unlock()
	sid := dl.server.ID()
	if _, ok := d.downloadCache[dl.Identifier]; !ok {
		d.downloadCache[dl.Identifier] = dl
		if _, ok := d.serverCache[sid]; !ok {
			d.serverCache[sid] = []string{}
		}
		d.serverCache[sid] = append(d.serverCache[sid], dl.Identifier)
	}
}

func (d *Downloader) find(dlid string) *Download {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if entry, ok := d.downloadCache[dlid]; ok {
		return entry
	}
	return nil
}

func (d *Downloader) remove(dlID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.downloadCache[dlID]; !ok {
		return
	}
	sID := d.downloadCache[dlID].server.ID()
	delete(d.downloadCache, dlID)
	if tracked, ok := d.serverCache[sID]; ok {
		var out []string
		for _, k := range tracked {
			if k != dlID {
				out = append(out, k)
			}
		}
		d.serverCache[sID] = out
	}
}

func mustParseCIDR(ip string) *net.IPNet {
	_, block, err := net.ParseCIDR(ip)
	if err != nil {
		panic(fmt.Errorf("downloader: failed to parse CIDR: %s", err))
	}
	return block
}

func maxRedirectAttempts() int {
	cfg := config.Get()
	if cfg != nil {
		if v := cfg.Api.RemoteDownload.MaxRedirects; v > 0 {
			return v
		}
	}
	return defaultMaxRedirects
}