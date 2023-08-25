// Licensed to Apache Software Foundation (ASF) under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Apache Software Foundation (ASF) licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/apache/skywalking-infra-e2e/internal/components/verifier"
	"github.com/apache/skywalking-infra-e2e/internal/config"
	"github.com/apache/skywalking-infra-e2e/internal/logger"
	"github.com/apache/skywalking-infra-e2e/internal/util"
	"github.com/apache/skywalking-infra-e2e/pkg/output"
)

var (
	query    string
	get      string
	actual   string
	expected string
	printer  output.Printer
)

func init() {
	Verify.Flags().StringVarP(&query, "query", "q", "", "the query to get the actual data, the result of the query should in YAML format")
	Verify.Flags().StringVarP(&get, "get", "g", "", "the get to get the actual data, the result of the query should in YAML format")
	Verify.Flags().StringVarP(&actual, "actual", "a", "", "the actual data file, only YAML file format is supported")
	Verify.Flags().StringVarP(&expected, "expected", "e", "", "the expected data file, only YAML file format is supported")
}

// Verify verifies that the actual data satisfies the expected data pattern.
var Verify = &cobra.Command{
	Use:   "verify",
	Short: "verify if the actual data match the expected data",
	RunE: func(cmd *cobra.Command, args []string) error {
		if expected != "" {
			return verifySingleCase(expected, actual, query, get, nil)
		}

		// If there is no given flags.
		return DoVerifyAccordingConfig(nil)
	},
}

// verifyInfo contains necessary information about verification
type verifyInfo struct {
	caseNumber int
	retryCount int
	interval   time.Duration
	failFast   bool
}

func verifySingleCase(expectedFile, actualFile, query, get string, verifyCase *config.VerifyCase) error {
	expectedData, err := util.ReadFileContent(expectedFile)
	if err != nil {
		return fmt.Errorf("failed to read the expected data file: %v", err)
	}

	var actualData, sourceName, stderr string
	var actualHeader map[string]string
	if verifyCase.ActualData != "" {
		sourceName = "origin"
		actualData = verifyCase.ActualData
		actualHeader = verifyCase.ActualHeader
	} else if actualFile != "" {
		sourceName = actualFile
		actualData, err = util.ReadFileContent(actualFile)
		if err != nil {
			return fmt.Errorf("failed to read the actual data file: %v", err)
		}
	} else if query != "" {
		sourceName = query
		actualData, stderr, err = util.ExecuteCommand(query)
		if err != nil {
			return fmt.Errorf("failed to execute the query: %s, output: %s, error: %v", query, actualData, stderr)
		}
	} else if get != "" {
		sourceName = get
		header, origData, err := getData("GET", get, verifyCase.Headers)
		if err != nil {
			return fmt.Errorf("failed to execute the get: %s, header: %v, resp: %s, error: %v", get, actualHeader, origData, err)
		}
		actualHeader = convertHeaderToMap(header)
		//actualData, err = convertToYaml(origData)
		actualData = string(origData)
		logger.Log.Debugf("response header: %s", actualHeader)
		logger.Log.Debugf("response data: \n%s", actualData)
	}

	if err = checkHead(actualHeader, verifyCase.ExpectedHeaders, verifyCase.Name); err != nil {
		return err
	}

	if err = verifier.Verify(actualData, expectedData); err != nil {
		if me, ok := err.(*verifier.MismatchError); ok {
			return fmt.Errorf("failed to verify the output: %s, error:\n%v", sourceName, me.Error())
		}
		return fmt.Errorf("failed to verify the output: %s, error:\n%v", sourceName, err)
	}
	return nil
}

func request(method string, url string, m map[string]string) (*http.Request, error) {
	request, err := http.NewRequest(method, url, strings.NewReader(""))
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	for k, v := range m {
		headers[k] = []string{v}
	}
	request.Header = headers
	return request, err
}

