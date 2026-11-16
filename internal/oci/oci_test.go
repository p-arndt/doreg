package oci

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestImageConfigDockerExtensions(t *testing.T) {
	data := []byte(`{
		"architecture": "amd64", "os": "linux",
		"config": {
			"ExposedPorts": {"8080/tcp": {}},
			"Healthcheck": {"Test": ["CMD-SHELL", "wget -qO- localhost:8080"], "Interval": 30000000000, "Retries": 3},
			"Shell": ["/bin/bash", "-c"],
			"OnBuild": ["COPY . /app"]
		}
	}`)
	var cfg ImageConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	hc := cfg.Config.Healthcheck
	if hc == nil || !reflect.DeepEqual(hc.Test, []string{"CMD-SHELL", "wget -qO- localhost:8080"}) ||
		hc.Interval != 30e9 || hc.Retries != 3 {
		t.Fatalf("healthcheck = %+v", hc)
	}
	if !reflect.DeepEqual(cfg.Config.Shell, []string{"/bin/bash", "-c"}) {
		t.Fatalf("shell = %v", cfg.Config.Shell)
	}
	if !reflect.DeepEqual(cfg.Config.OnBuild, []string{"COPY . /app"}) {
		t.Fatalf("onbuild = %v", cfg.Config.OnBuild)
	}

	out, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var back ImageConfig
	if err := json.Unmarshal(out, &back); err != nil || !reflect.DeepEqual(back, cfg) {
		t.Fatalf("round trip changed config: %s", out)
	}
}
