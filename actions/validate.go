package actions

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mihaitodor/wormhole/config"
	"github.com/mihaitodor/wormhole/connection"
)

type ValidateAction struct {
	ActionBase
	Scheme      string
	Port        uint
	UrlPath     string `yaml:"url_path"`
	Retries     uint
	Timeout     time.Duration
	StatusCode  int    `yaml:"status_code"`
	BodyContent string `yaml:"body_content"`
}

func (a *ValidateAction) validate(ctx context.Context, req *http.Request) error {
	ctx, timeoutFunc := context.WithTimeout(ctx, a.Timeout)
	defer timeoutFunc()

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("failed to execute request: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != a.StatusCode {
		return fmt.Errorf("expected status %d but got %d instead", a.StatusCode, resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %s", err)
	}
	if !strings.Contains(string(body), a.BodyContent) {
		return errors.New("response does not contain expected content")
	}

	return nil
}

func (a *ValidateAction) Run(ctx context.Context, conn *connection.Connection, _ config.Config) error {
	host := conn.Server.Host
	if a.Port != 0 {
		host = fmt.Sprintf("%s:%d", host, a.Port)
	}
	u := url.URL{
		Scheme: a.Scheme,
		Host:   host,
		Path:   a.UrlPath,
	}

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create http request: %s", err)
	}

	// Try to run and validate the request several times
	retries := 1
	if a.Retries > 0 {
		retries = int(a.Retries)
	}
	for i := 0; i < retries; i++ {
		err = a.validate(ctx, req)
		if err == nil {
			return nil
		}
	}

	return fmt.Errorf("failed to validate %q after %d retries: %s", u.String(), a.Retries, err)
}
