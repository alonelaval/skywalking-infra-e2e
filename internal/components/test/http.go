// Licensed to Apache Software Foundation (ASF) under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Apache Software Foundation (ASF) licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.
package test

import (
	"bytes"
	"fmt"
	"github.com/apache/skywalking-infra-e2e/internal/util"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apache/skywalking-infra-e2e/internal/logger"
)

type httpAction struct {
	interval      time.Duration
	times         int
	url           string
	method        string
	body          string
	headers       map[string]string
	uploadFiles   map[string]string
	executedCount int
	stopCh        chan struct{}
	client        *http.Client
}

func NewHTTPAction(intervalStr string, times int, url, method, body string, headers map[string]string, uploadFiles map[string]string) (Action, error) {
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		return nil, err
	}

	if interval <= 0 {
		return nil, fmt.Errorf("trigger interval should be > 0, but was %s", interval)
	}

	// there can be env variables in url, say, "http://${GATEWAY_HOST}:${GATEWAY_PORT}/test"
	url = os.ExpandEnv(url)

	return &httpAction{
		interval:      interval,
		times:         times,
		url:           url,
		method:        strings.ToUpper(method),
		body:          body,
		headers:       headers,
		uploadFiles:   uploadFiles,
		executedCount: 0,
		stopCh:        make(chan struct{}),
		client:        &http.Client{},
	}, nil
}

func (h *httpAction) Do() (chan error, chan http.Header, chan []byte) {
	t := time.NewTicker(h.interval)

	logger.Log.Infof("trigger will request URL %s %d times with interval %s.", h.url, h.times, h.interval)

	errResult := make(chan error)
	header := make(chan http.Header)
	body := make(chan []byte)

	sent := false
	go func() {
		for {
			select {
			case <-t.C:
				err, head, b := h.execute()

				// `err == nil`: if no error occurs, everything is OK and send `nil` to the channel to continue.
				// `h.times == h.executedCount`: reach to the maximum retry count and send the `err`, no matter it's `nil` or not.
				if !sent && (err == nil || h.times == h.executedCount) {
					errResult <- err
					header <- head
					body <- b
					sent = true
				}
			case <-h.stopCh:
				t.Stop()
				errResult <- nil
				return
			}
		}
	}()

	return errResult, header, body
}

func (h *httpAction) Stop() {
	h.stopCh <- struct{}{}
}

func (h *httpAction) request() (*http.Request, error) {
	var request *http.Request
	var err error
	headers := http.Header{}
	if h.uploadFiles != nil { //上传文件

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		for name, path := range h.uploadFiles {
			file, err := os.Open(util.ResolveAbs(path))

			part, err := writer.CreateFormFile(name, filepath.Base(path))
			if err != nil {
				return nil, err
			}
			_, err = io.Copy(part, file)
		}

		// 参数
		params := getParams(h.body)

		for key, val := range params {
			_ = writer.WriteField(key, val)
		}
		err = writer.Close()
		if err != nil {
			return nil, err
		}

		request, err = http.NewRequest(h.method, h.url, body)
		headers.Set("Content-Type", writer.FormDataContentType())
	} else { //不是上传文件的请求
		request, err = http.NewRequest(h.method, h.url, strings.NewReader(h.body))
	}

	if err != nil {
		return nil, err
	}

	for k, v := range h.headers {
		headers[k] = []string{v}
	}
	request.Header = headers
	return request, err
}

func getParams(params string) map[string]string {
	if params != "" {
		paramArr := strings.Split(params, "&")
		paramMap := make(map[string]string)
		for _, param := range paramArr {
			kv := strings.Split(param, "=")
			paramMap[kv[0]] = kv[1]
		}
		return paramMap
	}
	return nil
}

func (h *httpAction) execute() (error, http.Header, []byte) {
	req, err := h.request()
	if err != nil {
		logger.Log.Errorf("failed to create new request %v", err)
		return err, nil, nil
	}
	logger.Log.Debugf("request URL %s the %d time.", h.url, h.executedCount)
	response, err := h.client.Do(req)
	h.executedCount++
	if err != nil {
		logger.Log.Errorf("do request error %v", err)
		return err, nil, nil
	}
	head := response.Header
	b, _ := io.ReadAll(response.Body)

	_ = response.Body.Close()

	logger.Log.Debugf("do request %v response http code %v", h.url, response.StatusCode)
	if response.StatusCode == http.StatusOK {
		logger.Log.Debugf("do http action %+v success.", *h)
		return nil, head, b
	}
	return fmt.Errorf("do request failed, response status code: %d", response.StatusCode), nil, nil
}

func (h *httpAction) Execute() error {
	req, err := h.request()
	if err != nil {
		logger.Log.Errorf("failed to create new request %v", err)
		return err
	}
	logger.Log.Debugf("request URL %s the %d time.", h.url, h.executedCount)
	response, err := h.client.Do(req)
	h.executedCount++
	if err != nil {
		logger.Log.Errorf("do request error %v", err)
		return err
	}
	_, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()

	logger.Log.Debugf("do request %v response http code %v", h.url, response.StatusCode)
	if response.StatusCode == http.StatusOK {
		logger.Log.Debugf("do http action %+v success.", *h)
		return nil
	}
	return fmt.Errorf("do request failed, response status code: %d", response.StatusCode)
}