func getData(method string, url string, m map[string]string) (http.Header, []byte, error) {
	client := &http.Client{}
	req, err := request(method, url, m)
	if err != nil {
		logger.Log.Errorf("failed to create new request %v", err)
		return nil, nil, err
	}

	response, err := client.Do(req)
	if err != nil {
		logger.Log.Errorf("do request error %v", err)
		return nil, nil, err
	}
	head := response.Header
	b, _ := io.ReadAll(response.Body)

	_ = response.Body.Close()

	logger.Log.Debugf("do request %v response http code %v", url, response.StatusCode)
	if response.StatusCode == http.StatusOK {
		logger.Log.Debugf("do http %+v success.", url)
		return head, b, err
	}
	return nil, nil, fmt.Errorf("do request failed, response status code: %d", response.StatusCode)
}

// concurrentlyVerifySingleCase verifies a single case in concurrency mode,
// it will call the cancel function if the case fails and the fail-fast is enabled.
func concurrentlyVerifySingleCase(
	ctx context.Context,
	cancel context.CancelFunc,
	v *config.VerifyCase,
	verifyInfo *verifyInfo,
) (res *output.CaseResult) {
	res = &output.CaseResult{}
	defer func() {
		if res.Err != nil && verifyInfo.failFast {
			cancel()
		}
	}()

	if v.GetExpected() == "" {
		res.Msg = fmt.Sprintf("failed to verify %v:", caseName(v))
		res.Err = fmt.Errorf("the expected data file for %v is not specified", caseName(v))
		return res
	}

	for current := 0; current <= verifyInfo.retryCount; current++ {
		select {
		case <-ctx.Done():
			res.Skip = true
			return res
		default:
			if err := verifySingleCase(v.GetExpected(), v.GetActual(), v.Query, v.Get, v); err == nil {
				if current == 0 {
					res.Msg = fmt.Sprintf("verified %v\n", caseName(v))
				} else {
					res.Msg = fmt.Sprintf("verified %v, retried %d time(s)\n", caseName(v), current)
				}
				return res
			} else if current != verifyInfo.retryCount {
				time.Sleep(verifyInfo.interval)
			} else {
				res.Msg = fmt.Sprintf("failed to verify %v, retried %d time(s):", caseName(v), current)
				res.Err = err
			}
		}
	}

	return res
}

