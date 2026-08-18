package main

import "testing"

func TestProportionalBarWidth(t *testing.T) {
	tests := []struct {
		name                  string
		value, largest, width int64
		want                  int
	}{
		{name: "zero activity", value: 0, largest: 0, width: 12, want: 0},
		{name: "zero beside activity", value: 0, largest: 10, width: 12, want: 0},
		{name: "single tool fills available width", value: 10, largest: 10, width: 12, want: 12},
		{name: "half activity", value: 5, largest: 10, width: 12, want: 6},
		{name: "small activity stays visible", value: 1, largest: 10, width: 12, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := proportionalBarWidth(tt.value, tt.largest, int(tt.width)); got != tt.want {
				t.Fatalf("proportionalBarWidth(%d, %d, %d) = %d, want %d", tt.value, tt.largest, tt.width, got, tt.want)
			}
		})
	}
}

func TestLocalActivitySparkline(t *testing.T) {
	if got, want := localActivitySparkline([]int64{0, 0, 0}), "░░░"; got != want {
		t.Fatalf("zero sparkline = %q, want %q", got, want)
	}
	if got := localActivitySparkline([]int64{0, 5, 10}); got != "░▅█" {
		t.Fatalf("scaled sparkline = %q, want %q", got, "░▅█")
	}
}
