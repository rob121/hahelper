package homekit

import "testing"

func TestSetupURI_bridgePin(t *testing.T) {
	uri, err := SetupURI("00102003", CategoryBridge, "HAHP")
	if err != nil {
		t.Fatal(err)
	}
	if len(uri) < len("X-HM://")+13 {
		t.Fatalf("unexpected uri length: %q", uri)
	}
	if uri[:7] != "X-HM://" {
		t.Fatalf("bad prefix: %q", uri)
	}
}
