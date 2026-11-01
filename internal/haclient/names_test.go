package haclient

import "testing"

func TestAccessoryDisplayName_area(t *testing.T) {
	got := AccessoryDisplayName(
		"light.kitchen_pendant",
		"Pendant",
		"kitchen",
		map[string]string{"kitchen": "Kitchen"},
		RoomNamingBoth,
	)
	if got != "Kitchen · Pendant" {
		t.Fatalf("got %q", got)
	}
}

func TestAccessoryDisplayName_entityIDFallback(t *testing.T) {
	got := AccessoryDisplayName(
		"light.kitchen_pendant",
		"Pendant",
		"",
		map[string]string{},
		RoomNamingBoth,
	)
	if got != "Kitchen · Pendant" {
		t.Fatalf("got %q", got)
	}
}
