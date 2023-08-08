package test

import (
	"fmt"
	"github.com/apache/skywalking-infra-e2e/internal/components/test"
	"github.com/apache/skywalking-infra-e2e/internal/config"
	"github.com/apache/skywalking-infra-e2e/internal/constant"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
	"net/http"
)

var Test = &cobra.Command{
	Use: "test",
	RunE: func(cmd *cobra.Command, args []string) error {
		//var acts []test.Action
		for _, v := range config.GlobalConfig.E2EConfig.Test {
			action, err := CreateTriggerAction(v)
			if err != nil {
				return fmt.Errorf("[Test] %v", err)
			}
			if action == nil {
				return nil
			}
			e, header, body := action.Do()

			if err := <-e; err != nil {
				return err
			}
			h := <-header
			b := <-body
			ydata, _ := convertToYaml(b)
			fmt.Println("yaml---------")
			fmt.Println(ydata)
			for _, ca := range v.Case {
				fmt.Println(ca.Name)
				if e := checkHead(h, ca.Headers, ca); e != nil {
					return e
				}
				//fmt.Println("ddd:" + h.Get("Content-Type"))
				//y, _ := displayYaml(b)
			}
			//acts = append(acts, action)
		}

		//wg := sync.WaitGroup{}
		//wg.Add(1)
		//util.AddShutDownHook(wg.Done)
		//wg.Wait()
		//
		//for _, act := range acts {
		//	act.Stop()
		//}
		return nil
	},
}

func convertToYaml(data []byte) (string, error) {
	bytes, e := JSONToYAML(data)
	if e != nil {
		return "", e
	}
	d := fmt.Sprintf("%v", string(bytes))
	return d, nil
}

func checkHead(h http.Header, m map[string]string, c config.VerifyCase) error {
	for k, v := range m {
		rv := h.Get(k)
		if rv != v {
			return fmt.Errorf("case: %s header: %s, expected: %s actual: %s", c.Name, k, v, rv)
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
