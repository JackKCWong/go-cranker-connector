package connector

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func BenchmarkConcurrentShortRequests(b *testing.B) {
	expect := assert.New(b)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := testClient.Get(testEndpoint("/get"))
			expect.Nil(err)
			expect.Equal(200, resp.StatusCode)
		}
	})
}
