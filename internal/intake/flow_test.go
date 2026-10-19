package intake

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFlowMetadataDropsContent(t *testing.T) {
	b, err := Decode(strings.NewReader(`{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"claude-code"}}]},"scopeLogs":[{"logRecords":[{"timeUnixNano":"1792324800000000000","attributes":[{"key":"session.id","value":{"stringValue":"one"}},{"key":"event.name","value":{"stringValue":"api_request"}},{"key":"model","value":{"stringValue":"claude-test"}},{"key":"input_tokens","value":{"intValue":"120"}},{"key":"output_tokens","value":{"intValue":"30"}},{"key":"cost_usd","value":{"doubleValue":0.12}},{"key":"prompt","value":{"stringValue":"PRIVATE_PROMPT"}},{"key":"response","value":{"stringValue":"PRIVATE_RESPONSE"}}],"body":{"stringValue":"PRIVATE_BODY"}}]}]}]}`), "/v1/logs")
	if err != nil || len(b.Facts) != 1 {
		t.Fatal(b, err)
	}
	m := b.Facts[0].Metadata
	if m == nil || m.Model != "claude-test" || m.InputTokens == nil || *m.InputTokens != 120 || m.CostUSD == nil || *m.CostUSD != 0.12 {
		t.Fatal(m)
	}
	raw, _ := json.Marshal(b)
	if strings.Contains(string(raw), "PRIVATE_") {
		t.Fatal(string(raw))
	}
}

func TestUnknownDoubleContentRemainsIgnored(t *testing.T) {
	b, err := Decode(strings.NewReader(`{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"claude-code"}}]},"scopeLogs":[{"logRecords":[{"timeUnixNano":"1792324800000000000","eventName":"api_request","attributes":[{"key":"session.id","value":{"stringValue":"one"}},{"key":"unknown_content","value":{"doubleValue":"NaN"}},{"key":"cost_usd","value":{"doubleValue":"Infinity"}}]}]}]}]}`), "/v1/logs")
	if err != nil || len(b.Facts) != 1 {
		t.Fatal(b, err)
	}
	if b.Facts[0].Metadata != nil {
		t.Fatal("retained nonfinite cost")
	}
}
