package connector

import (
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
)

func TestCanHandleLargeHeaders(t *testing.T) {
	expect := assert.New(t)
	req, err := http.NewRequest("GET", testEndpoint("/headers"), nil)
	expect.Nil(err)

	for i := 0; i < 100; i++ {
		req.Header.Set(fmt.Sprintf("X-Header%d", i), strings.Repeat(fmt.Sprintf("value%d", i), 40))
	}

	resp, err := testClient.Do(req)
	expect.Nil(err)
	expect.Equal(200, resp.StatusCode)

	defer resp.Body.Close()

	rawBody, err := ioutil.ReadAll(resp.Body)
	expect.Nil(err)

	s := struct {
		Headers map[string]json.RawMessage `json:"headers"`
	}{}
	err = json.Unmarshal(rawBody, &s)
	expect.Nil(err)

	b := s.Headers
	for i := 0; i < 100; i++ {
		expect.Equal(fmt.Sprintf(`["%s"]`, strings.Repeat(fmt.Sprintf("value%d", i), 40)), string(b[fmt.Sprintf("X-Header%d", i)]))
	}
}
