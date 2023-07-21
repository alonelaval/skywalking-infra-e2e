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
package trigger

import (
	"fmt"
	"sync"

	"github.com/spf13/cobra"

	"github.com/apache/skywalking-infra-e2e/internal/components/trigger"
	"github.com/apache/skywalking-infra-e2e/internal/config"
	"github.com/apache/skywalking-infra-e2e/internal/util"

	"github.com/apache/skywalking-infra-e2e/internal/constant"
)

var Trigger = &cobra.Command{
	Use: "trigger",
	RunE: func(cmd *cobra.Command, args []string) error {
		action, err := CreateTriggerAction(config.GlobalConfig.E2EConfig.Trigger)
		if err != nil {
			return fmt.Errorf("[Trigger] %v", err)
		}
		if action == nil {
			return nil
		}
		if err := <-action.Do(); err != nil {
			return err
		}

		wg := sync.WaitGroup{}
		wg.Add(1)
		util.AddShutDownHook(wg.Done)
		wg.Wait()

		action.Stop()
		return nil
	},
}

var Triggers = &cobra.Command{
	Use: "triggers",
	RunE: func(cmd *cobra.Command, args []string) error {
		var acts []trigger.Action
		for _, v := range config.GlobalConfig.E2EConfig.Triggers {
			action, err := CreateTriggerAction(v)
			if err != nil {
				return fmt.Errorf("[Trigger] %v", err)
			}
			if action == nil {
				return nil
			}
			if err := <-action.Do(); err != nil {
				return err
			}
			acts = append(acts, action)
		}

		wg := sync.WaitGroup{}
		wg.Add(1)
		util.AddShutDownHook(wg.Done)
		wg.Wait()

		for _, act := range acts {
			act.Stop()
		}
		return nil
	},
}

func CreateTriggerAction(cnf config.Trigger) (trigger.Action, error) {
	if err := config.GlobalConfig.Error; err != nil {
		return nil, err
	}

	switch t := cnf; t.Action {
	case "":
		return nil, nil
	case constant.ActionHTTP:
		return trigger.NewHTTPAction(
			t.Interval,
			t.Times,
			t.URL,
			t.Method,
			t.Body,
			t.Headers,
		)
	default:
		return nil, fmt.Errorf("unsupported trigger action: %s", t.Action)
	}
}
