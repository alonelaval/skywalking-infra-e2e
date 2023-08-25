package test

import (
	"fmt"
	"github.com/apache/skywalking-infra-e2e/internal/components/test"
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
			logger.Log.Debugf("response header: %s", h)
			logger.Log.Debugf("response data: \n%s", ydata)
			var cases []*config.VerifyCase
			for i := range v.Case {
				ca := v.Case[i]
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