// verifyCasesConcurrently verifies the cases concurrently.
func verifyCasesConcurrently(cases []*config.VerifyCase, verifyInfo *verifyInfo) error {
	res := make([]*output.CaseResult, len(cases))
	for i := range res {
		res[i] = &output.CaseResult{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	for idx := range cases {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			// Check if the context is canceled before verifying the case.
			select {
			case <-ctx.Done():
				res[i].Skip = true
				return
			default:
				// It's safe to do this, since each goroutine only modifies a single, different, designated slice element.
				res[i] = concurrentlyVerifySingleCase(ctx, cancel, cases[i], verifyInfo)
			}
		}(idx)
	}
	wg.Wait()

	_, errNum, _ := printer.PrintResult(res)
	if errNum > 0 {
		return fmt.Errorf("failed to verify %d case(s)", errNum)
	}

	return nil
}

// verifyCasesSerially verifies the cases serially.
func verifyCasesSerially(cases []*config.VerifyCase, verifyInfo *verifyInfo) (err error) {
	// A case may be skipped in fail-fast mode, so set it in advance.
	res := make([]*output.CaseResult, len(cases))
	for i := range res {
		res[i] = &output.CaseResult{
			Skip: true,
		}
	}

	defer func() {
		_, errNum, _ := printer.PrintResult(res)
		if errNum > 0 {
			err = fmt.Errorf("failed to verify %d case(s)", errNum)
		}
	}()

	for idx := range cases {
		printer.Start()
		v := cases[idx]

		if v.GetExpected() == "" {
			res[idx].Skip = false
			res[idx].Msg = fmt.Sprintf("failed to verify %v", caseName(v))
			res[idx].Err = fmt.Errorf("the expected data file for %v is not specified", caseName(v))

			printer.Warning(res[idx].Msg)
			printer.Fail(res[idx].Err.Error())
			if verifyInfo.failFast {
				return
			}
			continue
		}

		for current := 0; current <= verifyInfo.retryCount; current++ {
			if e := verifySingleCase(v.GetExpected(), v.GetActual(), v.Query, v.Get, v); e == nil {
				if current == 0 {
					res[idx].Msg = fmt.Sprintf("verified %v \n", caseName(v))
				} else {
					res[idx].Msg = fmt.Sprintf("verified %v, retried %d time(s)\n", caseName(v), current)
				}
				res[idx].Skip = false
				printer.Success(res[idx].Msg)
				break
			} else if current != verifyInfo.retryCount {
				if current == 0 {
					printer.UpdateText(fmt.Sprintf("failed to verify %v, will continue retry:", caseName(v)))
				} else {
					printer.UpdateText(fmt.Sprintf("failed to verify %v, retry [%d/%d]", caseName(v), current, verifyInfo.retryCount))
				}
				time.Sleep(verifyInfo.interval)
			} else {
				res[idx].Msg = fmt.Sprintf("failed to verify %v, retried %d time(s):", caseName(v), current)
				res[idx].Err = e
				res[idx].Skip = false
				printer.UpdateText(fmt.Sprintf("failed to verify %v, retry [%d/%d]", caseName(v), current, verifyInfo.retryCount))
				printer.Warning(res[idx].Msg)
				printer.Fail(res[idx].Err.Error())
				if verifyInfo.failFast {
					return
				}
			}
		}
	}

	return nil
}

func caseName(v *config.VerifyCase) string {
	if v.Name == "" {
		if v.Actual != "" {
			return fmt.Sprintf("case[%s]", v.Actual)
		}
		return fmt.Sprintf("case[%s]", v.Query)
	}
	return v.Name
}

// DoVerifyAccordingConfig reads cases from the config file and verifies them.
func DoVerifyAccordingConfig(cases []*config.VerifyCase) error {
	if config.GlobalConfig.Error != nil {
		return config.GlobalConfig.Error
	}
	e2eConfig := config.GlobalConfig.E2EConfig
	retryCount := e2eConfig.Verify.RetryStrategy.Count
	if retryCount <= 0 {
		retryCount = 0
	}
	interval, err := parseInterval(e2eConfig.Verify.RetryStrategy.Interval)
	if err != nil {
		return err
	}
	failFast := e2eConfig.Verify.FailFast
	if cases == nil {
		for _, ca := range e2eConfig.Verify.Cases {
			cases = append(cases, &ca)
		}
	}
	caseNumber := len(cases)
	VerifyInfo := verifyInfo{
		caseNumber,
		retryCount,
		interval,
		failFast,
	}
	concurrency := e2eConfig.Verify.Concurrency
	if concurrency {
		// enable batch output mode when concurrency is enabled
		printer = output.NewPrinter(true)
		return verifyCasesConcurrently(cases, &VerifyInfo)
	}

	printer = output.NewPrinter(util.BatchMode)
	return verifyCasesSerially(cases, &VerifyInfo)
}

// TODO remove this in 2.0.0
func parseInterval(retryInterval any) (time.Duration, error) {
	var interval time.Duration
	var err error
	switch itv := retryInterval.(type) {
	case int:
		logger.Log.Warnf(`configuring verify.retry.interval with number is deprecated
and will be removed in future version, please use Duration style instead, such as 10s, 1m.`)
		interval = time.Duration(itv) * time.Millisecond
	case string:
		if interval, err = time.ParseDuration(itv); err != nil {
			return 0, err
		}
	case nil:
		interval = 0
	default:
		return 0, fmt.Errorf("failed to parse verify.retry.interval: %v", retryInterval)
	}
	if interval < 0 {
		interval = 1 * time.Second
	}
	return interval, nil
}
