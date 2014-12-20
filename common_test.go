package parse

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
)

type ctxT struct {
	ts            *httptest.Server
	oldHost       string
	oldHttpClient *http.Client
}

var ctx = ctxT{}

func setupTestServer(handler http.HandlerFunc) *httptest.Server {
	ts := httptest.NewTLSServer(handler)
	ctx.ts = ts

	_url, err := url.Parse(ts.URL)
	if err != nil {
		panic(err)
	}

	ctx.oldHost = parseHost
	ctx.oldHttpClient = httpClient

	parseHost = _url.Host
	httpClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	return ts
}

func teardownTestServer() {
	ctx.ts.Close()
	parseHost = ctx.oldHost
	httpClient = ctx.oldHttpClient
}
