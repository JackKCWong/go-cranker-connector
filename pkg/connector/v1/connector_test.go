package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"net/http"
	"testing"
	"time"
)

func TestCanHandleGetRequest(t *testing.T) {
	assert := assert.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	defer connector.Shutdown(ctx)

	req, _ := http.NewRequest("GET", testEndpoint("/get"), nil)
	resp, err := testClient.Do(req)

	assert.Nilf(err, "failed to request to cranker: %q", err)

	defer resp.Body.Close()
	assert.Equal("200 OK", resp.Status)
}

func TestCanHandleReconnect(t *testing.T) {
	assert := assert.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	defer connector.Shutdown(ctx)

	for i := 0; i < 6; i++ {
		req, _ := http.NewRequest("GET", testEndpoint("/get"), nil)
		resp, err := testClient.Do(req)

		assert.Nilf(err, "failed to request to cranker: %q", err)

		defer resp.Body.Close()
		assert.Equal("200 OK", resp.Status)

		time.Sleep(100 * time.Microsecond)
	}
}

func TestCanHandlePostRequest(t *testing.T) {
	assert := assert.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	req, _ := http.NewRequest("POST",
		testEndpoint("/post"),
		bytes.NewBufferString("hello world"))

	req = req.WithContext(ctx)

	resp, err := testClient.Do(req)

	assert.Nilf(err, "failed to request to cranker")
	assert.Equal("200 OK", resp.Status)

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)

	assert.Nilf(err, "failed to read resp from cranker")

	binResp := HttpBinResp{}
	err = json.Unmarshal(body, &binResp)
	assert.Nilf(err, "not a valid httpbin response")

	assert.Equal("hello world", binResp.Data)
}
