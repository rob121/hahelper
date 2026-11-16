package haclient

import "testing"

func TestCameraProxyStreamURL(t *testing.T) {
	got := CameraProxyStreamURL("http://hass:8123", "camera.door")
	want := "http://hass:8123/api/camera_proxy_stream/camera.door"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}