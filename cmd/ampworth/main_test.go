package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAPI answers like the real API for one calculator and one document, so
// the tests run without the network.
func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	detail := `{"calculator":{"id":"solar-pv-output","title":"Solar PV annual output","summary":"A year of generation.","standard":"MIS 3002",
		"example":{"postcode":"BS1 5TR","capacityKwp":4.4,"orientation":30,"inclination":35},
		"inputSchema":{"type":"object","required":["capacityKwp","orientation","inclination"],"properties":{
			"postcode":{"type":"string","description":"UK postcode."},
			"capacityKwp":{"type":"number","description":"Capacity in kWp."},
			"orientation":{"type":"number","description":"Degrees from south."},
			"inclination":{"type":"number","description":"Pitch."},
			"clearHorizon":{"type":"boolean","description":"A made-up flag for the test."}}}}}`
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/calculators", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"calculators":[{"id":"solar-pv-output","title":"Solar PV annual output","standard":"MIS 3002"}]}`))
	})
	mux.HandleFunc("/api/v1/calculators/solar-pv-output", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery == "" && r.Method == http.MethodGet {
			w.Write([]byte(detail))
			return
		}
		q := r.URL.Query()
		if q.Get("capacityKwp") == "-5" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{"error":{"code":"validation_failed","message":"One or more fields are invalid.","fields":[{"field":"capacityKwp","message":"Must be between 0.1 and 5000 kWp."}]}}`))
			return
		}
		// Echo the query, so a test can see what the CLI sent.
		echo, _ := json.Marshal(q)
		w.Write([]byte(`{"calculator":"solar-pv-output","standard":"MIS 3002","result":` + string(echo) + `,"summary":"Estimated annual output: 4,162 kWh.","notice":"An installation needs a survey."}`))
	})
	mux.HandleFunc("/api/v1/documents/search", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"results":[{"slug":"sizing-a-heat-pump","kind":"guide","title":"Sizing a heat pump","summary":"How heat loss is worked out.","readingMinutes":8}]}`))
	})
	mux.HandleFunc("/api/v1/documents/guides/sizing-a-heat-pump", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"document":{"slug":"sizing-a-heat-pump","kind":"guide","title":"Sizing a heat pump","summary":"How heat loss is worked out.","text":"The full text.","url":"https://ampworth.com/guides/sizing-a-heat-pump"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func runCLI(t *testing.T, srv *httptest.Server, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(context.Background(), append([]string{"--api", srv.URL + "/api/v1"}, args...), strings.NewReader(""), &out)
	return out.String(), err
}

func TestRunMapsFlagsToFields(t *testing.T) {
	srv := fakeAPI(t)
	out, err := runCLI(t, srv, "run", "solar-pv-output", "--postcode", "BS1 5TR", "--capacity-kwp", "4.4",
		"--orientation=30", "--inclination", "35", "--clear-horizon", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Result map[string][]string `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	want := map[string]string{"postcode": "BS1 5TR", "capacityKwp": "4.4", "orientation": "30", "inclination": "35", "clearHorizon": "true"}
	for k, v := range want {
		if got := res.Result[k]; len(got) != 1 || got[0] != v {
			t.Errorf("%s sent as %v, want %q", k, got, v)
		}
	}
}

func TestRunPrintsTheSummary(t *testing.T) {
	out, err := runCLI(t, fakeAPI(t), "run", "solar-pv-output", "--capacityKwp", "4.4", "--orientation", "0", "--inclination", "35")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "4,162 kWh") || !strings.Contains(out, "survey") {
		t.Errorf("want the summary and the notice, got:\n%s", out)
	}
}

func TestRunErrors(t *testing.T) {
	srv := fakeAPI(t)
	_, err := runCLI(t, srv, "run", "solar-pv-output", "--capacity", "4")
	if _, ok := err.(usageError); !ok || !strings.Contains(err.Error(), "no field --capacity") {
		t.Errorf("an unknown flag gave %v", err)
	}
	_, err = runCLI(t, srv, "run", "solar-pv-output", "--capacity-kwp", "-5", "--orientation", "0", "--inclination", "35")
	if err == nil || !strings.Contains(err.Error(), "capacityKwp: Must be between") {
		t.Errorf("the server's field error should come through, got %v", err)
	}
}

func TestDescribeShowsFlags(t *testing.T) {
	out, err := runCLI(t, fakeAPI(t), "describe", "solar-pv-output")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--capacity-kwp  (number, required)", "--postcode  (string)", "ampworth run solar-pv-output"} {
		if !strings.Contains(out, want) {
			t.Errorf("describe is missing %q:\n%s", want, out)
		}
	}
}

func TestSearchAndRead(t *testing.T) {
	srv := fakeAPI(t)
	out, err := runCLI(t, srv, "search", "heat", "loss")
	if err != nil || !strings.Contains(out, "ampworth read guides/sizing-a-heat-pump") {
		t.Errorf("search: %v\n%s", err, out)
	}
	for _, ref := range []string{"guides/sizing-a-heat-pump", "https://ampworth.com/guides/sizing-a-heat-pump.md"} {
		out, err = runCLI(t, srv, "read", ref)
		if err != nil || !strings.Contains(out, "The full text.") {
			t.Errorf("read %s: %v\n%s", ref, err, out)
		}
	}
}

func TestKebabAndNormalise(t *testing.T) {
	if got := kebab("annualConsumptionKwh"); got != "annual-consumption-kwh" {
		t.Errorf("kebab = %q", got)
	}
	if normalise("annual-consumption-kwh") != normalise("annualConsumptionKwh") {
		t.Error("kebab and camel case should match")
	}
}
