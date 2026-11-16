package haclient

import (
	"net/url"
	"strings"
)

// CameraProxyURL returns the HA still-image endpoint for a camera entity.
func CameraProxyURL(haBaseURL, entityID string) string {
	return cameraAPIPath(haBaseURL, "camera_proxy", entityID)
}

// CameraProxyStreamURL returns the HA MJPEG stream endpoint for a camera entity.
func CameraProxyStreamURL(haBaseURL, entityID string) string {
	return cameraAPIPath(haBaseURL, "camera_proxy_stream", entityID)
}

func cameraAPIPath(haBaseURL, segment, entityID string) string {
	base := strings.TrimSuffix(strings.TrimSpace(haBaseURL), "/")
	return base + "/api/" + segment + "/" + url.PathEscape(entityID)
}