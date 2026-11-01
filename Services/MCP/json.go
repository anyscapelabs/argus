package MCP

import "encoding/json"

func jsonUnmarshalImpl(s string, v interface{}) error {
	return json.Unmarshal([]byte(s), v)
}

func jsonMarshalImpl(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}
