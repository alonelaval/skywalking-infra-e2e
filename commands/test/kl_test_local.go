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
	"github.com/apache/skywalking-infra-e2e/internal/config"
	"github.com/apache/skywalking-infra-e2e/internal/logger"
	"github.com/spf13/cobra"
)

var T = &cobra.Command{
	Use: "t",
	RunE: func(cmd *cobra.Command, args []string) error {

		if config.GlobalConfig.Error != nil {
			return config.GlobalConfig.Error
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
