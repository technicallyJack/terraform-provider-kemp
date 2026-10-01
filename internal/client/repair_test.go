package client

import (
	"encoding/json"
	"testing"
)

func TestRepairJSONPreProcessRules(t *testing.T) {
	// Trimmed from a real showvs response (firmware 7.2.50.0.18765) with two
	// pre-processing rules attached. Line endings are \r\n as sent.
	raw := "{ \"code\": 200,\r\n \"Index\" : 4,\r\n \"NPreProcessRules\" : 2,\r\n\"PreProcessRules\": [\r\n" +
		"\"rule_a\",\r\n\"rule_b\" \r\n \"EspEnabled\" : false,\r\n]\r\n, \"InputAuthMode\" : 0,\r\n" +
		" \"MasterVS\" : 0,\r\n  \"status\": \"ok\"\r\n}"
	if json.Valid([]byte(raw)) {
		t.Fatal("fixture should reproduce the firmware bug")
	}

	fixed := repairJSON([]byte(raw))
	var got struct {
		Index           int      `json:"Index"`
		PreProcessRules []string `json:"PreProcessRules"`
		EspEnabled      bool     `json:"EspEnabled"`
		InputAuthMode   int      `json:"InputAuthMode"`
		Status          string   `json:"status"`
	}
	if err := json.Unmarshal(fixed, &got); err != nil {
		t.Fatalf("repaired JSON still invalid: %v\n%s", err, fixed)
	}
	if len(got.PreProcessRules) != 2 || got.PreProcessRules[1] != "rule_b" || got.Index != 4 || got.Status != "ok" {
		t.Fatalf("unexpected result: %+v", got)
	}

	// A single rule, as in listvs with several services, also repairs.
	single := `{"VS":[{"PreProcessRules": [
"only" 
 "EspEnabled" : true,
]
, "Index": 1},{"Index": 2}],"status":"ok"}`
	if !json.Valid(repairJSON([]byte(single))) {
		t.Fatalf("single-rule repair failed:\n%s", repairJSON([]byte(single)))
	}
}

func TestDoRepairsBrokenResponse(t *testing.T) {
	srv := newTestServer(t, "{ \"code\": 200,\r\n\"PreProcessRules\": [\r\n\"r\" \r\n \"EspEnabled\" : false,\r\n]\r\n, \"Index\" : 9,\r\n \"status\": \"ok\"\r\n}")
	defer srv.Close()

	c, _ := New(Config{Host: srv.URL, APIKey: "k", Insecure: true})
	vs, err := c.GetVirtualService(t.Context(), 9)
	if err != nil || vs.Index != 9 {
		t.Fatalf("got %+v, %v", vs, err)
	}
}
