package go-loadbalancer

import "testing"

func BenchmarkMain(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = i
	}
}
