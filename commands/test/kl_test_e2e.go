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
	"fmt"
	"github.com/apache/skywalking-infra-e2e/commands/cleanup"
	"github.com/apache/skywalking-infra-e2e/commands/setup"
	"github.com/apache/skywalking-infra-e2e/internal/components/test"
	t "github.com/apache/skywalking-infra-e2e/internal/components/trigger"
	"github.com/apache/skywalking-infra-e2e/internal/config"
	"github.com/apache/skywalking-infra-e2e/internal/constant"
	"github.com/apache/skywalking-infra-e2e/internal/logger"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"

	//"gopkg.in/yaml.v2"
	"net/http"
)

var Test = &cobra.Command{
	Use: "test",
	RunE: func(cmd *cobra.Command, args []string) error {

		if config.GlobalConfig.Error != nil {
			return config.GlobalConfig.Error
		}

		var action t.Action
		stopAction := func() {
			if action != nil {
				action.Stop()
			}
		}
		// If cleanup.on == Always and there is error in setup step, we should defer cleanup step right now.
		cleanupOnCondition := config.GlobalConfig.E2EConfig.Cleanup.On
		if cleanupOnCondition == constant.CleanUpAlways {
			defer doCleanup(stopAction)
		}

		// setup part
		err := setup.DoSetupAccordingE2E()
		if err != nil {
			return err
		}
		logger.Log.Infof("setup part finished successfully")

		if cleanupOnCondition != constant.CleanUpAlways {
			defer func() {
				shouldCleanup := (cleanupOnCondition == constant.CleanUpOnSuccess && err == nil) ||
					(cleanupOnCondition == constant.CleanUpOnFailure && err != nil)

				if !shouldCleanup {
					logger.Log.Infof("don't cleanup according to config")
					return
				}

				doCleanup(stopAction)
			}()
		}

		//var acts []test.Action
		for _, v := range config.GlobalConfig.E2EConfig.Test {
			action, err := CreateTriggerAction(v)
			if err != nil {
				return fmt.Errorf("[Test] %v", err)
			}
			if action == nil {
				continue
			}
			e, header, body := action.Do()

			if err := <-e; err != nil {
				return err
			}
			h := <-header
			b := <-body
			ydata, _ := convertToYaml(b)
			logger.Log.Debugf("response header: %s", h)
			logger.Log.Debugf("response data: \n%s", ydata)
			action.Stop()
			var cases []*config.VerifyCase
			for i := range v.Case {
				ca := v.Case[i]
				ca.TestName = v.Name
				cases = append(cases, &ca)
				if ca.Get != "" || ca.Query != "" {
					continue
				}
				ca.SetActualHeader(convertHeaderToMap(h))
				ca.SetActualData(ydata)
			}
			err = DoVerifyAccordingConfig(cases)
			if err != nil {
				return err
			}
		}
		return nil
	},
}

func doCleanup(stopAction func()) {
	if stopAction != nil {
		stopAction()
	}
	setup.DoStopSetup()
	if err := cleanup.DoCleanupAccordingE2E(); err != nil {
		logger.Log.Errorf("cleanup part error: %s", err)
	} else {
		logger.Log.Infof("cleanup part finished successfully")
	}
}

func convertHeaderToMap(header http.Header) map[string]string {
	m := make(map[string]string)
	for k, v := range header {
		m[k] = v[0]
	}
	return m
}
func convertToYaml(data []byte) (string, error) {
	bytes, e := JSONToYAML(data)
	if e != nil {
		return "", e
	}
	d := fmt.Sprintf("%v", string(bytes))
	return d, nil
}

func checkHead(actualHeader map[string]string, expectedHeader map[string]string, name string) error {
	for k, v := range expectedHeader {
		rv := actualHeader[k]
		if rv != v {
			return fmt.Errorf("case: %s header: %s, expected: %s actual: %s", name, k, v, rv)
		}
	}
	return nil
}

func JSONToYAML(j []byte) ([]byte, error) {
	// Convert the JSON to an object.
	jsonObj := yaml.MapSlice{}

	// We are using yaml.Unmarshal here (instead of json.Unmarshal) because the
	// Go JSON library doesn't try to pick the right number type (int, float,
	// etc.) when unmarshalling to interface{}, it just picks float64
	// universally. go-yaml does go through the effort of picking the right
	// number type, so we can preserve number type throughout this process.

	err := yaml.Unmarshal(j, &jsonObj)
	if err != nil {
		return nil, err
	}

	// Marshal this object into YAML.
	return yaml.Marshal(jsonObj)
}

func CreateTriggerAction(cnf config.Test) (test.Action, error) {
	if err := config.GlobalConfig.Error; err != nil {
		return nil, err
	}

	switch t := cnf; t.Action {
	case "":
		return nil, nil
	case constant.ActionHTTP:
		return test.NewHTTPAction(
			t.Interval,
			t.Times,
			t.URL,
			t.Method,
			t.Body,
			t.Headers,
		)
	default:
		return nil, fmt.Errorf("unsupported Test action: %s", t.Action)
	}
}
