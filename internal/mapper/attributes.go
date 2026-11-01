package mapper

import haclient "github.com/mkelcik/go-ha-client/v2"

func deviceClass(se haclient.StateEntity) string {
	if se.Attributes == nil {
		return ""
	}
	s, _ := se.Attributes["device_class"].(string)
	return s
}

func isGarageCover(se haclient.StateEntity) bool {
	switch deviceClass(se) {
	case "garage", "garage_door":
		return true
	default:
		return false
	}
}
